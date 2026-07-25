package main

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// pairWrapAAD is the constant AAD bound to the AEAD envelope carrying an
// account_key over a QR pair handoff. Both the Go seal (desktop) and the TS
// open (mobile / web) MUST use these exact bytes verbatim. Changing this
// string requires a matching change in web/src/shared/lib/opaque.ts and
// desktop/frontend/src/lib/opaque.ts (search PAIR_WRAP_AAD).
var pairWrapAAD = []byte("atterm-pair-wrap-v1")

// wrapAccountKey seals ak into a 73-byte envelope under a freshly-generated
// 32-byte wrap key. Envelope layout:
//
//	byte 0        : cipher_id, 0x01 (XChaCha20-Poly1305)
//	bytes 1..24   : nonce (24B, random per wrap)
//	bytes 25..    : ciphertext || Poly1305 tag (48B for a 32B ak)
//
// The wrap key is returned to the caller so it can be shipped to the mobile
// via QR (never uploaded to the relay). Callers MUST NOT persist the wrap
// key beyond the QR generation call — it exists solely as ephemeral
// key-transport material.
func wrapAccountKey(ak []byte) (envelope, wrapKey []byte, err error) {
	if len(ak) != 32 {
		return nil, nil, fmt.Errorf("wrapAccountKey: account_key must be 32 bytes, got %d", len(ak))
	}
	wk := make([]byte, chacha20poly1305.KeySize) // 32
	if _, err := rand.Read(wk); err != nil {
		return nil, nil, fmt.Errorf("rand wk: %w", err)
	}

	aead, err := chacha20poly1305.NewX(wk)
	if err != nil {
		return nil, nil, fmt.Errorf("aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize()) // 24
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("rand nonce: %w", err)
	}

	env := make([]byte, 0, 1+len(nonce)+len(ak)+aead.Overhead())
	env = append(env, 0x01) // cipher_id
	env = append(env, nonce...)
	env = aead.Seal(env, nonce, ak, pairWrapAAD)
	return env, wk, nil
}
