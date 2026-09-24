package configsync

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/attson/atterm/internal/peercrypto"
)

var (
	ErrWrongSpace        = errors.New("configsync: operation belongs to another space")
	ErrReplicaNotEmpty   = errors.New("configsync: snapshot requires an empty replica")
	ErrIncompleteHistory = errors.New("configsync: operation history has counter gaps")
)

// ApplyResult describes whether an inbound token changed durable replica
// state and its materialized view.
type ApplyResult struct {
	Stored       bool
	Materialized bool
	Duplicate    bool
	Compacted    bool
}

// Record is one materialized scalar or map entity. A deleted record remains
// visible as a tombstone so concurrent stale sets cannot resurrect it.
type Record struct {
	Collection    string
	RecordID      string
	Deleted       bool
	Payload       []byte
	OpID          string
	ActorDeviceID string
	Counter       uint64
	HLC           Timestamp
}

type recordKey struct {
	collection string
	recordID   string
}

// Replica is the in-memory canonical operation set and materialized view.
// Persistence, snapshots, and network anti-entropy are layered on this core.
type Replica struct {
	mu                     sync.RWMutex
	localMu                sync.Mutex
	spaceID                string
	supportedSchemaVersion uint32
	clock                  *Clock
	ops                    map[string]VerifiedOp
	seenCounters           map[string]map[uint64]struct{}
	maxCounters            VersionVector
	vector                 VersionVector
	view                   map[recordKey]VerifiedOp
	compactedVector        VersionVector
	compactedPayloadIDs    map[string][32]byte
	retained               map[string]VerifiedOp
}

// NewReplica creates an empty replica. Only operations matching
// supportedSchemaVersion are materialized; other valid operations are kept so
// upgraded peers can still retrieve them.
func NewReplica(spaceID string, supportedSchemaVersion uint32, clock *Clock) (*Replica, error) {
	if !validSpaceID(spaceID) || supportedSchemaVersion == 0 {
		return nil, fmt.Errorf("%w: space or supported schema version", ErrInvalidOp)
	}
	if clock == nil {
		clock = NewClock(DefaultMaxFutureSkew)
	}
	return &Replica{
		spaceID:                spaceID,
		supportedSchemaVersion: supportedSchemaVersion,
		clock:                  clock,
		ops:                    make(map[string]VerifiedOp),
		seenCounters:           make(map[string]map[uint64]struct{}),
		maxCounters:            make(VersionVector),
		vector:                 make(VersionVector),
		view:                   make(map[recordKey]VerifiedOp),
		compactedVector:        make(VersionVector),
		compactedPayloadIDs:    make(map[string][32]byte),
		retained:               make(map[string]VerifiedOp),
	}, nil
}

// Apply verifies and stores one operation. Duplicate delivery is idempotent;
// a different operation at the same actor counter is a permanent fork error.
func (r *Replica) Apply(token string) (ApplyResult, error) {
	verified, err := VerifyOp(token)
	if err != nil {
		return ApplyResult{}, err
	}
	if verified.Document.SpaceID != r.spaceID {
		return ApplyResult{}, ErrWrongSpace
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	doc := verified.Document
	if doc.Counter <= r.compactedVector[doc.ActorDeviceID] {
		if payloadID, known := r.compactedPayloadIDs[doc.OpID]; known && payloadID != verified.payloadID {
			return ApplyResult{}, fmt.Errorf("%w: actor=%s counter=%d", ErrCounterFork, doc.ActorDeviceID, doc.Counter)
		}
		return ApplyResult{Duplicate: true, Compacted: true}, nil
	}
	if existing, ok := r.ops[doc.OpID]; ok {
		if existing.payloadID == verified.payloadID {
			return ApplyResult{Duplicate: true}, nil
		}
		return ApplyResult{}, fmt.Errorf("%w: actor=%s counter=%d", ErrCounterFork, doc.ActorDeviceID, doc.Counter)
	}
	if _, err := r.clock.Observe(doc.HLC); err != nil {
		return ApplyResult{}, err
	}

	verified = cloneVerified(verified)
	r.ops[doc.OpID] = verified
	if r.seenCounters[doc.ActorDeviceID] == nil {
		r.seenCounters[doc.ActorDeviceID] = make(map[uint64]struct{})
	}
	r.seenCounters[doc.ActorDeviceID][doc.Counter] = struct{}{}
	if doc.Counter > r.maxCounters[doc.ActorDeviceID] {
		r.maxCounters[doc.ActorDeviceID] = doc.Counter
	}
	for {
		next := r.vector[doc.ActorDeviceID] + 1
		if _, ok := r.seenCounters[doc.ActorDeviceID][next]; !ok {
			break
		}
		r.vector[doc.ActorDeviceID] = next
	}

	result := ApplyResult{Stored: true}
	if doc.SchemaVersion != r.supportedSchemaVersion {
		return result, nil
	}
	key := recordKey{collection: doc.Collection, recordID: doc.RecordID}
	current, exists := r.view[key]
	if !exists || operationWins(doc, current.Document) {
		r.view[key] = verified
		result.Materialized = true
	}
	return result, nil
}

// Append creates and applies the next operation for identity.
func (r *Replica) Append(identity *peercrypto.Identity, mutation Mutation) (VerifiedOp, error) {
	if identity == nil {
		return VerifiedOp{}, fmt.Errorf("%w: missing identity", ErrInvalidOp)
	}
	r.localMu.Lock()
	defer r.localMu.Unlock()
	r.mu.RLock()
	next := r.maxCounters[identity.PeerID()] + 1
	r.mu.RUnlock()
	return r.appendAtCounter(identity, next, mutation)
}

// AppendAtCounter is the persistence boundary for callers that own a durable
// device counter. A mismatch stops sync instead of silently reusing an op ID.
func (r *Replica) AppendAtCounter(identity *peercrypto.Identity, counter uint64, mutation Mutation) (VerifiedOp, error) {
	r.localMu.Lock()
	defer r.localMu.Unlock()
	return r.appendAtCounter(identity, counter, mutation)
}

func (r *Replica) appendAtCounter(identity *peercrypto.Identity, counter uint64, mutation Mutation) (VerifiedOp, error) {
	if identity == nil {
		return VerifiedOp{}, fmt.Errorf("%w: missing identity", ErrInvalidOp)
	}
	actor := identity.PeerID()
	r.mu.RLock()
	expected := r.maxCounters[actor] + 1
	causal := r.vector.Clone()
	r.mu.RUnlock()
	if counter != expected || causal[actor] != counter-1 {
		return VerifiedOp{}, fmt.Errorf("%w: actor=%s got=%d want=%d", ErrCounterRollback, actor, counter, expected)
	}
	if mutation.SchemaVersion == 0 {
		mutation.SchemaVersion = r.supportedSchemaVersion
	}
	token, err := SignOp(identity, r.spaceID, counter, r.clock.Tick(), causal, mutation)
	if err != nil {
		return VerifiedOp{}, err
	}
	if _, err := r.Apply(token); err != nil {
		return VerifiedOp{}, err
	}
	return VerifyOp(token)
}

// Vector returns the greatest contiguous counters stored by this replica.
func (r *Replica) Vector() VersionVector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.vector.Clone()
}

// MissingTokens returns stored tokens not covered by the remote vector in a
// stable actor/counter order. Batch sizing remains the adapter's concern.
func (r *Replica) MissingTokens(remote VersionVector) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	type item struct {
		actor   string
		counter uint64
		token   string
	}
	items := make([]item, 0)
	for _, op := range r.ops {
		doc := op.Document
		if doc.Counter > remote[doc.ActorDeviceID] {
			items = append(items, item{actor: doc.ActorDeviceID, counter: doc.Counter, token: op.Token})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].actor != items[j].actor {
			return items[i].actor < items[j].actor
		}
		return items[i].counter < items[j].counter
	})
	out := make([]string, len(items))
	for i := range items {
		out[i] = items[i].token
	}
	return out
}

// CompactedVector returns history represented only by an installed snapshot.
// A peer behind this vector needs the snapshot before tail operations.
func (r *Replica) CompactedVector() VersionVector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.compactedVector.Clone()
}

// Get returns one materialized record, including tombstones.
func (r *Replica) Get(collection, recordID string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	op, ok := r.view[recordKey{collection: collection, recordID: recordID}]
	if !ok {
		return Record{}, false
	}
	doc := op.Document
	return Record{
		Collection: doc.Collection, RecordID: doc.RecordID,
		Deleted: doc.Kind == KindDelete, Payload: append([]byte(nil), doc.Payload...),
		OpID: doc.OpID, ActorDeviceID: doc.ActorDeviceID,
		Counter: doc.Counter, HLC: doc.HLC,
	}, true
}

func operationWins(candidate, current SyncOp) bool {
	candidateSawCurrent := candidate.CausalContext[current.ActorDeviceID] >= current.Counter
	currentSawCandidate := current.CausalContext[candidate.ActorDeviceID] >= candidate.Counter
	if candidateSawCurrent && !currentSawCandidate {
		return true
	}
	if currentSawCandidate && !candidateSawCurrent {
		return false
	}
	if candidate.Kind != current.Kind {
		return candidate.Kind == KindDelete
	}
	if compared := CompareTimestamp(candidate.HLC, current.HLC); compared != 0 {
		return compared > 0
	}
	if candidate.ActorDeviceID != current.ActorDeviceID {
		return candidate.ActorDeviceID > current.ActorDeviceID
	}
	return candidate.OpID > current.OpID
}

func cloneVerified(op VerifiedOp) VerifiedOp {
	op.PublicKey = append([]byte(nil), op.PublicKey...)
	op.Document.Payload = append([]byte(nil), op.Document.Payload...)
	op.Document.CausalContext = op.Document.CausalContext.Clone()
	return op
}

func (r *Replica) replaceState(other *Replica) {
	other.mu.RLock()
	defer other.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spaceID = other.spaceID
	r.supportedSchemaVersion = other.supportedSchemaVersion
	r.clock = other.clock
	r.ops = other.ops
	r.seenCounters = other.seenCounters
	r.maxCounters = other.maxCounters
	r.vector = other.vector
	r.view = other.view
	r.compactedVector = other.compactedVector
	r.compactedPayloadIDs = other.compactedPayloadIDs
	r.retained = other.retained
}
