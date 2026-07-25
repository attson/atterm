package main

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestWrapAccountKey_RoundTrip(t *testing.T) {
	ak := bytes.Repeat([]byte{0x11}, 32)
	env, wk, err := wrapAccountKey(ak)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if len(wk) != 32 {
		t.Fatalf("wk len = %d, want 32", len(wk))
	}
	if len(env) != 1+24+32+16 {
		t.Fatalf("env len = %d, want 73", len(env))
	}
	if env[0] != 0x01 {
		t.Fatalf("cipher_id = 0x%02x, want 0x01", env[0])
	}

	// Decrypt with the same helper the TS side will call: cipher_id peeled,
	// 24-byte nonce, XChaCha20-Poly1305, AAD = "atterm-pair-wrap-v1".
	aead, err := chacha20poly1305.NewX(wk)
	if err != nil {
		t.Fatalf("aead: %v", err)
	}
	nonce := env[1:25]
	ct := env[25:]
	got, err := aead.Open(nil, nonce, ct, []byte("atterm-pair-wrap-v1"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, ak) {
		t.Fatalf("plaintext mismatch")
	}
}

func TestWrapAccountKey_FreshWKPerCall(t *testing.T) {
	ak := bytes.Repeat([]byte{0x22}, 32)
	_, wk1, _ := wrapAccountKey(ak)
	_, wk2, _ := wrapAccountKey(ak)
	if bytes.Equal(wk1, wk2) {
		t.Fatal("wrap_key reused across calls")
	}
}
