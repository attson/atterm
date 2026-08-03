package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
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
