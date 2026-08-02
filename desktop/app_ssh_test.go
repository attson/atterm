package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
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
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
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
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	a := &App{host: newTestRelayHost(t), cfgStore: newTestConfigStore(t), ctx: context.Background()}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "noCred", Host: "h", User: "u", AuthKind: "password"}}
	_ = a.cfgStore.Set(cfg)

	_, err := a.NewSshSessionByID("noCred")
	if err == nil || err.Error() != errCredentialMissing {
		t.Fatalf("expected errCredentialMissing, got %v", err)
	}
}
