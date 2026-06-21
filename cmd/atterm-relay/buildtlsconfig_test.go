package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestCert generates a throwaway ECDSA cert/key pair and writes them to
// dir, returning the two file paths.
func writeTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("createcert: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshalkey: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestBuildTLSConfig_RequiresCertAndKey(t *testing.T) {
	if _, err := buildTLSConfig("", ""); err == nil {
		t.Fatal("missing cert+key must error (no self-signed fallback)")
	}
	if _, err := buildTLSConfig("/some/cert.pem", ""); err == nil {
		t.Fatal("missing key must error")
	}
	if _, err := buildTLSConfig("", "/some/key.pem"); err == nil {
		t.Fatal("missing cert must error")
	}
}

func TestBuildTLSConfig_BadPathErrors(t *testing.T) {
	if _, err := buildTLSConfig("/nope/cert.pem", "/nope/key.pem"); err == nil {
		t.Fatal("unreadable cert/key must error")
	}
}

func TestBuildTLSConfig_ValidCert(t *testing.T) {
	certPath, keyPath := writeTestCert(t, t.TempDir())
	cfg, err := buildTLSConfig(certPath, keyPath)
	if err != nil {
		t.Fatalf("valid cert/key: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("want one loaded cert, got %+v", cfg)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x; want TLS1.2 (%x)", cfg.MinVersion, tls.VersionTLS12)
	}
}
