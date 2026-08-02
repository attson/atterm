package sshclient

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	s, _ := ssh.NewSignerFromKey(k)
	return s.PublicKey()
}

func TestKnownHostsUnknownTriggersTOFUAndPersists(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	pub := testHostKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	var askedFingerprint string
	cb := KnownHostsCallback(khPath, func(host, fp string) bool {
		askedFingerprint = fp
		return true // accept
	})

	// First: unknown → onUnknown → accept → persist, returns nil error.
	if err := cb("127.0.0.1:2222", addr, pub); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if askedFingerprint == "" {
		t.Fatal("onUnknown not called")
	}

	// Second: already persisted → no ask, allowed.
	cb2 := KnownHostsCallback(khPath, func(host, fp string) bool {
		t.Fatal("should not ask again")
		return false
	})
	if err := cb2("127.0.0.1:2222", addr, pub); err != nil {
		t.Fatalf("second connect: %v", err)
	}
}

func TestKnownHostsMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	pub1 := testHostKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	// Persist pub1 first.
	accept := KnownHostsCallback(khPath, func(string, string) bool { return true })
	if err := accept("127.0.0.1:2222", addr, pub1); err != nil {
		t.Fatal(err)
	}

	// Switch to pub2 → mismatch → must be rejected even if onUnknown says yes.
	pub2 := testHostKey(t)
	cb := KnownHostsCallback(khPath, func(string, string) bool { return true })
	if err := cb("127.0.0.1:2222", addr, pub2); err == nil {
		t.Fatal("mismatch must be rejected, got nil error")
	}
}

func TestKnownHostsRejectWhenUserDeclines(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	pub := testHostKey(t)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	cb := KnownHostsCallback(khPath, func(string, string) bool { return false })
	if err := cb("127.0.0.1:2222", addr, pub); err == nil {
		t.Fatal("declined host key must be rejected")
	}
	// Nothing should have been written.
	data, _ := os.ReadFile(khPath)
	if len(data) != 0 {
		t.Fatalf("known_hosts should stay empty, got %q", data)
	}
}
