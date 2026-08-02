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
