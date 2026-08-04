package e2eecrypto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/chacha20poly1305"
)

// CipherID discriminates the AEAD algorithm used for a sealed envelope.
// Reserved for future cipher agility. M2 ships only CipherXChaCha20Poly1305.
type CipherID byte

const (
	// CipherXChaCha20Poly1305 is the spec §6 default: 24-byte nonces, 16-byte
	// Poly1305 tags. Picked because the wide nonce can be filled from
	// crypto/rand without a per-key counter and because portable libraries
	// exist on every target runtime (Go, Rust, TS via @noble).
	CipherXChaCha20Poly1305 CipherID = 0x01
)

// EnvelopePrefixSize is the byte count of the cipher_id (1) + nonce (24)
// prefix on every sealed envelope. Total wire size = EnvelopePrefixSize +
// len(plaintext) + 16 (Poly1305 tag).
const EnvelopePrefixSize = 1 + chacha20poly1305.NonceSizeX

// minEnvelopeLen is EnvelopePrefixSize + Poly1305 tag length (16). A buffer
// shorter than this cannot possibly be a well-formed sealed envelope.
const minEnvelopeLen = EnvelopePrefixSize + 16

// LooksLikeSealed heuristically detects whether data is a sealed envelope
// (as produced by SealOut / SealUnsequenced): cipher_id 0x01 at byte 0 AND
// at least the minimum envelope size. The check is intentionally
// heuristic — a plaintext chunk starting with 0x01 (Ctrl-A) AND at least
// 41 bytes long would false-positive, but Ctrl-A keystrokes are single-byte
// chunks in practice, so the overlap is vanishingly small. A false positive
// only suppresses downstream OSC parsing / plaintext handling, which is the
// right side to err on under the E2EE threat model.
//
// Shared by the relay's inbound OUT gate and the desktop's unsequenced
// envelope guard so both paths use the same definition of "looks sealed".
func LooksLikeSealed(data []byte) bool {
	if len(data) < minEnvelopeLen {
		return false
	}
	return data[0] == byte(CipherXChaCha20Poly1305)
}

// Sentinel errors. ErrInvalidEnvelope covers structural problems
// (truncated, unknown cipher ID); ErrAuthFailed covers AEAD tag mismatch
// (tampered ciphertext, wrong key, wrong AAD). Callers usually want to
// treat both as "drop the frame and reconnect" — but they distinguish so
// debug logs can tell apart "we sent bad bytes" from "key/AAD wrong".
var (
	ErrInvalidEnvelope = errors.New("e2eecrypto: invalid envelope")
	ErrAuthFailed      = errors.New("e2eecrypto: authentication failed")
)

// SealOut encrypts a single TypeOut chunk. The AAD is constructed per
// spec §6.1 as session_uuid (16B) || frame_type (1B) || seq (8B BE) so
// the same ciphertext cannot be replayed under a different session id
// or a different sequence number. frameType is passed explicitly rather
// than hardcoded so callers can reuse this helper for any seq-bound
// frame (currently only TypeOut, but TypeIn could opt in later).
func SealOut(sessionKey []byte, sessionID uuid.UUID, frameType byte, seq uint64, plaintext []byte) ([]byte, error) {
	aad := outAAD(sessionID, frameType, seq)
	return seal(sessionKey, aad, plaintext)
}

// OpenOut is the inverse of SealOut.
func OpenOut(sessionKey []byte, sessionID uuid.UUID, frameType byte, seq uint64, envelope []byte) ([]byte, error) {
	aad := outAAD(sessionID, frameType, seq)
	return open(sessionKey, aad, envelope)
}

// SealUnsequenced encrypts a frame that is not bound to a monotonic seq —
// currently TypeIn, TypePasteImage, and TypePasteFile. The AAD is
// session_uuid || frame_type only; per-frame uniqueness is delegated to
// the 24-byte random nonce.
func SealUnsequenced(sessionKey []byte, sessionID uuid.UUID, frameType byte, plaintext []byte) ([]byte, error) {
	aad := unsequencedAAD(sessionID, frameType)
	return seal(sessionKey, aad, plaintext)
}

// OpenUnsequenced is the inverse of SealUnsequenced.
func OpenUnsequenced(sessionKey []byte, sessionID uuid.UUID, frameType byte, envelope []byte) ([]byte, error) {
	aad := unsequencedAAD(sessionID, frameType)
	return open(sessionKey, aad, envelope)
}

// outAAD builds session_uuid || frame_type || seq_be64. Splitting this
// out from SealOut makes the AAD scheme grep-able and lets tests reach
// the exact bytes that get bound to the ciphertext.
func outAAD(sessionID uuid.UUID, frameType byte, seq uint64) []byte {
	aad := make([]byte, 16+1+8)
	copy(aad[0:16], sessionID[:])
	aad[16] = frameType
	binary.BigEndian.PutUint64(aad[17:25], seq)
	return aad
}

func unsequencedAAD(sessionID uuid.UUID, frameType byte) []byte {
	aad := make([]byte, 16+1)
	copy(aad[0:16], sessionID[:])
	aad[16] = frameType
	return aad
}

func seal(sessionKey, aad, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("aead: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("rand nonce: %w", err)
	}
	envelope := make([]byte, 0, EnvelopePrefixSize+len(plaintext)+aead.Overhead())
	envelope = append(envelope, byte(CipherXChaCha20Poly1305))
	envelope = append(envelope, nonce...)
	envelope = aead.Seal(envelope, nonce, plaintext, aad)
	return envelope, nil
}

func open(sessionKey, aad, envelope []byte) ([]byte, error) {
	if len(envelope) < EnvelopePrefixSize {
		return nil, ErrInvalidEnvelope
	}
	if CipherID(envelope[0]) != CipherXChaCha20Poly1305 {
		return nil, fmt.Errorf("%w: unknown cipher_id 0x%02x", ErrInvalidEnvelope, envelope[0])
	}
	aead, err := chacha20poly1305.NewX(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("aead: %w", err)
	}
	nonce := envelope[1 : 1+aead.NonceSize()]
	ciphertext := envelope[1+aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthFailed
	}
	return plaintext, nil
}
