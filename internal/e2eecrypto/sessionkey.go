// Package e2eecrypto holds the M2-and-later crypto primitives used by the
// agent and clients to encrypt terminal content (OUT/IN/PASTE_IMAGE
// frames) so the relay carries only opaque bytes. See the design spec
// at docs/superpowers/specs/2026-06-15-relay-e2ee-design.md §§5-7.
//
// Split into three files by responsibility:
//   - accountkey.go — Argon2id + XChaCha20-Poly1305 wrap/unwrap of the
//     per-account account_key by the user's password (AccountKeyWrap
//     envelope + KDFParams).
//   - sessionkey.go — HKDF-SHA256 derivation of per-session AEAD keys
//     from account_key + session_uuid.
//   - envelope.go   — the on-wire cipher_id + nonce + AEAD framing that
//     seals/opens session-level payloads.
//
// The HTTP client that speaks OPAQUE to the relay (Register / Login /
// wire-format helpers) lives in internal/e2eeclient — it now imports
// this package for wrap/unwrap rather than re-implementing it.
package e2eecrypto

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

// SessionKeySize is the length of a per-session AEAD key in bytes.
const SessionKeySize = 32

// sessionInfoPrefix is the HKDF info string that domain-separates per-
// session key derivation from any other use of account_key. Bumping the
// version suffix is a one-way migration — keys derived under v1 cannot be
// recomputed by code that looks for "v2".
const sessionInfoPrefix = "atterm-session-v1"

// DeriveSessionKey returns the per-session 32-byte key for sessionID,
// computed as HKDF-SHA256(account_key, info = "atterm-session-v1" || uuid_bytes).
// It returns ErrAccountKeyShort if account_key is shorter than 32 bytes —
// the relay's wrap blob always produces a 32-byte key, so a short input
// indicates a programming error rather than a runtime condition.
func DeriveSessionKey(accountKey []byte, sessionID uuid.UUID) ([]byte, error) {
	if len(accountKey) < SessionKeySize {
		return nil, ErrAccountKeyShort
	}
	info := make([]byte, 0, len(sessionInfoPrefix)+16)
	info = append(info, sessionInfoPrefix...)
	info = append(info, sessionID[:]...)
	r := hkdf.New(sha256.New, accountKey, nil, info)
	out := make([]byte, SessionKeySize)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("hkdf read: %w", err)
	}
	return out, nil
}

// ErrAccountKeyShort is returned when DeriveSessionKey is called with an
// account_key shorter than SessionKeySize (32) bytes. Always indicates a
// caller bug — the OPAQUE register/login flow only ever produces 32-byte
// account_keys.
var ErrAccountKeyShort = errors.New("e2eecrypto: account_key must be at least 32 bytes")
