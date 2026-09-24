package configsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const (
	OpVersion       = 1
	opTokenPrefix   = "aco1"
	maxOpTokenBytes = 1 << 20
	maxPayloadBytes = 512 << 10
	maxCausalActors = 256
)

var (
	ErrInvalidOp       = errors.New("configsync: invalid operation")
	ErrCounterFork     = errors.New("configsync: device counter fork")
	ErrCounterRollback = errors.New("configsync: device counter rollback")
)

// Kind is deliberately limited to ordinary configuration mutations.
// Membership, revocation, and key rotation belong to a deny-wins governance
// log and must never inherit these LWW rules.
type Kind string

const (
	KindSet    Kind = "set"
	KindDelete Kind = "delete"
)

// Mutation is the unsigned application-level input for a SyncOp.
type Mutation struct {
	SchemaVersion uint32
	Collection    string
	RecordID      string
	Kind          Kind
	Payload       []byte
}

// SyncOp is the immutable payload covered by a peer identity signature.
// Payload is opaque to the replica; schema adapters validate and decode it.
type SyncOp struct {
	V              int           `json:"v"`
	SpaceID        string        `json:"space_id"`
	SchemaVersion  uint32        `json:"schema_version"`
	OpID           string        `json:"op_id"`
	ActorDeviceID  string        `json:"actor_device_id"`
	ActorPublicKey string        `json:"actor_public_key"`
	Counter        uint64        `json:"counter"`
	HLC            Timestamp     `json:"hlc"`
	Collection     string        `json:"collection"`
	RecordID       string        `json:"record_id"`
	Kind           Kind          `json:"kind"`
	Payload        []byte        `json:"payload"`
	PayloadHash    string        `json:"payload_hash"`
	CausalContext  VersionVector `json:"causal_context"`
}

// VerifiedOp retains the exact signed token. Callers should persist Token,
// rather than serializing Document again.
type VerifiedOp struct {
	Token     string
	Document  SyncOp
	PublicKey []byte
	payloadID [sha256.Size]byte
}

// OpIDFor deterministically names one device counter.
func OpIDFor(actorDeviceID string, counter uint64) string {
	return actorDeviceID + ":" + strconv.FormatUint(counter, 10)
}

// SignOp creates an immutable signed operation token.
func SignOp(identity *peercrypto.Identity, spaceID string, counter uint64, hlc Timestamp, causal VersionVector, mutation Mutation) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("%w: missing identity", ErrInvalidOp)
	}
	publicKey := identity.PublicBytes()
	payload := append([]byte(nil), mutation.Payload...)
	payloadHash := sha256.Sum256(payload)
	doc := SyncOp{
		V:              OpVersion,
		SpaceID:        spaceID,
		SchemaVersion:  mutation.SchemaVersion,
		ActorDeviceID:  identity.PeerID(),
		ActorPublicKey: encode(publicKey),
		Counter:        counter,
		HLC:            hlc,
		Collection:     mutation.Collection,
		RecordID:       mutation.RecordID,
		Kind:           mutation.Kind,
		Payload:        payload,
		PayloadHash:    encode(payloadHash[:]),
		CausalContext:  causal.Clone(),
	}
	doc.OpID = OpIDFor(doc.ActorDeviceID, counter)
	if doc.CausalContext == nil {
		doc.CausalContext = VersionVector{}
	}
	if err := validateOp(doc, publicKey); err != nil {
		return "", err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal config operation: %w", err)
	}
	signature, err := identity.Sign(raw)
	if err != nil {
		return "", err
	}
	return opTokenPrefix + "." + encode(raw) + "." + encode(signature), nil
}

// VerifyOp strictly parses a token and verifies its signature over the exact
// JSON bytes carried by that token.
func VerifyOp(token string) (VerifiedOp, error) {
	if len(token) == 0 || len(token) > maxOpTokenBytes {
		return VerifiedOp{}, fmt.Errorf("%w: token size", ErrInvalidOp)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != opTokenPrefix {
		return VerifiedOp{}, fmt.Errorf("%w: token prefix", ErrInvalidOp)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return VerifiedOp{}, fmt.Errorf("%w: payload encoding", ErrInvalidOp)
	}
	signature, err := decodeSized(parts[2], peercrypto.SignatureSize, "signature")
	if err != nil {
		return VerifiedOp{}, err
	}

	var doc SyncOp
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return VerifiedOp{}, fmt.Errorf("%w: payload JSON: %v", ErrInvalidOp, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil || !errors.Is(err, io.EOF) {
		return VerifiedOp{}, fmt.Errorf("%w: trailing payload", ErrInvalidOp)
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) {
		return VerifiedOp{}, fmt.Errorf("%w: non-canonical payload", ErrInvalidOp)
	}

	publicKey, err := decodeSized(doc.ActorPublicKey, peercrypto.PublicKeySize, "actor public key")
	if err != nil {
		return VerifiedOp{}, err
	}
	if err := validateOp(doc, publicKey); err != nil {
		return VerifiedOp{}, err
	}
	if err := peercrypto.Verify(publicKey, raw, signature); err != nil {
		return VerifiedOp{}, fmt.Errorf("%w: signature: %v", ErrInvalidOp, err)
	}
	doc.Payload = append([]byte(nil), doc.Payload...)
	doc.CausalContext = doc.CausalContext.Clone()
	return VerifiedOp{Token: token, Document: doc, PublicKey: publicKey, payloadID: sha256.Sum256(raw)}, nil
}

func validateOp(doc SyncOp, publicKey []byte) error {
	if doc.V != OpVersion || !validSpaceID(doc.SpaceID) || doc.SchemaVersion == 0 || doc.Counter == 0 {
		return fmt.Errorf("%w: version, space, schema, or counter", ErrInvalidOp)
	}
	if doc.HLC.PhysicalMS <= 0 {
		return fmt.Errorf("%w: HLC", ErrInvalidOp)
	}
	if peercrypto.PeerID(publicKey) != doc.ActorDeviceID || !validPeerID(doc.ActorDeviceID) {
		return fmt.Errorf("%w: actor identity", ErrInvalidOp)
	}
	if doc.OpID != OpIDFor(doc.ActorDeviceID, doc.Counter) {
		return fmt.Errorf("%w: operation id", ErrInvalidOp)
	}
	if !validName(doc.Collection, 64) || !validRecordID(doc.RecordID) {
		return fmt.Errorf("%w: collection or record id", ErrInvalidOp)
	}
	if doc.Kind != KindSet && doc.Kind != KindDelete {
		return fmt.Errorf("%w: mutation kind", ErrInvalidOp)
	}
	if len(doc.Payload) > maxPayloadBytes || (doc.Kind == KindDelete && len(doc.Payload) != 0) {
		return fmt.Errorf("%w: payload", ErrInvalidOp)
	}
	payloadHash := sha256.Sum256(doc.Payload)
	if doc.PayloadHash != encode(payloadHash[:]) {
		return fmt.Errorf("%w: payload hash", ErrInvalidOp)
	}
	if len(doc.CausalContext) > maxCausalActors {
		return fmt.Errorf("%w: causal context size", ErrInvalidOp)
	}
	if doc.CausalContext == nil {
		return fmt.Errorf("%w: missing causal context", ErrInvalidOp)
	}
	for actor, seenCounter := range doc.CausalContext {
		if !validPeerID(actor) || seenCounter == 0 {
			return fmt.Errorf("%w: causal context", ErrInvalidOp)
		}
	}
	wantPrevious := doc.Counter - 1
	if doc.CausalContext[doc.ActorDeviceID] != wantPrevious {
		return fmt.Errorf("%w: actor counter continuity", ErrInvalidOp)
	}
	return nil
}

func validSpaceID(value string) bool {
	return uuid.Validate(value) == nil
}

func validName(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validRecordID(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value)
}

func validPeerID(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && encode(decoded) == value
}

func decodeSized(value string, size int, field string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != size || encode(decoded) != value {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOp, field)
	}
	return decoded, nil
}

func encode(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
