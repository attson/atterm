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

	chain, err := a.dialThroughJumps(context.Background(), target, false)
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

	chain, err := a.dialThroughJumps(context.Background(), targetHost, false)
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

	chain, err := a.dialThroughJumps(context.Background(), targetHost, false)
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

	chain, err := a.dialThroughJumps(context.Background(), target, false)
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

	chain, err := a.dialThroughJumps(context.Background(), hostA, false)
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

	chain, err := a.dialThroughJumps(context.Background(), target, false)
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
	deadHost, deadPort := deadAddr(t)

	a := newJumpTestApp(t, srv1, srv2)
	addServerHost(t, a, "h1", srv1, "u1", "p1", "")
	addServerHost(t, a, "h2", srv2, "u2", "p2", "")
	addSavedHost(t, a, "h3", deadHost, deadPort, "u3", "p3", "")
	target := addServerHost(t, a, "db", srv1, "u1", "p1", "h1,h2,h3")

	chain, err := a.dialThroughJumps(context.Background(), target, false)
	if err == nil {
		_ = chain.Close()
		t.Fatal("a chain whose third hop is unreachable must fail")
	}
	if !strings.Contains(err.Error(), "h3") {
		t.Fatalf("error must name the hop that failed, got %v", err)
	}
	waitClosed(t, srv1, 1, "hop 1 after a later hop failed")
	waitClosed(t, srv2, 1, "hop 2 after a later hop failed")
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

	chain, err := a.dialThroughJumps(context.Background(), target, false)
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

// TestAcceptedHostKeyConnectsThroughTheChain is the other half of TOFU: once
// the user accepts, every hop's key is recorded under its own host:port, so a
// second connect asks nothing. It also pins design risk #3 — the target's entry
// is keyed the same whether it was reached directly or through a jump host, so
// the user is not asked twice for the same machine.
func TestAcceptedHostKeyConnectsThroughTheChain(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "ub", "pb")
	target := startForwardingSSHTestServerAs(t, "ut", "pt")

	a := newJumpTestApp(t) // trusts nothing
	addServerHost(t, a, "bastion", bastion, "ub", "pb", "")
	targetHost := addServerHost(t, a, "db", target, "ut", "pt", "bastion")

	chain, err := a.dialThroughJumps(context.Background(), targetHost, true)
	if err != nil {
		t.Fatalf("dialThroughJumps with acceptHostKey: %v", err)
	}
	_ = chain.Close()

	// Both keys are now on file: a second connect needs no acceptance at all.
	chain2, err := a.dialThroughJumps(context.Background(), targetHost, false)
	if err != nil {
		t.Fatalf("second connect must not prompt again: %v", err)
	}
	_ = chain2.Close()
}
