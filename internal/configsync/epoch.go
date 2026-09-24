package configsync

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	EpochKeySize          = 32
	epochEnvelopeVersion  = 1
	epochEnvelopePrefix   = "ake1"
	maxEpochEnvelopeBytes = 8 << 10
	epochWrapInfo         = "atterm-peer-epoch-wrap-v1"
)

var (
	ErrInvalidEpochKey      = errors.New("configsync: invalid epoch key")
	ErrInvalidEpochEnvelope = errors.New("configsync: invalid epoch envelope")
	ErrSecretSyncDenied     = errors.New("configsync: recipient cannot sync vault secrets")
	ErrPayloadAuth          = errors.New("configsync: payload authentication failed")
)

// KeyClass separates portable configuration from explicitly enabled secrets.
type KeyClass string

const (
	KeyClassSync  KeyClass = "sync"
	KeyClassVault KeyClass = "vault"
)

func (c KeyClass) valid() bool {
	return c == KeyClassSync || c == KeyClassVault
}

func (c KeyClass) discriminator() byte {
	if c == KeyClassVault {
		return 2
	}
	return 1
}

// EpochKey is one generation of either the portable sync key or vault key.
type EpochKey struct {
	Class KeyClass
	Epoch uint64
	key   [EpochKeySize]byte
	valid bool
}

// GenerateEpochKey creates a new random key for a positive epoch.
func GenerateEpochKey(class KeyClass, epoch uint64) (EpochKey, error) {
	if !class.valid() || epoch == 0 {
		return EpochKey{}, ErrInvalidEpochKey
	}
	var key EpochKey
	key.Class, key.Epoch, key.valid = class, epoch, true
	if _, err := rand.Read(key.key[:]); err != nil {
		return EpochKey{}, fmt.Errorf("generate epoch key: %w", err)
	}
	return key, nil
}

// ParseEpochKey reconstructs a key obtained from secure storage or an envelope.
func ParseEpochKey(class KeyClass, epoch uint64, raw []byte) (EpochKey, error) {
	if !class.valid() || epoch == 0 || len(raw) != EpochKeySize {
		return EpochKey{}, ErrInvalidEpochKey
	}
	key := EpochKey{Class: class, Epoch: epoch, valid: true}
	copy(key.key[:], raw)
	return key, nil
}

// Bytes returns a detached copy for secure storage.
func (k EpochKey) Bytes() []byte {
	if !k.valid {
		return nil
	}
	return append([]byte(nil), k.key[:]...)
}

// EpochRecipient is derived from a verified membership certificate.
type EpochRecipient struct {
	PeerID            string
	WrappingPublicKey []byte
	CanSyncSecrets    bool
}

type epochEnvelope struct {
	V                    int      `json:"v"`
	SpaceID              string   `json:"space_id"`
	KeyClass             KeyClass `json:"key_class"`
	Epoch                uint64   `json:"epoch"`
	RecipientPeerID      string   `json:"recipient_peer_id"`
	RecipientWrappingKey string   `json:"recipient_wrapping_key"`
	EphemeralPublicKey   string   `json:"ephemeral_public_key"`
	Nonce                string   `json:"nonce"`
	Ciphertext           string   `json:"ciphertext"`
}

// SealEpochKey creates a P-256 ECDH/HKDF/AES-GCM hybrid envelope for one
// member. Vault envelopes are rejected unless membership grants secret sync.
func SealEpochKey(spaceID string, key EpochKey, recipient EpochRecipient) (string, error) {
	if !validSpaceID(spaceID) || !key.valid || !key.Class.valid() || key.Epoch == 0 || !validPeerID(recipient.PeerID) {
		return "", ErrInvalidEpochEnvelope
	}
	if key.Class == KeyClassVault && !recipient.CanSyncSecrets {
		return "", ErrSecretSyncDenied
	}
	if _, err := peercrypto.ValidateWrappingPublicKey(recipient.WrappingPublicKey); err != nil {
		return "", fmt.Errorf("%w: recipient wrapping key", ErrInvalidEpochEnvelope)
	}
	ephemeral, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		return "", err
	}
	shared, err := ephemeral.ECDH(recipient.WrappingPublicKey)
	if err != nil {
		return "", err
	}
	doc := epochEnvelope{
		V:                    epochEnvelopeVersion,
		SpaceID:              spaceID,
		KeyClass:             key.Class,
		Epoch:                key.Epoch,
		RecipientPeerID:      recipient.PeerID,
		RecipientWrappingKey: encode(recipient.WrappingPublicKey),
		EphemeralPublicKey:   encode(ephemeral.PublicBytes()),
	}
	aad, err := epochEnvelopeAAD(doc)
	if err != nil {
		return "", err
	}
	wrapKey, err := deriveEpochWrapKey(shared, aad)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(wrapKey)
	if err != nil {
		return "", fmt.Errorf("create epoch wrap cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate epoch envelope nonce: %w", err)
	}
	doc.Nonce = encode(nonce)
	doc.Ciphertext = encode(aead.Seal(nil, nonce, key.key[:], aad))
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal epoch envelope: %w", err)
	}
	return epochEnvelopePrefix + "." + encode(raw), nil
}

// OpenEpochKey authenticates and opens an envelope for the expected Space and
// signing peer identity.
func OpenEpochKey(token, expectedSpaceID, expectedPeerID string, identity *peercrypto.WrappingIdentity) (EpochKey, error) {
	doc, err := parseEpochEnvelope(token)
	if err != nil {
		return EpochKey{}, err
	}
	if doc.SpaceID != expectedSpaceID || doc.RecipientPeerID != expectedPeerID || identity == nil || !bytes.Equal(identity.PublicBytes(), mustDecode(doc.RecipientWrappingKey)) {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	ephemeralPublic, err := decodeSized(doc.EphemeralPublicKey, peercrypto.WrappingPublicKeySize, "ephemeral public key")
	if err != nil {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	shared, err := identity.ECDH(ephemeralPublic)
	if err != nil {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	aad, err := epochEnvelopeAAD(doc)
	if err != nil {
		return EpochKey{}, err
	}
	wrapKey, err := deriveEpochWrapKey(shared, aad)
	if err != nil {
		return EpochKey{}, err
	}
	block, err := aes.NewCipher(wrapKey)
	if err != nil {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return EpochKey{}, err
	}
	nonce, err := decodeSized(doc.Nonce, aead.NonceSize(), "nonce")
	if err != nil {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(doc.Ciphertext)
	if err != nil || encode(ciphertext) != doc.Ciphertext || len(ciphertext) != EpochKeySize+aead.Overhead() {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return EpochKey{}, ErrInvalidEpochEnvelope
	}
	return ParseEpochKey(doc.KeyClass, doc.Epoch, plaintext)
}

// SignEncryptedOp seals mutation.Payload under the supplied epoch key before
// signing the immutable operation. Delete operations carry no ciphertext.
func SignEncryptedOp(identity *peercrypto.Identity, spaceID string, counter uint64, hlc Timestamp, causal VersionVector, key EpochKey, mutation Mutation) (string, error) {
	if identity == nil || !key.valid {
		return "", ErrInvalidEpochKey
	}
	mutation.KeyClass, mutation.KeyEpoch = key.Class, key.Epoch
	if mutation.Kind == KindDelete {
		mutation.Payload = nil
		return SignOp(identity, spaceID, counter, hlc, causal, mutation)
	}
	context := PayloadContext{
		SpaceID: spaceID, Collection: mutation.Collection, RecordID: mutation.RecordID,
		OpID: OpIDFor(identity.PeerID(), counter), KeyClass: key.Class, Epoch: key.Epoch,
	}
	sealed, err := SealConfigPayload(key, context, mutation.Payload)
	if err != nil {
		return "", err
	}
	mutation.Payload = sealed
	return SignOp(identity, spaceID, counter, hlc, causal, mutation)
}

// PayloadContext is bound into every encrypted config value.
type PayloadContext struct {
	SpaceID    string
	Collection string
	RecordID   string
	OpID       string
	KeyClass   KeyClass
	Epoch      uint64
}

// SealConfigPayload encrypts a config value with XChaCha20-Poly1305.
func SealConfigPayload(key EpochKey, context PayloadContext, plaintext []byte) ([]byte, error) {
	if !key.valid || key.Class != context.KeyClass || key.Epoch != context.Epoch {
		return nil, ErrInvalidEpochKey
	}
	aad, err := configPayloadAAD(context)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key.key[:])
	if err != nil {
		return nil, ErrInvalidEpochKey
	}
	if len(plaintext) > maxPayloadBytes-e2eecrypto.EnvelopePrefixSize-aead.Overhead() {
		return nil, fmt.Errorf("%w: payload size", ErrInvalidEpochKey)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate config payload nonce: %w", err)
	}
	envelope := make([]byte, 0, e2eecrypto.EnvelopePrefixSize+len(plaintext)+aead.Overhead())
	envelope = append(envelope, byte(e2eecrypto.CipherXChaCha20Poly1305))
	envelope = append(envelope, nonce...)
	return aead.Seal(envelope, nonce, plaintext, aad), nil
}

// OpenConfigPayload opens a payload only under the exact operation context.
func OpenConfigPayload(key EpochKey, context PayloadContext, envelope []byte) ([]byte, error) {
	if !key.valid || key.Class != context.KeyClass || key.Epoch != context.Epoch {
		return nil, ErrInvalidEpochKey
	}
	aad, err := configPayloadAAD(context)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key.key[:])
	if err != nil {
		return nil, ErrInvalidEpochKey
	}
	if len(envelope) < e2eecrypto.EnvelopePrefixSize+aead.Overhead() || len(envelope) > maxPayloadBytes || envelope[0] != byte(e2eecrypto.CipherXChaCha20Poly1305) {
		return nil, ErrPayloadAuth
	}
	plaintext, err := aead.Open(nil, envelope[1:1+aead.NonceSize()], envelope[1+aead.NonceSize():], aad)
	if err != nil {
		return nil, ErrPayloadAuth
	}
	return plaintext, nil
}

// OpenOpPayload derives the AAD directly from a verified operation.
func OpenOpPayload(key EpochKey, op VerifiedOp) ([]byte, error) {
	if op.Document.Kind == KindDelete {
		return nil, nil
	}
	return OpenConfigPayload(key, PayloadContext{
		SpaceID: op.Document.SpaceID, Collection: op.Document.Collection,
		RecordID: op.Document.RecordID, OpID: op.Document.OpID,
		KeyClass: op.Document.KeyClass, Epoch: op.Document.KeyEpoch,
	}, op.Document.Payload)
}

// OpenRecordPayload opens a materialized record returned by Replica.Get.
func OpenRecordPayload(key EpochKey, record Record) ([]byte, error) {
	if record.Deleted {
		return nil, nil
	}
	return OpenConfigPayload(key, PayloadContext{
		SpaceID: record.SpaceID, Collection: record.Collection, RecordID: record.RecordID,
		OpID: record.OpID, KeyClass: record.KeyClass, Epoch: record.KeyEpoch,
	}, record.Payload)
}

func parseEpochEnvelope(token string) (epochEnvelope, error) {
	if len(token) == 0 || len(token) > maxEpochEnvelopeBytes {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] != epochEnvelopePrefix {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	var doc epochEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil || !errors.Is(err, io.EOF) {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) || doc.V != epochEnvelopeVersion || !validSpaceID(doc.SpaceID) || !doc.KeyClass.valid() || doc.Epoch == 0 || !validPeerID(doc.RecipientPeerID) {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	if _, err := decodeSized(doc.RecipientWrappingKey, peercrypto.WrappingPublicKeySize, "recipient wrapping key"); err != nil {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	if _, err := decodeSized(doc.EphemeralPublicKey, peercrypto.WrappingPublicKeySize, "ephemeral public key"); err != nil {
		return epochEnvelope{}, ErrInvalidEpochEnvelope
	}
	return doc, nil
}

func epochEnvelopeAAD(doc epochEnvelope) ([]byte, error) {
	spaceID, err := uuid.Parse(doc.SpaceID)
	if err != nil {
		return nil, ErrInvalidEpochEnvelope
	}
	peerID, err := base64.RawURLEncoding.Strict().DecodeString(doc.RecipientPeerID)
	if err != nil || len(peerID) != sha256.Size {
		return nil, ErrInvalidEpochEnvelope
	}
	recipientKey, err := decodeSized(doc.RecipientWrappingKey, peercrypto.WrappingPublicKeySize, "recipient wrapping key")
	if err != nil {
		return nil, ErrInvalidEpochEnvelope
	}
	ephemeralKey, err := decodeSized(doc.EphemeralPublicKey, peercrypto.WrappingPublicKeySize, "ephemeral public key")
	if err != nil {
		return nil, ErrInvalidEpochEnvelope
	}
	aad := make([]byte, 0, 16+1+1+8+sha256.Size+2*peercrypto.WrappingPublicKeySize)
	aad = append(aad, spaceID[:]...)
	aad = append(aad, e2eecrypto.AADTagPeerEpochEnvelope, doc.KeyClass.discriminator())
	aad = binary.BigEndian.AppendUint64(aad, doc.Epoch)
	aad = append(aad, peerID...)
	aad = append(aad, recipientKey...)
	aad = append(aad, ephemeralKey...)
	return aad, nil
}

func configPayloadAAD(context PayloadContext) ([]byte, error) {
	spaceID, err := uuid.Parse(context.SpaceID)
	if err != nil || !context.KeyClass.valid() || context.Epoch == 0 || !validName(context.Collection, 64) || !validRecordID(context.RecordID) || context.OpID == "" {
		return nil, ErrInvalidEpochKey
	}
	aad := make([]byte, 0, 32+len(context.Collection)+len(context.RecordID)+len(context.OpID))
	aad = append(aad, spaceID[:]...)
	aad = append(aad, e2eecrypto.AADTagPeerConfigPayload, context.KeyClass.discriminator())
	aad = binary.BigEndian.AppendUint64(aad, context.Epoch)
	for _, value := range []string{context.Collection, context.RecordID, context.OpID} {
		if len(value) > 65535 {
			return nil, ErrInvalidEpochKey
		}
		aad = binary.BigEndian.AppendUint16(aad, uint16(len(value)))
		aad = append(aad, value...)
	}
	return aad, nil
}

func deriveEpochWrapKey(shared, aad []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, shared, nil, append([]byte(epochWrapInfo), aad...))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("derive epoch wrap key: %w", err)
	}
	return key, nil
}

func mustDecode(value string) []byte {
	decoded, _ := base64.RawURLEncoding.Strict().DecodeString(value)
	return decoded
}
