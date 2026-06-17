package userstore

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// SecretCipher wraps a single ChaCha20-Poly1305 AEAD with a random
// per-message nonce. Output layout: nonce(12B) || ciphertext || tag(16B).
//
// One cipher per relay process; key sourced from the
// ATTERM_FEISHU_ENCRYPT_KEY env var by the relay main. Missing /
// wrong-length key fails fast at startup — there is no plaintext fallback.
type SecretCipher struct {
	aead cipher.AEAD
}

// NewSecretCipher constructs a cipher from a 32-byte key.
func NewSecretCipher(key []byte) (*SecretCipher, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("secret cipher: key length must be %d, got %d", chacha20poly1305.KeySize, len(key))
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("chacha20poly1305.New: %w", err)
	}
	return &SecretCipher{aead: aead}, nil
}

// Encrypt returns nonce||ct||tag.
func (c *SecretCipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	out := c.aead.Seal(nonce, nonce, plain, nil)
	return out, nil
}

// Decrypt inverts Encrypt.
func (c *SecretCipher) Decrypt(blob []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	minLen := n + c.aead.Overhead()
	if len(blob) < minLen {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := blob[:n], blob[n:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("aead open: %w", err)
	}
	return plain, nil
}
