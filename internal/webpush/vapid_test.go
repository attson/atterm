package webpush

import (
	"strings"
	"testing"
)

func TestGenerateVAPIDKeypairReturnsBase64URLStrings(t *testing.T) {
	priv, pub, err := generateVAPIDKeypair()
	if err != nil {
		t.Fatalf("generateVAPIDKeypair: %v", err)
	}
	if priv == "" {
		t.Fatal("private key empty")
	}
	if pub == "" {
		t.Fatal("public key empty")
	}
	// Both should be base64url-encoded (no '+' or '/' or '=' padding).
	for _, k := range []string{priv, pub} {
		if strings.ContainsAny(k, "+/=") {
			t.Fatalf("key %q is not base64url (contains +/=)", k)
		}
	}
	if priv == pub {
		t.Fatal("private and public keys are identical")
	}
}

func TestGenerateVAPIDKeypairProducesDistinctKeys(t *testing.T) {
	priv1, pub1, _ := generateVAPIDKeypair()
	priv2, pub2, _ := generateVAPIDKeypair()
	if priv1 == priv2 || pub1 == pub2 {
		t.Fatalf("two generations produced identical keys: priv same=%v pub same=%v", priv1 == priv2, pub1 == pub2)
	}
}
