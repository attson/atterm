package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		AcceptHostKey: false,
	})
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError, got %v", err)
	}
	if hkErr.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestNewSshSessionAcceptHostKeyConnects(t *testing.T) {
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := newSSHTestApp(t)
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	resp, err := a.NewSshSession(SSHConnectReq{
		Host: host, Port: port, User: "u",
		AuthKind: "password", Password: "pw",
		AcceptHostKey: true,
	})
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
	_, err = a.NewSshSessionByID(h.ID)
	var hkErr *HostKeyUnknownError
	if !errors.As(err, &hkErr) {
		t.Fatalf("expected HostKeyUnknownError (cred resolved, TOFU prompt), got %v", err)
	}
}

func TestNewSshSessionByIDMissingCredential(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "noCred", Host: "h", User: "u", AuthKind: "password"}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("noCred")
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

	_, err = a.NewSshSessionByID("h1")
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
	_, err := a.NewSshSessionByID("h1")
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
		AcceptHostKey: true, SSHHostID: "host-123",
	}, testFixedHostKeyCb(hostPub))
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

// TestProxyJumpHostRefusesDirectConnect is the core assertion for the
// direct-connect gate. It points the host at a *real, reachable* SSH test
// server and gives it a valid stored credential — so if the gate did not
// fire before the dial, NewSshSessionByID would proceed exactly like
// TestNewSshSessionByIDResolvesCredAndConnects and return
// *HostKeyUnknownError (TOFU prompt), which only happens after a dial
// attempt. Asserting that error type is absent, together with the
// ProxyJump-naming error that IS returned, proves no dial occurred —
// not just that some error came back.
func TestProxyJumpHostRefusesDirectConnect(t *testing.T) {
	useIsolatedKeyring(t)
	addr, _ := startSSHTestServer(t)
	host, port, _ := net.SplitHostPort(addr)

	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}
	a.sshKnownHostsPath = filepath.Join(t.TempDir(), "known_hosts")

	h, err := a.AddSSHHost(
		SSHHost{Host: host, Port: port, User: "u", AuthKind: "password", ProxyJump: "bastion"},
		sshCredential{Password: "pw"},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.NewSshSessionByID(h.ID)
	if err == nil {
		t.Fatal("must refuse to connect a ProxyJump host directly")
	}
	if !strings.Contains(err.Error(), "ProxyJump") {
		t.Fatalf("error must name the reason, got %v", err)
	}
	var hkErr *HostKeyUnknownError
	if errors.As(err, &hkErr) {
		t.Fatalf("gate must return before any dial: got a TOFU prompt (%v), meaning a dial was attempted", err)
	}
}

// TestProxyJumpGateFiresBeforeCredentialRead is the discriminator the
// TOFU-based test above can't provide on its own: that test proves the gate
// runs before the *dial*, but a gate moved down to just above
// a.NewSshSession(req) — i.e. below the `switch found.AuthKind` credential
// read — would still pass it (the switch would fail first, with
// errCredentialMissing, before ever reaching the moved gate... except here
// there's no stored credential at all, so a gate-after-switch build returns
// errCredentialMissing instead of the ProxyJump error). Only a gate
// positioned strictly before the credential switch returns the ProxyJump
// error in this exact setup: a ProxyJump host with nothing in the keyring.
func TestProxyJumpGateFiresBeforeCredentialRead(t *testing.T) {
	useIsolatedKeyring(t)
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "p2", Alias: "db", Host: "10.0.0.5", User: "root", AuthKind: "password", ProxyJump: "bastion"}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("p2")
	if err == nil {
		t.Fatal("must refuse to connect a ProxyJump host directly")
	}
	if err.Error() == errCredentialMissing {
		t.Fatal("gate must fire before the credential read: got errCredentialMissing, meaning the AuthKind switch ran first")
	}
	if !strings.Contains(err.Error(), "ProxyJump") {
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

	_, err := a.NewSshSessionByID("p3")
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
