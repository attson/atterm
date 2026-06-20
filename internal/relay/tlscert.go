package relay

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// selfSignedTTL is how long a generated cert is valid. Long, because this is
// a self-hosted self-signed cert the operator accepts once in the browser —
// re-prompting every year is pure annoyance with no security gain.
const selfSignedTTL = 10 * 365 * 24 * time.Hour

// LoadOrCreateSelfSigned returns a TLS certificate for the relay's built-in
// HTTPS listener. It loads <dir>/tls/{cert.pem,key.pem} if present, valid, and
// unexpired; otherwise it generates a fresh self-signed ECDSA cert covering
// hosts (plus loopback) and persists it so the cert is stable across restarts
// (so the browser's one-time "untrusted" acceptance keeps working).
//
// This is NOT a CA-trusted cert — browsers warn once. It exists so OPAQUE's
// WebCrypto (secure-context-only) works out of the box without a reverse
// proxy. Operators wanting no warning bring their own cert (ATTERM_TLS_CERT/
// KEY) or front the HTTP port with Caddy/Tailscale.
func LoadOrCreateSelfSigned(dir string, hosts []string) (tls.Certificate, error) {
	tlsDir := filepath.Join(dir, "tls")
	certPath := filepath.Join(tlsDir, "cert.pem")
	keyPath := filepath.Join(tlsDir, "key.pem")

	if cert, ok := loadValidCert(certPath, keyPath); ok {
		return cert, nil
	}

	cert, certPEM, keyPEM, err := generateSelfSigned(hosts)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("create tls dir: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write key: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, fmt.Errorf("write cert: %w", err)
	}
	return cert, nil
}

// loadValidCert returns the persisted cert if both files exist, parse, and the
// leaf is not within a day of expiry. Any problem returns ok=false so the
// caller regenerates.
func loadValidCert(certPath, keyPath string) (tls.Certificate, bool) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, false
	}
	if time.Now().After(leaf.NotAfter.Add(-24 * time.Hour)) {
		return tls.Certificate{}, false
	}
	cert.Leaf = leaf
	return cert, true
}

func generateSelfSigned(hosts []string) (cert tls.Certificate, certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("serial: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "atterm-relay self-signed"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(selfSignedTTL),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	// Always cover loopback so an ssh tunnel / localhost works, then add the
	// operator-supplied hosts (IPs → IPAddresses, names → DNSNames).
	for _, h := range append([]string{"localhost", "127.0.0.1", "::1"}, hosts...) {
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = appendUniqueIP(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = appendUniqueOrigin(tmpl.DNSNames, h)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("marshal key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	loaded, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("load generated pair: %w", err)
	}
	return loaded, certPEM, keyPEM, nil
}

func appendUniqueIP(ips []net.IP, ip net.IP) []net.IP {
	for _, e := range ips {
		if e.Equal(ip) {
			return ips
		}
	}
	return append(ips, ip)
}
