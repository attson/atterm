package configsync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const (
	snapshotVersion       = 1
	snapshotTokenPrefix   = "acs1"
	maxSnapshotTokenBytes = 32 << 20
	maxSnapshotOps        = 100_000
)

var ErrInvalidSnapshot = errors.New("configsync: invalid snapshot")

// Snapshot is a signed compaction boundary. RecordOps contains exactly one
// winning signed operation per materialized record. RetainedOps preserves
// operations whose schema this replica cannot interpret.
type Snapshot struct {
	V                int           `json:"v"`
	SnapshotID       string        `json:"snapshot_id"`
	SpaceID          string        `json:"space_id"`
	SchemaVersion    uint32        `json:"schema_version"`
	CreatorDeviceID  string        `json:"creator_device_id"`
	CreatorPublicKey string        `json:"creator_public_key"`
	CreatedHLC       Timestamp     `json:"created_hlc"`
	CoverVector      VersionVector `json:"cover_vector"`
	RecordOps        []string      `json:"record_ops"`
	RetainedOps      []string      `json:"retained_ops"`
}

// VerifiedSnapshot contains the exact signed token and its independently
// verified embedded operations.
type VerifiedSnapshot struct {
	Token       string
	Document    Snapshot
	PublicKey   []byte
	RecordOps   []VerifiedOp
	RetainedOps []VerifiedOp
}

// SignSnapshot compacts a complete replica state into a signed token. A
// replica with counter gaps cannot be snapshotted because its apparent winner
// may still depend on a missing earlier operation.
func (r *Replica) SignSnapshot(identity *peercrypto.Identity) (string, error) {
	token, _, err := r.signSnapshot(identity, nil)
	return token, err
}

func (r *Replica) signSnapshot(identity *peercrypto.Identity, stable VersionVector) (string, int, error) {
	if identity == nil {
		return "", 0, fmt.Errorf("%w: missing identity", ErrInvalidSnapshot)
	}
	r.mu.RLock()
	if !r.vector.Covers(r.maxCounters) {
		r.mu.RUnlock()
		return "", 0, ErrIncompleteHistory
	}
	if stable != nil && (validateAntiEntropyVector(stable) != nil || !r.vector.Covers(stable)) {
		r.mu.RUnlock()
		return "", 0, fmt.Errorf("%w: stable vector", ErrInvalidSnapshot)
	}
	doc := Snapshot{
		V:                snapshotVersion,
		SnapshotID:       uuid.NewString(),
		SpaceID:          r.spaceID,
		SchemaVersion:    r.supportedSchemaVersion,
		CreatorDeviceID:  identity.PeerID(),
		CreatorPublicKey: encode(identity.PublicBytes()),
		CoverVector:      r.vector.Clone(),
	}
	recordOps := make([]VerifiedOp, 0, len(r.view))
	prunedTombstones := 0
	for _, op := range r.view {
		if op.Document.Kind == KindDelete && stable[op.Document.ActorDeviceID] >= op.Document.Counter {
			prunedTombstones++
			continue
		}
		recordOps = append(recordOps, op)
	}
	retainedByID := make(map[string]VerifiedOp, len(r.retained))
	for opID, op := range r.retained {
		retainedByID[opID] = op
	}
	for opID, op := range r.ops {
		if op.Document.SchemaVersion != r.supportedSchemaVersion {
			retainedByID[opID] = op
		}
	}
	r.mu.RUnlock()

	retainedOps := make([]VerifiedOp, 0, len(retainedByID))
	for _, op := range retainedByID {
		retainedOps = append(retainedOps, op)
	}
	sort.Slice(recordOps, func(i, j int) bool { return snapshotOpLess(recordOps[i], recordOps[j]) })
	sort.Slice(retainedOps, func(i, j int) bool { return snapshotOpLess(retainedOps[i], retainedOps[j]) })
	for _, op := range recordOps {
		doc.RecordOps = append(doc.RecordOps, op.Token)
	}
	for _, op := range retainedOps {
		doc.RetainedOps = append(doc.RetainedOps, op.Token)
	}
	if doc.RecordOps == nil {
		doc.RecordOps = []string{}
	}
	if doc.RetainedOps == nil {
		doc.RetainedOps = []string{}
	}
	doc.CreatedHLC = r.clock.Tick()
	token, err := signSnapshotDocument(doc, identity)
	return token, prunedTombstones, err
}

func (r *Replica) prunableTombstones(stable VersionVector) int {
	if stable == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, op := range r.view {
		if op.Document.Kind == KindDelete && stable[op.Document.ActorDeviceID] >= op.Document.Counter {
			count++
		}
	}
	return count
}

// VerifySnapshot validates the outer signature, vector, ordering, and every
// embedded operation before returning any state to a caller.
func VerifySnapshot(token string) (VerifiedSnapshot, error) {
	if len(token) == 0 || len(token) > maxSnapshotTokenBytes {
		return VerifiedSnapshot{}, fmt.Errorf("%w: token size", ErrInvalidSnapshot)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != snapshotTokenPrefix {
		return VerifiedSnapshot{}, fmt.Errorf("%w: token prefix", ErrInvalidSnapshot)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return VerifiedSnapshot{}, fmt.Errorf("%w: payload encoding", ErrInvalidSnapshot)
	}
	signature, err := decodeSnapshotSized(parts[2], peercrypto.SignatureSize, "signature")
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	var doc Snapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return VerifiedSnapshot{}, fmt.Errorf("%w: payload JSON: %v", ErrInvalidSnapshot, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil || !errors.Is(err, io.EOF) {
		return VerifiedSnapshot{}, fmt.Errorf("%w: trailing payload", ErrInvalidSnapshot)
	}
	canonical, err := json.Marshal(doc)
	if err != nil || !bytes.Equal(canonical, raw) {
		return VerifiedSnapshot{}, fmt.Errorf("%w: non-canonical payload", ErrInvalidSnapshot)
	}
	publicKey, err := decodeSnapshotSized(doc.CreatorPublicKey, peercrypto.PublicKeySize, "creator public key")
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := validateSnapshotHeader(doc, publicKey); err != nil {
		return VerifiedSnapshot{}, err
	}
	if err := peercrypto.Verify(publicKey, raw, signature); err != nil {
		return VerifiedSnapshot{}, fmt.Errorf("%w: signature: %v", ErrInvalidSnapshot, err)
	}

	verified := VerifiedSnapshot{Token: token, Document: doc, PublicKey: publicKey}
	seenOps := make(map[string]struct{}, len(doc.RecordOps)+len(doc.RetainedOps))
	seenRecords := make(map[recordKey]struct{}, len(doc.RecordOps))
	var previous *VerifiedOp
	for _, embedded := range doc.RecordOps {
		op, err := verifySnapshotOp(embedded, doc, seenOps)
		if err != nil {
			return VerifiedSnapshot{}, err
		}
		if op.Document.SchemaVersion != doc.SchemaVersion {
			return VerifiedSnapshot{}, fmt.Errorf("%w: record schema", ErrInvalidSnapshot)
		}
		if CompareTimestamp(op.Document.HLC, doc.CreatedHLC) > 0 {
			return VerifiedSnapshot{}, fmt.Errorf("%w: record newer than snapshot", ErrInvalidSnapshot)
		}
		key := recordKey{collection: op.Document.Collection, recordID: op.Document.RecordID}
		if _, duplicate := seenRecords[key]; duplicate {
			return VerifiedSnapshot{}, fmt.Errorf("%w: duplicate materialized record", ErrInvalidSnapshot)
		}
		seenRecords[key] = struct{}{}
		if previous != nil && !snapshotOpLess(*previous, op) {
			return VerifiedSnapshot{}, fmt.Errorf("%w: record operation order", ErrInvalidSnapshot)
		}
		current := op
		previous = &current
		verified.RecordOps = append(verified.RecordOps, op)
	}
	previous = nil
	for _, embedded := range doc.RetainedOps {
		op, err := verifySnapshotOp(embedded, doc, seenOps)
		if err != nil {
			return VerifiedSnapshot{}, err
		}
		if op.Document.SchemaVersion == doc.SchemaVersion {
			return VerifiedSnapshot{}, fmt.Errorf("%w: retained current-schema operation", ErrInvalidSnapshot)
		}
		if CompareTimestamp(op.Document.HLC, doc.CreatedHLC) > 0 {
			return VerifiedSnapshot{}, fmt.Errorf("%w: retained operation newer than snapshot", ErrInvalidSnapshot)
		}
		if previous != nil && !snapshotOpLess(*previous, op) {
			return VerifiedSnapshot{}, fmt.Errorf("%w: retained operation order", ErrInvalidSnapshot)
		}
		current := op
		previous = &current
		verified.RetainedOps = append(verified.RetainedOps, op)
	}
	return verified, nil
}

// InstallSnapshot replaces the empty replica's compaction base. Authorization
// of the snapshot creator against Space membership is the caller's job.
func (r *Replica) InstallSnapshot(token string) error {
	verified, err := VerifySnapshot(token)
	if err != nil {
		return err
	}
	if verified.Document.SpaceID != r.spaceID || verified.Document.SchemaVersion != r.supportedSchemaVersion {
		return fmt.Errorf("%w: space or schema", ErrInvalidSnapshot)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ops) != 0 || len(r.view) != 0 || len(r.vector) != 0 || len(r.compactedVector) != 0 {
		return ErrReplicaNotEmpty
	}
	if _, err := r.clock.Observe(verified.Document.CreatedHLC); err != nil {
		return err
	}
	r.compactedVector = verified.Document.CoverVector.Clone()
	r.vector = verified.Document.CoverVector.Clone()
	r.maxCounters = verified.Document.CoverVector.Clone()
	for _, op := range verified.RecordOps {
		op = cloneVerified(op)
		key := recordKey{collection: op.Document.Collection, recordID: op.Document.RecordID}
		r.view[key] = op
		r.compactedPayloadIDs[op.Document.OpID] = op.payloadID
	}
	for _, op := range verified.RetainedOps {
		op = cloneVerified(op)
		r.retained[op.Document.OpID] = op
		r.compactedPayloadIDs[op.Document.OpID] = op.payloadID
	}
	return nil
}

func signSnapshotDocument(doc Snapshot, identity *peercrypto.Identity) (string, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal config snapshot: %w", err)
	}
	signature, err := identity.Sign(raw)
	if err != nil {
		return "", err
	}
	return snapshotTokenPrefix + "." + encode(raw) + "." + encode(signature), nil
}

func validateSnapshotHeader(doc Snapshot, publicKey []byte) error {
	if doc.V != snapshotVersion || uuid.Validate(doc.SnapshotID) != nil || !validSpaceID(doc.SpaceID) || doc.SchemaVersion == 0 || doc.CreatedHLC.PhysicalMS <= 0 {
		return fmt.Errorf("%w: header", ErrInvalidSnapshot)
	}
	if peercrypto.PeerID(publicKey) != doc.CreatorDeviceID || !validPeerID(doc.CreatorDeviceID) {
		return fmt.Errorf("%w: creator identity", ErrInvalidSnapshot)
	}
	if doc.CoverVector == nil || doc.RecordOps == nil || doc.RetainedOps == nil || len(doc.CoverVector) > maxCausalActors || len(doc.RecordOps)+len(doc.RetainedOps) > maxSnapshotOps {
		return fmt.Errorf("%w: vector or operation count", ErrInvalidSnapshot)
	}
	for actor, counter := range doc.CoverVector {
		if !validPeerID(actor) || counter == 0 {
			return fmt.Errorf("%w: cover vector", ErrInvalidSnapshot)
		}
	}
	return nil
}

func verifySnapshotOp(token string, snapshot Snapshot, seen map[string]struct{}) (VerifiedOp, error) {
	op, err := VerifyOp(token)
	if err != nil {
		return VerifiedOp{}, fmt.Errorf("%w: embedded operation: %v", ErrInvalidSnapshot, err)
	}
	if op.Document.SpaceID != snapshot.SpaceID || snapshot.CoverVector[op.Document.ActorDeviceID] < op.Document.Counter || !snapshot.CoverVector.Covers(op.Document.CausalContext) {
		return VerifiedOp{}, fmt.Errorf("%w: uncovered operation", ErrInvalidSnapshot)
	}
	if _, duplicate := seen[op.Document.OpID]; duplicate {
		return VerifiedOp{}, fmt.Errorf("%w: duplicate operation", ErrInvalidSnapshot)
	}
	seen[op.Document.OpID] = struct{}{}
	return op, nil
}

func snapshotOpLess(a, b VerifiedOp) bool {
	if a.Document.Collection != b.Document.Collection {
		return a.Document.Collection < b.Document.Collection
	}
	if a.Document.RecordID != b.Document.RecordID {
		return a.Document.RecordID < b.Document.RecordID
	}
	if a.Document.ActorDeviceID != b.Document.ActorDeviceID {
		return a.Document.ActorDeviceID < b.Document.ActorDeviceID
	}
	return a.Document.Counter < b.Document.Counter
}

func decodeSnapshotSized(value string, size int, field string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != size || encode(decoded) != value {
		return nil, fmt.Errorf("%w: %s", ErrInvalidSnapshot, field)
	}
	return decoded, nil
}
