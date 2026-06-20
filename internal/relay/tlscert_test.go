package relay

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSelfSigned_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	cert, err := LoadOrCreateSelfSigned(dir, []string{"121.43.40.128", "relay.example.com"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Files written with the right perms.
	keyInfo, err := os.Stat(filepath.Join(dir, "tls", "key.pem"))
	if err != nil {
		t.Fatalf("key not written: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Errorf("key perm = %v; want 0600", keyInfo.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "tls", "cert.pem")); err != nil {
		t.Fatalf("cert not written: %v", err)
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	// SANs: loopback + supplied IP + supplied DNS.
	if err := leaf.VerifyHostname("121.43.40.128"); err != nil {
		t.Errorf("missing IP SAN: %v", err)
	}
	if err := leaf.VerifyHostname("relay.example.com"); err != nil {
		t.Errorf("missing DNS SAN: %v", err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("missing localhost SAN: %v", err)
	}
	hasLoopback := false
	for _, ip := range leaf.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Error("missing 127.0.0.1 SAN")
	}
}

func TestLoadOrCreateSelfSigned_ReusesPersisted(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateSelfSigned(dir, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateSelfSigned(dir, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	// Same serial / bytes → the second call reused the persisted cert rather
	// than minting a new one (so the browser's one-time trust survives).
	a, _ := x509.ParseCertificate(first.Certificate[0])
	b, _ := x509.ParseCertificate(second.Certificate[0])
	if a.SerialNumber.Cmp(b.SerialNumber) != 0 {
		t.Fatalf("cert regenerated on reuse: serials %s vs %s", a.SerialNumber, b.SerialNumber)
	}
}

func TestLoadOrCreateSelfSigned_RegeneratesOnCorruption(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateSelfSigned(dir, []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the persisted cert → next call must regenerate, not error.
	if err := os.WriteFile(filepath.Join(dir, "tls", "cert.pem"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateSelfSigned(dir, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("regenerate after corruption: %v", err)
	}
	a, _ := x509.ParseCertificate(first.Certificate[0])
	b, _ := x509.ParseCertificate(second.Certificate[0])
	if a.SerialNumber.Cmp(b.SerialNumber) == 0 {
		t.Fatal("expected a fresh cert after corruption")
	}
}
