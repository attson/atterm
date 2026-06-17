package userstore

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestSecretCipher_RoundTrip(t *testing.T) {
	c, err := NewSecretCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewSecretCipher: %v", err)
	}
	for _, plain := range []string{"", "a", "app_secret_token_123", "中文 ☃"} {
		ct, err := c.Encrypt([]byte(plain))
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt %q: %v", plain, err)
		}
		if string(got) != plain {
			t.Fatalf("round-trip mismatch: want %q got %q", plain, string(got))
		}
	}
}

func TestSecretCipher_DistinctNonces(t *testing.T) {
	c, _ := NewSecretCipher(testKey(t))
	a, _ := c.Encrypt([]byte("same"))
	b, _ := c.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatalf("encrypting same plaintext twice produced identical ciphertext")
	}
}

func TestSecretCipher_WrongKeyFails(t *testing.T) {
	c1, _ := NewSecretCipher(testKey(t))
	c2, _ := NewSecretCipher(testKey(t))
	ct, _ := c1.Encrypt([]byte("hello"))
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatalf("decrypt with wrong key should fail")
	}
}

func TestSecretCipher_BadKeyLength(t *testing.T) {
	if _, err := NewSecretCipher(make([]byte, 16)); err == nil {
		t.Fatalf("16-byte key should be rejected")
	}
}
