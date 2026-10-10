// Package peercrypto implements account-independent device identities used by
// Peer Spaces. It deliberately has no dependency on Relay credentials or the
// account E2EE key.
package peercrypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

const (
	PrivateKeySize = 32
	PublicKeySize  = 65
	SignatureSize  = 64
)

var ErrInvalidKey = errors.New("peercrypto: invalid key")

// Identity owns one P-256 device signing key. Callers persist PrivateBytes in
// platform secure storage; it must never be written to ordinary app config.
type Identity struct {
	private *ecdsa.PrivateKey
}

// GenerateIdentity creates a new device identity using the system CSPRNG.
func GenerateIdentity() (*Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 identity: %w", err)
	}
	return &Identity{private: key}, nil
}

// ParseIdentity reconstructs an identity from its fixed-width P-256 scalar.
func ParseIdentity(privateBytes []byte) (*Identity, error) {
	if len(privateBytes) != PrivateKeySize {
		return nil, fmt.Errorf("%w: private key length %d", ErrInvalidKey, len(privateBytes))
	}
	d := new(big.Int).SetBytes(privateBytes)
	curve := elliptic.P256()
	if d.Sign() <= 0 || d.Cmp(curve.Params().N) >= 0 {
		return nil, fmt.Errorf("%w: private scalar out of range", ErrInvalidKey)
	}
	x, y := curve.ScalarBaseMult(privateBytes)
	return &Identity{private: &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}}, nil
}

// PrivateBytes returns a new fixed-width copy suitable for secure storage.
func (i *Identity) PrivateBytes() []byte {
	out := make([]byte, PrivateKeySize)
	i.private.D.FillBytes(out)
	return out
}

// PublicBytes returns the uncompressed SEC1 P-256 public key accepted by
// WebCrypto's raw EC import format.
func (i *Identity) PublicBytes() []byte {
	return elliptic.Marshal(elliptic.P256(), i.private.X, i.private.Y)
}

// PeerID is the base64url SHA-256 digest of the exact public-key bytes.
func (i *Identity) PeerID() string {
	return PeerID(i.PublicBytes())
}

// PeerID derives a stable identifier after validating the public key.
func PeerID(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ValidatePublicKey parses the cross-platform uncompressed SEC1 encoding.
func ValidatePublicKey(publicKey []byte) (*ecdsa.PublicKey, error) {
	if len(publicKey) != PublicKeySize {
		return nil, fmt.Errorf("%w: public key length %d", ErrInvalidKey, len(publicKey))
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return nil, fmt.Errorf("%w: public key is not on P-256", ErrInvalidKey)
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

// Sign signs SHA-256(payload) and returns an IEEE P1363 r||s signature. Low-S
// normalization removes ECDSA malleability so one document has one accepted
// signature class across Go and WebCrypto clients.
func (i *Identity) Sign(payload []byte) ([]byte, error) {
	digest := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, i.private, digest[:])
	if err != nil {
		return nil, fmt.Errorf("sign peer document: %w", err)
	}
	halfN := new(big.Int).Rsh(new(big.Int).Set(elliptic.P256().Params().N), 1)
	if s.Cmp(halfN) > 0 {
		s.Sub(elliptic.P256().Params().N, s)
	}
	out := make([]byte, SignatureSize)
	r.FillBytes(out[:SignatureSize/2])
	s.FillBytes(out[SignatureSize/2:])
	return out, nil
}

// Verify checks a low-S P1363 signature over the exact payload bytes.
func Verify(publicKey, payload, signature []byte) error {
	pub, err := ValidatePublicKey(publicKey)
	if err != nil {
		return err
	}
	if len(signature) != SignatureSize {
		return fmt.Errorf("peercrypto: signature length %d", len(signature))
	}
	r := new(big.Int).SetBytes(signature[:SignatureSize/2])
	s := new(big.Int).SetBytes(signature[SignatureSize/2:])
	halfN := new(big.Int).Rsh(new(big.Int).Set(elliptic.P256().Params().N), 1)
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(elliptic.P256().Params().N) >= 0 || s.Cmp(halfN) > 0 {
		return errors.New("peercrypto: invalid signature scalar")
	}
	digest := sha256.Sum256(payload)
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return errors.New("peercrypto: signature verification failed")
	}
	return nil
}
