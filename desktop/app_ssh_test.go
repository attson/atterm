package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newSSHTestApp(t *testing.T) *App {
	t.Helper()
	return &App{host: newTestRelayHost(t), ctx: context.Background()}
}

func TestNewSshSessionUnknownHostReturnsFingerprint(t *testing.T) {
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := newSSHTestApp(t)
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	_, err := a.NewSshSession(SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
	})
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError, got %v", err)
	}
	if hkErr.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
	if hkErr.Host == "" {
		t.Fatal("empty host: the retry has nothing to echo back")
	}
}

// TestNewSshSessionAcceptedHostKeyConnects walks the real TOFU round trip: the
// first attempt is refused with a fingerprint, and the retry carries back
// exactly the (host, fingerprint) pair the user was shown. That pair — not a
// bool — is what the callback matches, so this is also the test that would fail
// if the retry ever went back to "accept whatever key turns up next".
func TestNewSshSessionAcceptedHostKeyConnects(t *testing.T) {
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := newSSHTestApp(t)
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	req := SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
	}
	_, err := a.NewSshSession(req)
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError first, got %v", err)
	}

	// A fingerprint the user was never shown must not get through, even for the
	// host they *were* asked about.
	wrong := req
	wrong.AcceptedHostKeyHost = hkErr.Host
	wrong.AcceptedHostKeyFingerprint = "SHA256:not-the-key-you-were-shown"
	if _, err := a.NewSshSession(wrong); !errors.As(err, &hkErr) {
		t.Fatalf("a mismatched fingerprint must not be accepted, got %v", err)
	}

	req.AcceptedHostKeyHost = hkErr.Host
	req.AcceptedHostKeyFingerprint = hkErr.Fingerprint
	resp, err := a.NewSshSession(req)
	if err != nil {
		t.Fatalf("NewSshSession: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
	data, _ := os.ReadFile(a.sshKnownHostsPath)
	if len(data) == 0 {
		t.Fatal("known_hosts not written")
	}
}

func TestNewSshSessionByIDResolvesCredAndConnects(t *testing.T) {
	useIsolatedKeyring(t)
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	h, err := a.AddSSHHost(SSHHost{Host: host, Port: port, User: "u", AuthKind: "password"},
		sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	// First connect to an unknown host: the credential is resolved and the
	// connection proceeds far enough to hit TOFU (unknown host key), proving
	// NewSshSessionByID looked up + passed the stored credential.
	_, err = a.NewSshSessionByID(h.ID, AcceptedHostKey{})
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError (cred resolved, TOFU prompt), got %v", err)
	}
}

// TestNewSshSessionByIDAcceptedHostKeyConnects is the saved-host half of the
// TOFU round trip, and the reason NewSshSessionByID grew a second parameter.
//
// Before roadmap item 27 this method took the id alone: the prompt came back,
// the answer had nowhere to go, and the next attempt asked the same question
// forever. That also walled off the tunnel path, which refuses unknown keys
// outright and tells the user to accept the fingerprint in a terminal first —
// advice that was impossible to follow for a saved host.
//
// The acceptance has to arrive at the callback as the exact pair the user was
// shown, so the test feeds back a *wrong* fingerprint for the right host first:
// that must still be refused.
func TestNewSshSessionByIDAcceptedHostKeyConnects(t *testing.T) {
	useIsolatedKeyring(t)
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	h, err := a.AddSSHHost(SSHHost{Host: host, Port: port, User: "u", AuthKind: "password"},
		sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.NewSshSessionByID(h.ID, AcceptedHostKey{})
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError first, got %v", err)
	}
	if hkErr.Host == "" || hkErr.Fingerprint == "" {
		t.Fatalf("prompt must carry both halves to echo back, got host=%q fp=%q",
			hkErr.Host, hkErr.Fingerprint)
	}

	wrong := AcceptedHostKey{Host: hkErr.Host, Fingerprint: "SHA256:not-the-key-you-were-shown"}
	if _, err := a.NewSshSessionByID(h.ID, wrong); !errors.As(err, &hkErr) {
		t.Fatalf("a fingerprint the user was never shown must not be accepted, got %v", err)
	}

	resp, err := a.NewSshSessionByID(h.ID, AcceptedHostKey{Host: hkErr.Host, Fingerprint: hkErr.Fingerprint})
	if err != nil {
		t.Fatalf("NewSshSessionByID after accepting the shown key: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
}

func TestNewSshSessionByIDMissingCredential(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "noCred", Host: "h", User: "u", AuthKind: "password"}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("noCred", AcceptedHostKey{})
	if err == nil || err.Error() != errCredentialMissing {
		t.Fatalf("expected errCredentialMissing, got %v", err)
	}
}

func TestNewSshSessionByIDKeyAuth(t *testing.T) {
	useIsolatedKeyring(t)
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	k, err := a.AddSSHKey("k", testKeyPEM(t), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: host, Port: port, User: "u", AuthKind: "key", KeyID: k.ID}}
	_ = a.cfgStore.Set(cfg)

	_, err = a.NewSshSessionByID("h1", AcceptedHostKey{})
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected HostKeyUnknownError (key resolved), got %v", err)
	}
}

func TestNewSshSessionByIDKeyMissing(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: "gone"}}
	_ = a.cfgStore.Set(cfg)
	_, err := a.NewSshSessionByID("h1", AcceptedHostKey{})
	if err == nil || err.Error() != errKeyMissing {
		t.Fatalf("expected errKeyMissing, got %v", err)
	}
}

func TestNewSshSessionByIDSetsSSHHostID(t *testing.T) {
	useIsolatedKeyring(t)
	addr, hostPub := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	h := newTestRelayHost(t)
	// Adopt via OpenSSHSession directly with a known host id, then assert the
	// registered SessionInfo carries SSHHostID.
	id, err := h.OpenSSHSession(context.Background(), SSHConnectReq{
		Host: host, Port: port, User: "u", AuthKind: "password", Password: "pw",
		SSHHostID: "host-123",
	}, testFixedHostKeyCb(hostPub), nil)
	if err != nil {
		t.Fatalf("OpenSSHSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatal("session not registered")
	}
	if got := sess.Info().SSHHostID; got != "host-123" {
		t.Fatalf("SSHHostID = %q, want host-123", got)
	}
	_ = h.CloseSession(id)
}

// --- jump hosts on the terminal path (roadmap item 27) -----------------------

// newJumpSessionTestApp is newJumpTestApp plus the relay host a terminal
// session needs to be adopted into.
func newJumpSessionTestApp(t *testing.T, trusted ...*forwardTestServer) *App {
	t.Helper()
	a := newJumpTestApp(t, trusted...)
	a.host = newTestRelayHost(t)
	return a
}

// TestProxyJumpHostOpensASessionThroughTheBastion is the terminal half of item
// 27: the host item 25 refused now connects, and connects *through the jump
// host*.
//
// "It connected" is not the assertion — a build that ignored ProxyJump and
// dialled HostName directly would also return a session id here, since the
// target is reachable from the test process. What pins the topology is what the
// bastion was asked to reach (a direct-tcpip channel to the target's address)
// together with each machine seeing only its own credential.
func TestProxyJumpHostOpensASessionThroughTheBastion(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpSessionTestApp(t, bastion, target)
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	th := addServerHost(t, a, "db", target, "target-user", "target-pw", "bastion")

	resp, err := a.NewSshSessionByID(th.ID, AcceptedHostKey{})
	if err != nil {
		t.Fatalf("a ProxyJump host must open a terminal session: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}

	if got := bastion.directDestAddrs(); len(got) != 1 || got[0] != target.addr {
		t.Fatalf("the bastion must be asked to reach the target %s, got %q", target.addr, got)
	}
	if opened, _ := bastion.counts(); opened != 1 {
		t.Fatalf("the bastion must be dialled exactly once, got %d connection(s)", opened)
	}
	if got := bastion.authCredentials(); len(got) != 1 || got[0] != "bastion-user:bastion-pw" {
		t.Fatalf("the bastion must see only its own credential, got %q", got)
	}
	if got := target.authCredentials(); len(got) != 1 || got[0] != "target-user:target-pw" {
		t.Fatalf("the target must see only its own credential, got %q", got)
	}

	sid, err := uuid.Parse(resp.SessionID)
	if err != nil {
		t.Fatalf("session id %q: %v", resp.SessionID, err)
	}
	if _, ok := a.host.server.Registry().Get(sid); !ok {
		t.Fatal("the session was not adopted into the registry")
	}

	// Closing the session closes the chain behind it. Session.Close only closes
	// the target connection, so without the chain being owned somewhere the
	// bastion login would outlive the terminal the user just closed.
	if err := a.host.CloseSession(sid); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	waitClosed(t, bastion, 1, "the bastion connection after the session was closed")
}

// TestProxyJumpSessionDialFailureClosesTheChain: the hops are up and the
// *target's* dial then fails (its host key is unknown, so the shell is never
// opened). The chain has to be closed on that path — the bastion connection is
// a real login that nothing downstream exists to close, and a user who accepts
// the fingerprint and retries would otherwise stack up one hanging bastion
// session per attempt.
func TestProxyJumpSessionDialFailureClosesTheChain(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpSessionTestApp(t, bastion) // the target's key is deliberately unknown
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	th := addServerHost(t, a, "db", target, "target-user", "target-pw", "bastion")

	_, err := a.NewSshSessionByID(th.ID, AcceptedHostKey{})
	var hk *HostKeyUnknownError
	if !errors.As(err, &hk) {
		t.Fatalf("expected *HostKeyUnknownError for the target, got %v", err)
	}
	if hk.HopIndex != 2 {
		t.Fatalf("HopIndex = %d, want 2 (one hop plus the target)", hk.HopIndex)
	}
	if hk.HopName != "db" {
		t.Fatalf("HopName = %q, want %q", hk.HopName, "db")
	}
	if opened, _ := bastion.counts(); opened != 1 {
		t.Fatalf("the bastion must really have been dialled, got %d connection(s)", opened)
	}
	waitClosed(t, bastion, 1, "the bastion connection after the target's dial failed")
}

// TestRequestAcceptedHostKeyIsScopedToOneHop is the same rule as
// TestAcceptedHostKeyIsScopedToOneHop, asserted one layer up — at the request
// boundary, where the acceptance arrives from the frontend.
//
// That boundary used to be a bool ("accept the next unknown key"), and a bool
// there is a Critical the moment a connection can run through a chain:
// KnownHostsCallback *appends* an accepted key to known_hosts, so accepting the
// bastion would silently record the target's key too — and a substituted target
// would then never prompt again. So each round here must move the prompt on to
// the next machine rather than silencing it, and known_hosts must only ever
// grow by the entry the user actually agreed to.
func TestRequestAcceptedHostKeyIsScopedToOneHop(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpSessionTestApp(t) // trusts nothing at all
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	th := addServerHost(t, a, "db", target, "target-user", "target-pw", "bastion")

	req := SSHConnectReq{
		Host: th.Host, Port: th.Port, User: th.User,
		AuthKind: "password", Password: "target-pw",
		SSHHostID: th.ID,
	}
	for _, want := range []struct {
		hop  int
		name string
	}{{1, "bastion"}, {2, "db"}} {
		_, err := a.NewSshSession(req)
		var hk *HostKeyUnknownError
		if !errors.As(err, &hk) {
			t.Fatalf("hop %d (%s): expected *HostKeyUnknownError, got %v", want.hop, want.name, err)
		}
		if hk.HopIndex != want.hop || hk.HopName != want.name {
			t.Fatalf("prompt was for hop %d %q, want hop %d %q",
				hk.HopIndex, hk.HopName, want.hop, want.name)
		}
		if got := countKnownHostsLines(t, a.sshKnownHostsPath); got != want.hop-1 {
			t.Fatalf("before accepting hop %d, known_hosts holds %d entries, want %d",
				want.hop, got, want.hop-1)
		}
		req.AcceptedHostKeyHost = hk.Host
		req.AcceptedHostKeyFingerprint = hk.Fingerprint
	}

	resp, err := a.NewSshSession(req)
	if err != nil {
		t.Fatalf("accepting the last unknown key must complete the connection: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
	if got := countKnownHostsLines(t, a.sshKnownHostsPath); got != 2 {
		t.Fatalf("known_hosts holds %d entries after accepting both, want 2", got)
	}
}

// TestProxyJumpToAnUnsavedHopIsRefusedWithoutDialling replaces item 25's
// blanket refusal with §5.1's: a ProxyJump host is no longer refused for having
// a ProxyJump, but a hop that is not a saved host still is — that is the only
// honest answer to "where does the bastion's credential come from".
//
// The dial count is what makes it more than "some error came back": the target
// is a real, reachable server with a valid stored credential, so a build that
// resolved hops lazily would connect to it and the count would be 1.
func TestProxyJumpToAnUnsavedHopIsRefusedWithoutDialling(t *testing.T) {
	srv := startForwardingSSHTestServerAs(t, "u", "pw")

	a := newJumpSessionTestApp(t, srv)
	h := addServerHost(t, a, "db", srv, "u", "pw", "bastion") // "bastion" is not saved

	_, err := a.NewSshSessionByID(h.ID, AcceptedHostKey{})
	if err == nil {
		t.Fatal("a ProxyJump naming an unsaved host must be refused")
	}
	if !strings.Contains(err.Error(), "bastion") {
		t.Fatalf("error must name the unresolved hop, got %v", err)
	}
	if !strings.Contains(err.Error(), "add") {
		t.Fatalf("error must tell the user to add the hop as a host, got %v", err)
	}
	var hkErr *HostKeyUnknownError
	if errors.As(err, &hkErr) {
		t.Fatalf("the refusal must come before any dial: got a TOFU prompt (%v)", err)
	}
	if opened, _ := srv.counts(); opened != 0 {
		t.Fatalf("nothing may be dialled for an unresolvable chain, server saw %d connection(s)", opened)
	}
}

// TestProxyJumpThroughAProxyCommandHopIsRefused is the same rule as
// TestJumpHopWithProxyCommandIsRefusedBeforeAnyDial, asserted where the user
// meets it: the gate refuses a ProxyCommand host with "this host cannot be
// connected directly", and that sentence has to stay true for a machine reached
// as somebody else's hop. Clicking "db" must not dial the bastion's
// HostName:Port behind the promise made about clicking the bastion.
func TestProxyJumpThroughAProxyCommandHopIsRefused(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpSessionTestApp(t, bastion, target)
	addProxyCommandHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "corkscrew proxy 8080 %h %p")
	th := addServerHost(t, a, "db", target, "target-user", "target-pw", "bastion")

	_, err := a.NewSshSessionByID(th.ID, AcceptedHostKey{})
	if err == nil {
		t.Fatal("a chain through a ProxyCommand host must be refused")
	}
	if !strings.Contains(err.Error(), "bastion") || !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("error must name the hop and the reason, got %v", err)
	}
	var hkErr *HostKeyUnknownError
	if errors.As(err, &hkErr) {
		t.Fatalf("the refusal must come before any dial: got a TOFU prompt (%v)", err)
	}
	assertNoDials(t, bastion, "the hop carrying a ProxyCommand")
	assertNoDials(t, target, "the destination behind a ProxyCommand hop")
}

// TestProxyJumpHostWithoutCredentialFailsBeforeAnyDial: the destination's own
// credential is checked before the first hop is dialled, and the error stays the
// bare errCredentialMissing sentinel the frontend answers by prompting for this
// host. Discovering it after logging into a bastion would spend exactly the side
// effect the static checks exist to avoid.
func TestProxyJumpHostWithoutCredentialFailsBeforeAnyDial(t *testing.T) {
	bastion := startForwardingSSHTestServerAs(t, "bastion-user", "bastion-pw")
	target := startForwardingSSHTestServerAs(t, "target-user", "target-pw")

	a := newJumpSessionTestApp(t, bastion, target)
	addServerHost(t, a, "bastion", bastion, "bastion-user", "bastion-pw", "")
	th := addServerHost(t, a, "db", target, "target-user", "", "bastion") // no credential saved

	_, err := a.NewSshSessionByID(th.ID, AcceptedHostKey{})
	if err == nil || err.Error() != errCredentialMissing {
		t.Fatalf("want the bare %q sentinel, got %v", errCredentialMissing, err)
	}
	assertNoDials(t, bastion, "the bastion while the destination has no credential")
}

// TestProxyCommandGateFiresBeforeCredentialRead is the discriminator for the
// arm of the gate that still refuses. A gate moved down to just above
// a.NewSshSession(req) — below the `switch found.AuthKind` credential read —
// would return errCredentialMissing for this host (nothing is in the keyring)
// instead of naming ProxyCommand. Only a gate positioned strictly before the
// credential switch names the reason here.
func TestProxyCommandGateFiresBeforeCredentialRead(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{
		ID: "p2", Alias: "db", Host: "10.0.0.5", User: "root", AuthKind: "password",
		ProxyCommand: "corkscrew proxy 8080 %h %p",
	}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("p2", AcceptedHostKey{})
	if err == nil {
		t.Fatal("must refuse to connect a ProxyCommand host")
	}
	if err.Error() == errCredentialMissing {
		t.Fatal("gate must fire before the credential read: got errCredentialMissing, meaning the AuthKind switch ran first")
	}
	if !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("error must name the reason, got %v", err)
	}
}

// TestProxyCommandHostErrorNamesProxyCommand covers the other arm of the
// gate. A host that only sets ProxyCommand used to be told it "needs a jump
// host (ProxyJump \"\")" — a config line it does not have, quoting an empty
// value — sending the user to look for the wrong thing.
func TestProxyCommandHostErrorNamesProxyCommand(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{
		ID: "p3", Alias: "via-corkscrew", Host: "10.0.0.6", User: "root",
		AuthKind: "password", ProxyCommand: "corkscrew proxy 8080 %h %p",
	}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("p3", AcceptedHostKey{})
	if err == nil {
		t.Fatal("must refuse to connect a ProxyCommand host directly")
	}
	if !strings.Contains(err.Error(), "ProxyCommand") {
		t.Fatalf("error must name ProxyCommand, got %v", err)
	}
	if strings.Contains(err.Error(), "ProxyJump") {
		t.Fatalf("error must not name a ProxyJump this host does not have, got %v", err)
	}
}
