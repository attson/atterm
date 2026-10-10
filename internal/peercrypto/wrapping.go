package peercrypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
)

const (
	WrappingPrivateKeySize = 32
	WrappingPublicKeySize  = 65
)

var ErrInvalidWrappingKey = errors.New("peercrypto: invalid wrapping key")

// WrappingIdentity owns a P-256 ECDH key used only to unwrap Peer Space epoch
// keys. It is deliberately distinct from the ECDSA signing Identity because
// browser signing keys are non-exportable and restricted to sign/verify.
type WrappingIdentity struct {
	private *ecdh.PrivateKey
}

// GenerateWrappingIdentity creates a new static per-device wrapping key.
func GenerateWrappingIdentity() (*WrappingIdentity, error) {
	private, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 wrapping identity: %w", err)
	}
	return &WrappingIdentity{private: private}, nil
}

// ParseWrappingIdentity reconstructs a wrapping identity from secure storage.
func ParseWrappingIdentity(privateBytes []byte) (*WrappingIdentity, error) {
	if len(privateBytes) != WrappingPrivateKeySize {
		return nil, fmt.Errorf("%w: private key length %d", ErrInvalidWrappingKey, len(privateBytes))
	}
	private, err := ecdh.P256().NewPrivateKey(privateBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: private key: %v", ErrInvalidWrappingKey, err)
	}
	return &WrappingIdentity{private: private}, nil
}

// PrivateBytes returns a detached fixed-width scalar for secure storage.
func (i *WrappingIdentity) PrivateBytes() []byte {
	if i == nil || i.private == nil {
		return nil
	}
	return append([]byte(nil), i.private.Bytes()...)
}

// PublicBytes returns the uncompressed SEC1 key supported by WebCrypto ECDH.
func (i *WrappingIdentity) PublicBytes() []byte {
	if i == nil || i.private == nil {
		return nil
	}
	return append([]byte(nil), i.private.PublicKey().Bytes()...)
}

// ECDH derives a shared secret with another wrapping public key.
func (i *WrappingIdentity) ECDH(publicKey []byte) ([]byte, error) {
	if i == nil || i.private == nil {
		return nil, fmt.Errorf("%w: missing private key", ErrInvalidWrappingKey)
	}
	peer, err := ValidateWrappingPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	shared, err := i.private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("derive wrapping secret: %w", err)
	}
	return shared, nil
}

// ValidateWrappingPublicKey validates a WebCrypto-compatible P-256 ECDH key.
func ValidateWrappingPublicKey(publicKey []byte) (*ecdh.PublicKey, error) {
	if len(publicKey) != WrappingPublicKeySize {
		return nil, fmt.Errorf("%w: public key length %d", ErrInvalidWrappingKey, len(publicKey))
	}
	parsed, err := ecdh.P256().NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: public key: %v", ErrInvalidWrappingKey, err)
	}
	return parsed, nil
}
