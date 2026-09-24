package peertransport

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

const (
	accountKeySize        = 32
	directTicketSize      = 32
	maxTranscriptString   = 128
	p256PublicKeySize     = 65
	transcriptDomain      = "atterm-direct-handshake-v1"
	proofInfo             = "atterm-direct-proof-v1"
	clientProofDomain     = "atterm-direct-client-v1"
	hostProofDomain       = "atterm-direct-host-v1"
	finishProofDomain     = "atterm-direct-finish-v1"
	trafficInfo           = "atterm-direct-traffic-v1"
	directionalKeySize    = 32
	directionalNonceSize  = 16
	trafficKeyMaterialLen = 2*directionalKeySize + 2*directionalNonceSize
)

var (
	// ErrInvalidTranscript means a claim or public key cannot be represented by
	// the v1 canonical transcript.
	ErrInvalidTranscript = errors.New("peertransport: invalid transcript")
	// ErrAuthentication means a direct proof does not match the transcript.
	ErrAuthentication = errors.New("peertransport: authentication failed")
	// ErrInvalidKey means key material has the wrong size or is not on P-256.
	ErrInvalidKey = errors.New("peertransport: invalid key")
)

// Permission is the effective Relay permission bound into a direct attempt.
type Permission byte

const (
	PermissionView    Permission = 1
	PermissionControl Permission = 2
	PermissionFull    Permission = 3
)

func (p Permission) valid() bool {
	return p >= PermissionView && p <= PermissionFull
}

// Role separates client and host proofs made over the same transcript.
type Role byte

const (
	RoleClient Role = 1
	RoleHost   Role = 2
)

func (r Role) proofDomain() (string, error) {
	switch r {
	case RoleClient:
		return clientProofDomain, nil
	case RoleHost:
		return hostProofDomain, nil
	default:
		return "", fmt.Errorf("%w: unknown role %d", ErrAuthentication, r)
	}
}

// Transcript binds Relay authorization claims to both ephemeral ECDH keys.
// MarshalBinary is the only canonical encoding used by proofs and key setup.
type Transcript struct {
	AttemptID           uuid.UUID
	Ticket              []byte
	SessionID           uuid.UUID
	UserID              string
	HostID              string
	ClientInstanceID    string
	Permission          Permission
	ExpiresAtUnixMillis uint64
	ClientPublicKey     []byte
	HostPublicKey       []byte
}

// MarshalBinary returns the canonical v1 transcript described by the direct
// transport design. It validates curve points before any proof is calculated.
func (t Transcript) MarshalBinary() ([]byte, error) {
	if t.AttemptID == uuid.Nil || t.SessionID == uuid.Nil {
		return nil, fmt.Errorf("%w: nil attempt or session id", ErrInvalidTranscript)
	}
	if len(t.Ticket) != directTicketSize {
		return nil, fmt.Errorf("%w: ticket is %d bytes", ErrInvalidTranscript, len(t.Ticket))
	}
	if !t.Permission.valid() {
		return nil, fmt.Errorf("%w: permission %d", ErrInvalidTranscript, t.Permission)
	}
	if t.ExpiresAtUnixMillis == 0 {
		return nil, fmt.Errorf("%w: zero expiry", ErrInvalidTranscript)
	}
	for name, value := range map[string]string{
		"user_id":            t.UserID,
		"host_id":            t.HostID,
		"client_instance_id": t.ClientInstanceID,
	} {
		if value == "" || !utf8.ValidString(value) || len(value) > maxTranscriptString {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTranscript, name)
		}
	}
	curve := ecdh.P256()
	for name, publicKey := range map[string][]byte{
		"client public key": t.ClientPublicKey,
		"host public key":   t.HostPublicKey,
	} {
		if len(publicKey) != p256PublicKeySize {
			return nil, fmt.Errorf("%w: %s is %d bytes", ErrInvalidTranscript, name, len(publicKey))
		}
		if _, err := curve.NewPublicKey(publicKey); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidTranscript, name, err)
		}
	}

	capacity := len(transcriptDomain) + 16 + 2 + directTicketSize + 16 +
		2 + len(t.UserID) + 2 + len(t.HostID) + 2 + len(t.ClientInstanceID) +
		1 + 8 + 2 + len(t.ClientPublicKey) + 2 + len(t.HostPublicKey)
	out := make([]byte, 0, capacity)
	out = append(out, transcriptDomain...)
	out = append(out, t.AttemptID[:]...)
	out = appendField(out, t.Ticket)
	out = append(out, t.SessionID[:]...)
	out = appendField(out, []byte(t.UserID))
	out = appendField(out, []byte(t.HostID))
	out = appendField(out, []byte(t.ClientInstanceID))
	out = append(out, byte(t.Permission))
	out = binary.BigEndian.AppendUint64(out, t.ExpiresAtUnixMillis)
	out = appendField(out, t.ClientPublicKey)
	out = appendField(out, t.HostPublicKey)
	return out, nil
}

func appendField(dst, value []byte) []byte {
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(value)))
	return append(dst, value...)
}

// HandshakeAuthenticator proves authority over one transcript without
// exposing its root secret to the transport implementation.
type HandshakeAuthenticator interface {
	BuildProof(transcript []byte, role Role) ([]byte, error)
	VerifyProof(transcript []byte, role Role, proof []byte) error
	KeyBinding(transcript []byte) ([]byte, error)
}

// AccountKeyAuthenticator implements the v0.6 same-account direct handshake.
// It copies accountKey so callers may clear their input buffer immediately.
type AccountKeyAuthenticator struct {
	accountKey [accountKeySize]byte
}

// NewAccountKeyAuthenticator creates a per-account proof provider.
func NewAccountKeyAuthenticator(accountKey []byte) (*AccountKeyAuthenticator, error) {
	if len(accountKey) != accountKeySize {
		return nil, fmt.Errorf("%w: account_key is %d bytes", ErrInvalidKey, len(accountKey))
	}
	a := &AccountKeyAuthenticator{}
	copy(a.accountKey[:], accountKey)
	return a, nil
}

// KeyBinding derives the per-attempt proof key used as the ECDH HKDF salt.
func (a *AccountKeyAuthenticator) KeyBinding(transcript []byte) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: nil authenticator", ErrInvalidKey)
	}
	hash := sha256.Sum256(transcript)
	r := hkdf.New(sha256.New, a.accountKey[:], hash[:], []byte(proofInfo))
	key := make([]byte, directionalKeySize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("derive proof key: %w", err)
	}
	return key, nil
}

// BuildProof returns the role-separated HMAC for transcript.
func (a *AccountKeyAuthenticator) BuildProof(transcript []byte, role Role) ([]byte, error) {
	domain, err := role.proofDomain()
	if err != nil {
		return nil, err
	}
	key, err := a.KeyBinding(transcript)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(transcript)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write(hash[:])
	return mac.Sum(nil), nil
}

// VerifyProof checks a client or host proof in constant time.
func (a *AccountKeyAuthenticator) VerifyProof(transcript []byte, role Role, proof []byte) error {
	expected, err := a.BuildProof(transcript, role)
	if err != nil {
		return err
	}
	if !hmac.Equal(expected, proof) {
		return ErrAuthentication
	}
	return nil
}

// BuildFinishProof confirms that the client received and verified HOST_HELLO.
func BuildFinishProof(auth HandshakeAuthenticator, transcript []byte) ([]byte, error) {
	key, err := auth.KeyBinding(transcript)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(transcript)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(finishProofDomain))
	_, _ = mac.Write(hash[:])
	return mac.Sum(nil), nil
}

// GenerateEphemeralKey returns a fresh P-256 key for one direct attempt.
func GenerateEphemeralKey() (*ecdh.PrivateKey, error) {
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 key: %w", err)
	}
	return key, nil
}

// TrafficKeys contains direction-separated record keys and nonce prefixes.
type TrafficKeys struct {
	ClientToHostKey         [directionalKeySize]byte
	HostToClientKey         [directionalKeySize]byte
	ClientToHostNoncePrefix [directionalNonceSize]byte
	HostToClientNoncePrefix [directionalNonceSize]byte
}

// DeriveTrafficKeys performs P-256 ECDH and binds its result to the
// authenticator and exact transcript.
func DeriveTrafficKeys(privateKey *ecdh.PrivateKey, peerPublicKey, transcript []byte, auth HandshakeAuthenticator) (TrafficKeys, error) {
	if privateKey == nil || auth == nil {
		return TrafficKeys{}, fmt.Errorf("%w: missing private key or authenticator", ErrInvalidKey)
	}
	peer, err := ecdh.P256().NewPublicKey(peerPublicKey)
	if err != nil {
		return TrafficKeys{}, fmt.Errorf("%w: peer public key: %v", ErrInvalidKey, err)
	}
	shared, err := privateKey.ECDH(peer)
	if err != nil {
		return TrafficKeys{}, fmt.Errorf("P-256 ECDH: %w", err)
	}
	binding, err := auth.KeyBinding(transcript)
	if err != nil {
		return TrafficKeys{}, err
	}
	hash := sha256.Sum256(transcript)
	info := make([]byte, 0, len(trafficInfo)+len(hash))
	info = append(info, trafficInfo...)
	info = append(info, hash[:]...)
	r := hkdf.New(sha256.New, shared, binding, info)
	material := make([]byte, trafficKeyMaterialLen)
	if _, err := io.ReadFull(r, material); err != nil {
		return TrafficKeys{}, fmt.Errorf("derive traffic keys: %w", err)
	}
	var keys TrafficKeys
	copy(keys.ClientToHostKey[:], material[0:32])
	copy(keys.HostToClientKey[:], material[32:64])
	copy(keys.ClientToHostNoncePrefix[:], material[64:80])
	copy(keys.HostToClientNoncePrefix[:], material[80:96])
	return keys, nil
}

// TranscriptHash returns the record-layer binding for a canonical transcript.
func TranscriptHash(transcript []byte) [sha256.Size]byte {
	return sha256.Sum256(transcript)
}
