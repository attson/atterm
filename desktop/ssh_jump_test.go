package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
)

// Jump-host chains (roadmap item 27, design §5.1–§5.3).
//
// Every test here uses forwardTestServer from ssh_tunnels_test.go rather than a
// second SSH server double: a bastion leg *is* a "direct-tcpip" channel, which
// that server already opens for real (it dials the destination from its own
// side and splices), so a chain built on it is carried by the same mechanism a
// real sshd would use.

// --- helpers ----------------------------------------------------------------

// newJumpTestApp builds an App whose known_hosts already trusts exactly the
// given servers. Anything not listed is an unknown host key, which is how the
// TOFU tests pick which hop the user gets asked about.
func newJumpTestApp(t *testing.T, trusted ...*forwardTestServer) *App {
	t.Helper()
	useIsolatedKeyring(t)
	a := &App{cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = writeKnownHostsForServers(t, trusted...)
	return a
}

func writeKnownHostsForServers(t *testing.T, servers ...*forwardTestServer) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "known_hosts")
	var b strings.Builder
	for _, s := range servers {
		b.WriteString(knownhosts.Line([]string{knownhosts.Normalize(s.addr)}, s.hostPub) + "\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// addSavedHost saves a host with its own credential — the point of §5.1 is that
// every hop has one of these, so tests never share a credential between hosts
// unless they are asserting about that.
func addSavedHost(t *testing.T, a *App, alias, host, port, user, pass, proxyJump string) SSHHost {
	t.Helper()
	h, err := a.AddSSHHost(SSHHost{
		Alias: alias, Host: host, Port: port, User: user,
		AuthKind: "password", ProxyJump: proxyJump,
	}, sshCredential{Password: pass})
	if err != nil {
		t.Fatalf("AddSSHHost %s: %v", alias, err)
	}
	return h
}

func addServerHost(t *testing.T, a *App, alias string, srv *forwardTestServer, user, pass, proxyJump string) SSHHost {
	t.Helper()
	host, port, _ := net.SplitHostPort(srv.addr)
	return addSavedHost(t, a, alias, host, port, user, pass, proxyJump)
}

// deadAddr returns an address nothing is listening on, so a dial to it is
// refused immediately rather than hanging until a timeout.
func deadAddr(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	h, p, _ := net.SplitHostPort(addr)
	return h, p
}

// waitClosed waits for the test server to have torn down n connections. The
// server counts a connection closed when its serve goroutine returns, which
// happens a moment after the client's Close, so this cannot be asserted
// synchronously.
func waitClosed(t *testing.T, srv *forwardTestServer, n int, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, closed := srv.counts(); closed >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	opened, closed := srv.counts()
	t.Fatalf("%s: want %d closed connection(s), got opened=%d closed=%d", what, n, opened, closed)
}

func assertNoDials(t *testing.T, srv *forwardTestServer, what string) {
	t.Helper()
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("%s: the check must happen before any dial, but the server saw %d connection(s)", what, opened)
	}
}

// --- §5.1 every hop must be a saved host ------------------------------------

// TestJumpHopMustBeASavedHost pins the credential decision of §5.1: a hop that
// is not in atterm's host list is refused, because the only ways to connect it
// would be to send the *target's* credential to a different machine or to
// invent a host record. The dial count is the load-bearing assertion — a
// refusal that happens after the first hop is already connected has already
// paid the side effect it exists to avoid.
func TestJumpHopMustBeASavedHost(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	a := newJumpTestApp(t, srv)
	target := addServerHost(t, a, "db", srv, "u", "pw", "nosuchhost")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("a ProxyJump naming an unsaved host must be refused")
	}
	if !strings.Contains(err.Error(), "nosuchhost") {
		t.Fatalf("error must name the unresolved hop, got %v", err)
	}
	if !strings.Contains(err.Error(), "add") {
		t.Fatalf("error must tell the user to add the hop as a host, got %v", err)
	}
	assertNoDials(t, srv, "unresolvable hop")
}

// TestJumpNeverReusesTargetCredential is a security assertion, not a wiring
// one. The bastion and the target have *different* credentials here, and the
// bastion's own record is the only place its password exists: an
// implementation that reused the target's credential for the chain would be
// sending the target machine's password to a different machine, and would show
// up in the bastion's authAttempts. Asserting only that the chain connected
// would pass against exactly that leak whenever the two credentials matched.
func TestJumpNeverReusesTargetCredential(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpTestApp(t, bastion, target)
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	targetHost := addServerHost(t, a, "db", target, "target-user", "target-pw", "bastion")

	chain, err := a.dialThroughJumps(context.Background(), targetHost, acceptedHostKey{})
	if err != nil {
		t.Fatalf("dialThroughJumps: %v", err)
	}
	defer chain.Close()

	if got := bastion.authCredentials(); len(got) != 1 || got[0] != "bastion-user:bastion-pw" {
		t.Fatalf("the bastion must be authenticated with its own saved credential, got %q", got)
	}
	for _, c := range bastion.authCredentials() {
		if strings.Contains(c, "target-pw") || strings.Contains(c, "target-user") {
			t.Fatalf("the target's credential reached the bastion: %q", c)
		}
	}
	if got := target.authCredentials(); len(got) != 1 || got[0] != "target-user:target-pw" {
		t.Fatalf("the target must be authenticated with its own credential, got %q", got)
	}
}

// TestJumpUserHostPortElementOnlyMatchesASavedHost pins the other half of
// §5.1: `root@bastion:2222` is parsed to find *which saved host is meant*, and
// nothing more. The user and port in the element are dropped on purpose —
// honouring them would mean dialling a host record that exists nowhere and has
// no credential, which is the very thing the saved-host rule rules out. So the
// connection here must use the bastion's saved username and port.
func TestJumpUserHostPortElementOnlyMatchesASavedHost(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpTestApp(t, bastion, target)
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	targetHost := addServerHost(t, a, "db", target, "target-user", "target-pw", "root@bastion:2222")

	chain, err := a.dialThroughJumps(context.Background(), targetHost, acceptedHostKey{})
	if err != nil {
		t.Fatalf("dialThroughJumps: %v", err)
	}
	defer chain.Close()

	if got := bastion.authCredentials(); len(got) != 1 || got[0] != "bastion-user:bastion-pw" {
		t.Fatalf("the element's user must be ignored in favour of the saved host's, got %q", got)
	}
	if opened, _ := bastion.counts(); opened != 1 {
		t.Fatalf("the saved bastion must be the machine dialled, got %d connection(s)", opened)
	}
}

// TestJumpElementPortPicksTheSavedHostOnThatPort: two saved records for the
// same hostname on different ports are an ordinary setup (a container and the
// machine hosting it). The port in the element cannot build a host, but it can
// say which of the two the user meant — and taking whichever was saved first
// would send the connection, and the credential, to the other machine.
func TestJumpElementPortPicksTheSavedHostOnThatPort(t *testing.T) {
	wrong := startForwardingSSHTestServerAs(t, "u-wrong", "p-wrong")
	right := startForwardingSSHTestServerAs(t, "u-right", "p-right")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t, wrong, right, srvT)
	// Both saved under the same hostname; only the port tells them apart, and
	// the one saved first is deliberately the wrong one.
	wrongHost, wrongPort, _ := net.SplitHostPort(wrong.addr)
	rightHost, rightPort, _ := net.SplitHostPort(right.addr)
	addSavedHost(t, a, "", wrongHost, wrongPort, "u-wrong", "p-wrong", "")
	addSavedHost(t, a, "", rightHost, rightPort, "u-right", "p-right", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", right.addr)

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err != nil {
		t.Fatalf("dialThroughJumps: %v", err)
	}
	defer chain.Close()

	if opened, _ := right.counts(); opened != 1 {
		t.Fatalf("the hop on the port the element named must be the one dialled, got %d connection(s)", opened)
	}
	if opened, _ := wrong.counts(); opened != 0 {
		t.Fatalf("the record on the other port must not be dialled, got %d connection(s)", opened)
	}
}

// --- §4 the chain itself -----------------------------------------------------

// TestJumpChainDialsEveryHopInOrder asserts the topology, not just the
// outcome: hop 1 is asked to reach hop 2, hop 2 to reach hop 3, hop 3 to reach
// the target. A chain that dialled the target directly (or dialled the hops in
// any other order) would still return a usable connection here, so the
// per-server direct-tcpip destinations are what actually pins §4's fold.
func TestJumpChainDialsEveryHopInOrder(t *testing.T) {
	srvA := startForwardingSSHTestServerAs(t, "ua", "pa")
	srvB := startForwardingSSHTestServerAs(t, "ub", "pb")
	srvC := startForwardingSSHTestServerAs(t, "uc", "pc")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t, srvA, srvB, srvC, srvT)
	addServerHost(t, a, "a", srvA, "ua", "pa", "")
	addServerHost(t, a, "b", srvB, "ub", "pb", "")
	addServerHost(t, a, "c", srvC, "uc", "pc", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "a,b,c")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err != nil {
		t.Fatalf("dialThroughJumps: %v", err)
	}
	defer chain.Close()

	for name, srv := range map[string]*forwardTestServer{"a": srvA, "b": srvB, "c": srvC, "target": srvT} {
		if opened, _ := srv.counts(); opened != 1 {
			t.Fatalf("hop %s: want exactly 1 connection, got %d", name, opened)
		}
	}
	wantVia := []struct {
		from *forwardTestServer
		name string
		to   string
	}{
		{srvA, "a", srvB.addr},
		{srvB, "b", srvC.addr},
		{srvC, "c", srvT.addr},
	}
	for _, w := range wantVia {
		got := w.from.directDestAddrs()
		if len(got) != 1 || got[0] != w.to {
			t.Fatalf("hop %s must be asked to reach %s, got %q", w.name, w.to, got)
		}
	}
	if got := srvT.directDestAddrs(); len(got) != 0 {
		t.Fatalf("the target must not be asked to reach anything, got %q", got)
	}
	// The number the target would be reported under in a TOFU prompt, read off
	// the finished chain — Task 3 asks the same question of a hops-only chain
	// and must get the same answer.
	if got := chain.targetHopIndex(); got != 4 {
		t.Fatalf("targetHopIndex = %d, want 4 (three hops plus the target)", got)
	}

	// And the handle really is the target: bytes round-trip through all three
	// hops to a service only the target can reach on our behalf.
	echoHost, echoPort := startEchoTarget(t)
	remote, err := chain.Target().DialRemote("tcp", net.JoinHostPort(echoHost, echoPort))
	if err != nil {
		t.Fatalf("dial through the chain's target: %v", err)
	}
	defer remote.Close()
	echoThrough(t, remote, "hello through three hops")
}

// --- §5.3 cycles and depth, before any dial ---------------------------------

// TestJumpCycleDetectedBeforeAnyDial: a → b → a is resolved statically, so
// nothing is dialled at all. The zero-dial assertion is the test — a cycle
// noticed while connecting has already logged into both machines.
func TestJumpCycleDetectedBeforeAnyDial(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	a := newJumpTestApp(t, srv)
	hostA := addServerHost(t, a, "a", srv, "u", "pw", "b")
	addServerHost(t, a, "b", srv, "u", "pw", "a")

	chain, err := a.dialThroughJumps(context.Background(), hostA, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("a ProxyJump cycle must be refused")
	}
	if !strings.Contains(err.Error(), "loop") && !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error must say the chain loops, got %v", err)
	}
	assertNoDials(t, srv, "cycle")
}

// TestJumpDepthLimited caps a chain at 10 hops, statically. A chain that only
// discovered the cap while dialling would have opened ten logins to find out.
func TestJumpDepthLimited(t *testing.T) {
	srv := startForwardingSSHTestServer(t)
	a := newJumpTestApp(t, srv)

	var names []string
	for i := 1; i <= 11; i++ {
		alias := fmt.Sprintf("h%d", i)
		addServerHost(t, a, alias, srv, "u", "pw", "")
		names = append(names, alias)
	}
	target := addServerHost(t, a, "db", srv, "u", "pw", strings.Join(names, ","))

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("a chain deeper than the cap must be refused")
	}
	if !strings.Contains(err.Error(), "10") {
		t.Fatalf("error must name the limit, got %v", err)
	}
	assertNoDials(t, srv, "depth cap")
}

// --- cleanup -----------------------------------------------------------------

// TestFailedHopClosesEarlierHops: the third hop is unreachable, so the two
// connections already established have to be closed before the error returns.
// Otherwise every failed attempt leaves a session hanging on bastions the user
// cannot see, let alone close.
func TestFailedHopClosesEarlierHops(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srv2 := startForwardingSSHTestServerAs(t, "u2", "p2")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")
	deadHost, deadPort := deadAddr(t)

	a := newJumpTestApp(t, srv1, srv2, srvT)
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "h2", srv2, "u2", "p2", "")
	addSavedHost(t, a, "h3", deadHost, deadPort, "u3", "p3", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1,h2,h3")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("a chain whose third hop is unreachable must fail")
	}
	if !strings.Contains(err.Error(), "h3") {
		t.Fatalf("error must name the hop that failed, got %v", err)
	}
	// Both earlier hops were really opened (so this is not passing because the
	// chain was skipped entirely) and both are closed again.
	for _, s := range []struct {
		srv  *forwardTestServer
		name string
	}{{srv1, "hop 1"}, {srv2, "hop 2"}} {
		if opened, _ := s.srv.counts(); opened != 1 {
			t.Fatalf("%s: want exactly 1 connection opened, got %d", s.name, opened)
		}
		waitClosed(t, s.srv, 1, s.name+" after a later hop failed")
	}
	if opened, _ := srvT.counts(); opened != 0 {
		t.Fatalf("the target must not be dialled when a hop failed, got %d connection(s)", opened)
	}
}

// --- §5.2 per-hop host key verification -------------------------------------

// TestUnknownHostKeyNamesTheHop is the security red line of the design. The
// middle hop's key is the unknown one here while the target's is already
// trusted, so a TOFU prompt that carried only a fingerprint would show the user
// an unfamiliar key with no way to tell whether it belongs to the machine they
// asked for or to something in the middle. Accepting under that ambiguity is
// TOFU as a formality.
func TestUnknownHostKeyNamesTheHop(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srv2 := startForwardingSSHTestServerAs(t, "u2", "p2")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	// srv2 is deliberately absent from known_hosts.
	a := newJumpTestApp(t, srv1, srvT)
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "bastion-b", srv2, "u2", "p2", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1,bastion-b")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("an unknown host key on a jump hop must not connect")
	}
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected *HostKeyUnknownError, got %v", err)
	}
	if hk.Error() != errCodeHostKeyUnknown {
		t.Fatalf("Error() must stay the %q sentinel the frontend detects, got %q",
			errCodeHostKeyUnknown, hk.Error())
	}
	if hk.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
	if hk.HopIndex != 2 {
		t.Fatalf("HopIndex = %d, want 2 (the second hop of the chain)", hk.HopIndex)
	}
	if hk.HopName != "bastion-b" {
		t.Fatalf("HopName = %q, want %q", hk.HopName, "bastion-b")
	}
	// The target was never reached, and the hop that was already up is closed.
	if opened, _ := srvT.counts(); opened != 0 {
		t.Fatalf("the target must not be dialled after a hop is refused, got %d connection(s)", opened)
	}
	waitClosed(t, srv1, 1, "hop 1 after the next hop's key was refused")
}

// TestAcceptedHostKeyIsScopedToOneHop is the other half of §5.2, and the half
// that is easy to get wrong in a way nobody notices.
//
// An "accept unknown host keys" flag would not merely let the rest of the chain
// through: sshclient's KnownHostsCallback *writes* an accepted key to
// known_hosts (handleUnknown → appendKnownHost), so every later hop the user was
// never shown would be recorded as trusted and would never prompt again. The
// substitution becomes invisible exactly because it was written down.
//
// So this walks the chain one acceptance at a time and asserts that each
// acceptance moves the prompt on to the *next* hop rather than silencing it, and
// that known_hosts only ever grows by the entry the user actually agreed to.
func TestAcceptedHostKeyIsScopedToOneHop(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srv2 := startForwardingSSHTestServerAs(t, "u2", "p2")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t) // trusts nothing at all
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "h2", srv2, "u2", "p2", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1,h2")

	// Each round: the prompt must name the next hop in order, and accepting it
	// must not accept anything else.
	var accepted acceptedHostKey
	for _, want := range []struct {
		hop  int
		name string
	}{{1, "h1"}, {2, "h2"}, {3, "db"}} {
		chain, err := a.dialThroughJumps(context.Background(), target, accepted)
		if err == nil {
			_ = chain.Close()
			t.Fatalf("hop %d (%s): accepting an earlier hop's key must not accept this one too",
				want.hop, want.name)
		}
		var hk *HostKeyUnknownError
		if !errors.As(err, &hk) {
			t.Fatalf("hop %d (%s): expected *HostKeyUnknownError, got %v", want.hop, want.name, err)
		}
		if hk.HopIndex != want.hop || hk.HopName != want.name {
			t.Fatalf("prompt was for hop %d %q, want hop %d %q",
				hk.HopIndex, hk.HopName, want.hop, want.name)
		}
		// Only the hops accepted so far are on file — an acceptance must never
		// write an entry for a machine the user was not shown.
		if got := countKnownHostsLines(t, a.sshKnownHostsPath); got != want.hop-1 {
			t.Fatalf("before accepting hop %d, known_hosts holds %d entries, want %d",
				want.hop, got, want.hop-1)
		}
		accepted = acceptedHostKey{Host: hk.Host, Fingerprint: hk.Fingerprint}
	}

	// With the last hop accepted the whole chain connects, and every key is now
	// on file — so a fresh attempt accepting nothing prompts for nothing.
	chain, err := a.dialThroughJumps(context.Background(), target, accepted)
	if err != nil {
		t.Fatalf("accepting the last unknown key must complete the chain: %v", err)
	}
	_ = chain.Close()
	if got := countKnownHostsLines(t, a.sshKnownHostsPath); got != 3 {
		t.Fatalf("known_hosts holds %d entries after accepting all three, want 3", got)
	}

	chain2, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err != nil {
		t.Fatalf("second connect must not prompt again: %v", err)
	}
	_ = chain2.Close()
}

// TestAcceptedHostKeyDoesNotTravelToAnotherHop is the narrower half of the same
// rule: an acceptance is a (host, fingerprint) pair, so it cannot be spent on a
// different machine. Keying it on the hop's *alias* instead would let a
// substituted hop inherit an acceptance the user granted elsewhere — one
// bastion can legitimately appear twice in a chain, and an alias is
// user-editable besides.
func TestAcceptedHostKeyDoesNotTravelToAnotherHop(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t)
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1")

	_, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected *HostKeyUnknownError for hop 1, got %v", err)
	}

	// The same fingerprint, attributed to a different host: the callback must
	// not take it, because the pair is what the user agreed to.
	wrongHost := acceptedHostKey{Host: "some.other.host", Fingerprint: hk.Fingerprint}
	if _, err := a.dialThroughJumps(context.Background(), target, wrongHost); !errors.As(err, &hk) {
		t.Fatalf("an acceptance for a different host must not let this hop through, got %v", err)
	}
	// And the right host with a fingerprint that is not the one presented.
	wrongFP := acceptedHostKey{Host: hk.Host, Fingerprint: "SHA256:not-the-key-you-were-shown"}
	if _, err := a.dialThroughJumps(context.Background(), target, wrongFP); !errors.As(err, &hk) {
		t.Fatalf("an acceptance carrying a different fingerprint must not let this hop through, got %v", err)
	}
	if got := countKnownHostsLines(t, a.sshKnownHostsPath); got != 0 {
		t.Fatalf("a mismatched acceptance must write nothing to known_hosts, got %d entries", got)
	}
}

// TestUnknownTargetKeyOnAChainNamesTheTarget covers the other end of the chain
// from TestUnknownHostKeyNamesTheHop: both hops are trusted and the
// *destination* is the unknown one. Its hop index has to be len(hops)+1 so the
// dialog can say "this is the machine you asked for" rather than leaving the
// user to guess from a bare fingerprint again.
func TestUnknownTargetKeyOnAChainNamesTheTarget(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srv2 := startForwardingSSHTestServerAs(t, "u2", "p2")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t, srv1, srv2) // the target is the untrusted one
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "h2", srv2, "u2", "p2", "")
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1,h2")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("an unknown target key must not connect")
	}
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected *HostKeyUnknownError, got %v", err)
	}
	if hk.HopIndex != 3 {
		t.Fatalf("HopIndex = %d, want 3 (two hops plus the target)", hk.HopIndex)
	}
	if hk.HopName != "db" {
		t.Fatalf("HopName = %q, want %q", hk.HopName, "db")
	}
	// Both hops were really opened, and both are closed again.
	waitClosed(t, srv1, 1, "hop 1 after the target's key was refused")
	waitClosed(t, srv2, 1, "hop 2 after the target's key was refused")
}

// --- hosts with no ProxyJump at all -----------------------------------------

// TestDirectHostDialsWithoutAChain is the case that will be nearly every
// connection the app makes once this path is wired in: a host with no
// ProxyJump. It must come back as a one-element chain whose Target() is the
// host itself, having dialled nothing else.
func TestDirectHostDialsWithoutAChain(t *testing.T) {
	srv := startForwardingSSHTestServerAs(t, "u", "pw")
	a := newJumpTestApp(t, srv)
	host := addServerHost(t, a, "plain", srv, "u", "pw", "")

	chain, err := a.dialThroughJumps(context.Background(), host, acceptedHostKey{})
	if err != nil {
		t.Fatalf("dialThroughJumps on a host with no ProxyJump: %v", err)
	}
	defer chain.Close()

	if len(chain.conns) != 1 {
		t.Fatalf("a host with no ProxyJump must yield exactly one connection, got %d", len(chain.conns))
	}
	if chain.targetHopIndex() != 0 {
		t.Fatalf("targetHopIndex = %d, want 0 (no chain to disambiguate)", chain.targetHopIndex())
	}
	if opened, _ := srv.counts(); opened != 1 {
		t.Fatalf("want exactly 1 connection to the host, got %d", opened)
	}
	echoHost, echoPort := startEchoTarget(t)
	remote, err := chain.Target().DialRemote("tcp", net.JoinHostPort(echoHost, echoPort))
	if err != nil {
		t.Fatalf("dial through the chain's target: %v", err)
	}
	defer remote.Close()
	echoThrough(t, remote, "hello with no jump host")
}

// TestDirectHostUnknownKeyReportsNoHop: a direct host's TOFU prompt must read
// exactly as it did before jump hosts existed. HopIndex 0 is what tells the
// dialog there is no chain to explain.
func TestDirectHostUnknownKeyReportsNoHop(t *testing.T) {
	srv := startForwardingSSHTestServerAs(t, "u", "pw")
	a := newJumpTestApp(t) // trusts nothing
	host := addServerHost(t, a, "plain", srv, "u", "pw", "")

	_, err := a.dialThroughJumps(context.Background(), host, acceptedHostKey{})
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected *HostKeyUnknownError, got %v", err)
	}
	if hk.HopIndex != 0 {
		t.Fatalf("HopIndex = %d, want 0 for a direct connection", hk.HopIndex)
	}
	if hk.Fingerprint == "" || hk.Host == "" {
		t.Fatalf("the direct case must still carry host + fingerprint, got %+v", hk)
	}
}

// TestDirectHostMissingCredentialKeepsTheSentinel: the frontend answers a bare
// errCredentialMissing by prompting for the host the user named. Wrapping it
// with chain wording — for a host that has no chain — would break that.
func TestDirectHostMissingCredentialKeepsTheSentinel(t *testing.T) {
	srv := startForwardingSSHTestServerAs(t, "u", "pw")
	a := newJumpTestApp(t, srv)
	host := addServerHost(t, a, "plain", srv, "u", "", "") // no credential saved

	_, err := a.dialThroughJumps(context.Background(), host, acceptedHostKey{})
	if err == nil || err.Error() != errCredentialMissing {
		t.Fatalf("want the bare %q sentinel, got %v", errCredentialMissing, err)
	}
	assertNoDials(t, srv, "missing credential")
}

// TestJumpHopWithoutCredentialFailsBeforeAnyDial: a hop can be a saved host and
// still have nothing in the keyring. Finding that out at dial time would mean
// the hops in front of it had already logged in for nothing, which is the same
// side-effect-before-failure the cycle and depth checks exist to avoid.
func TestJumpHopWithoutCredentialFailsBeforeAnyDial(t *testing.T) {
	srv1 := startForwardingSSHTestServerAs(t, "u1", "p1")
	srv2 := startForwardingSSHTestServerAs(t, "u2", "p2")
	srvT := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t, srv1, srv2, srvT)
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "h2", srv2, "u2", "", "") // saved, but no credential
	target := addServerHost(t, a, "db", srvT, "ut", "pt", "h1,h2")

	chain, err := a.dialThroughJumps(context.Background(), target, acceptedHostKey{})
	if err == nil {
		_ = chain.Close()
		t.Fatal("a hop with no stored credential must be refused")
	}
	if !strings.Contains(err.Error(), "h2") {
		t.Fatalf("error must name the hop whose credential is missing, got %v", err)
	}
	if err.Error() == errCredentialMissing {
		t.Fatal("a hop's missing credential must not masquerade as the target's " +
			"(the frontend would prompt for the wrong host)")
	}
	assertNoDials(t, srv1, "hop 1 while a later hop has no credential")
	assertNoDials(t, srvT, "target while a hop has no credential")
}

// countKnownHostsLines counts the entries in a known_hosts file; a missing file
// counts as zero.
func countKnownHostsLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
