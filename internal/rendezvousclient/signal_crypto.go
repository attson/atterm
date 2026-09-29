package rendezvousclient

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	signalEnvelopeVersion = 1
	signalKeyInfo         = "atterm-peer-rendezvous-signal-key-v1"
	signalAADDomain       = "atterm-peer-rendezvous-signal-aad-v1"
	maxSignalPlaintext    = 60 << 10
)

var (
	ErrServiceUnavailable = errors.New("rendezvous client: service unavailable")
	ErrPeerOffline        = errors.New("rendezvous client: peer offline")
	ErrICEFailed          = errors.New("rendezvous client: ICE failed")
	ErrAuthentication     = errors.New("rendezvous client: authentication failed")
	ErrInvalidSignal      = errors.New("rendezvous client: invalid encrypted signal")
	ErrSignalReplay       = errors.New("rendezvous client: signal replay")
)

// SignalCipherConfig binds one pairwise signaling key to the current sync
// epoch and the two verified Peer memberships.
type SignalCipherConfig struct {
	EpochKey                configsync.EpochKey
	SpaceID                 string
	LocalPeerID             string
	RemotePeerID            string
	LocalWrappingIdentity   *peercrypto.WrappingIdentity
	RemoteWrappingPublicKey []byte
}

// SignalRouteBinding is authenticated with every sealed message so a
// Rendezvous operator cannot redirect ciphertext across routes or retries.
type SignalRouteBinding struct {
	Topic          string
	FromPresenceID string
	ToPresenceID   string
	MessageID      string
}

// SignalCipher encrypts SDP/ICE and route authorization independently from
// both Rendezvous TLS and the later authenticated DataChannel handshake.
type SignalCipher struct {
	key [chacha20poly1305.KeySize]byte
}

func NewSignalCipher(cfg SignalCipherConfig) (*SignalCipher, error) {
	if cfg.EpochKey.Class != configsync.KeyClassSync || cfg.EpochKey.Epoch == 0 ||
		len(cfg.EpochKey.Bytes()) != configsync.EpochKeySize || cfg.SpaceID == "" ||
		cfg.LocalPeerID == "" || cfg.RemotePeerID == "" || cfg.LocalPeerID == cfg.RemotePeerID ||
		cfg.LocalWrappingIdentity == nil {
		return nil, ErrInvalidSignal
	}
	shared, err := cfg.LocalWrappingIdentity.ECDH(cfg.RemoteWrappingPublicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: peer ECDH", ErrInvalidSignal)
	}
	first, second := cfg.LocalPeerID, cfg.RemotePeerID
	if second < first {
		first, second = second, first
	}
	info := encodeSignalParts([]byte(signalKeyInfo), []byte(cfg.SpaceID), []byte(first), []byte(second), uint64Bytes(cfg.EpochKey.Epoch))
	reader := hkdf.New(sha256.New, shared, cfg.EpochKey.Bytes(), info)
	cipher := &SignalCipher{}
	if _, err := io.ReadFull(reader, cipher.key[:]); err != nil {
		return nil, fmt.Errorf("derive Rendezvous signal key: %w", err)
	}
	return cipher, nil
}

func (c *SignalCipher) Seal(binding SignalRouteBinding, plaintext []byte) ([]byte, error) {
	if c == nil || len(plaintext) == 0 || len(plaintext) > maxSignalPlaintext {
		return nil, ErrInvalidSignal
	}
	aad, err := signalAAD(binding)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(c.key[:])
	if err != nil {
		return nil, fmt.Errorf("create Rendezvous signal cipher: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("create Rendezvous signal nonce: %w", err)
	}
	out := make([]byte, 1, 1+len(nonce)+len(plaintext)+aead.Overhead())
	out[0] = signalEnvelopeVersion
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad), nil
}

func (c *SignalCipher) Open(binding SignalRouteBinding, envelope []byte) ([]byte, error) {
	if c == nil || len(envelope) < 1+chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead ||
		len(envelope) > 1+chacha20poly1305.NonceSizeX+maxSignalPlaintext+chacha20poly1305.Overhead ||
		envelope[0] != signalEnvelopeVersion {
		return nil, ErrInvalidSignal
	}
	aad, err := signalAAD(binding)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(c.key[:])
	if err != nil {
		return nil, ErrInvalidSignal
	}
	nonce := envelope[1 : 1+chacha20poly1305.NonceSizeX]
	plaintext, err := aead.Open(nil, nonce, envelope[1+chacha20poly1305.NonceSizeX:], aad)
	if err != nil || len(plaintext) == 0 || len(plaintext) > maxSignalPlaintext {
		return nil, ErrInvalidSignal
	}
	return plaintext, nil
}

func signalAAD(binding SignalRouteBinding) ([]byte, error) {
	if validateOpaqueID(binding.Topic, 32) != nil || validateOpaqueID(binding.FromPresenceID, 32) != nil ||
		validateOpaqueID(binding.ToPresenceID, 32) != nil || validateOpaqueID(binding.MessageID, 16) != nil ||
		binding.FromPresenceID == binding.ToPresenceID {
		return nil, ErrInvalidSignal
	}
	return encodeSignalParts(
		[]byte(signalAADDomain), []byte(binding.Topic), []byte(binding.FromPresenceID),
		[]byte(binding.ToPresenceID), []byte(binding.MessageID),
	), nil
}

func encodeSignalParts(parts ...[]byte) []byte {
	var out bytes.Buffer
	for _, part := range parts {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(part)))
		out.Write(size[:])
		out.Write(part)
	}
	return out.Bytes()
}

func uint64Bytes(value uint64) []byte {
	var out [8]byte
	binary.BigEndian.PutUint64(out[:], value)
	return out[:]
}

// SignalReplayWindow rejects duplicate sender/message pairs with bounded
// memory. The Rendezvous retry window is finite, so FIFO eviction is enough.
type SignalReplayWindow struct {
	mu       sync.Mutex
	capacity int
	order    []string
	seen     map[string]struct{}
}

func NewSignalReplayWindow(capacity int) *SignalReplayWindow {
	if capacity < 1 {
		capacity = 1
	}
	return &SignalReplayWindow{capacity: capacity, seen: make(map[string]struct{}, capacity)}
}

func (w *SignalReplayWindow) Accept(binding SignalRouteBinding) error {
	if w == nil || validateOpaqueID(binding.FromPresenceID, 32) != nil || validateOpaqueID(binding.MessageID, 16) != nil {
		return ErrInvalidSignal
	}
	key := binding.FromPresenceID + "\x00" + binding.MessageID
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, duplicate := w.seen[key]; duplicate {
		return ErrSignalReplay
	}
	if len(w.order) == w.capacity {
		delete(w.seen, w.order[0])
		copy(w.order, w.order[1:])
		w.order = w.order[:len(w.order)-1]
	}
	w.order = append(w.order, key)
	w.seen[key] = struct{}{}
	return nil
}

func newSignalMessageID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
