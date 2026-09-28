package configsync

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"

	"github.com/attson/atterm/internal/peerproto"
)

const (
	AntiEntropyVersion          = 1
	DefaultAntiEntropyBatchSize = 256 << 10
	MinAntiEntropyBatchSize     = 1 << 10
	MaxAntiEntropyBatchSize     = 16 << 20
	maxAntiEntropyMemberships   = 256
	maxAntiEntropyItems         = 100_000 + maxAntiEntropyMemberships + 2*maxEpochRotationCandidates
)

var (
	ErrInvalidAntiEntropy = errors.New("configsync: invalid anti-entropy state")
	ErrAntiEntropyOrder   = errors.New("configsync: anti-entropy batch out of order")
	ErrAntiEntropyLimit   = errors.New("configsync: anti-entropy size limit")
)

// AntiEntropyItemKind identifies one independently signed token stream.
type AntiEntropyItemKind string

const (
	AntiEntropyMembership AntiEntropyItemKind = "membership"
	AntiEntropySnapshot   AntiEntropyItemKind = "snapshot"
	AntiEntropyOperation  AntiEntropyItemKind = "operation"
	AntiEntropyRevocation AntiEntropyItemKind = "revocation"
	AntiEntropyRotation   AntiEntropyItemKind = "rotation"
)

func (kind AntiEntropyItemKind) valid() bool {
	return kind == AntiEntropyMembership || kind == AntiEntropySnapshot || kind == AntiEntropyOperation || kind == AntiEntropyRevocation || kind == AntiEntropyRotation
}

// AntiEntropyInventory is the compact state advertised by a receiving peer.
// Governance hashes are sorted and unique; configuration history uses the
// contiguous version vector instead of an unbounded operation hash list.
type AntiEntropyInventory struct {
	V                int           `json:"v"`
	SpaceID          string        `json:"space_id"`
	Vector           VersionVector `json:"vector"`
	MembershipHashes []string      `json:"membership_hashes"`
	RevocationHashes []string      `json:"revocation_hashes"`
	RotationHashes   []string      `json:"rotation_hashes"`
}

// AntiEntropyCursor identifies an exact item byte boundary in an immutable
// plan. It is safe to echo as an acknowledgement cursor.
type AntiEntropyCursor struct {
	Item   int `json:"item"`
	Offset int `json:"offset"`
}

// AntiEntropyChunk carries a contiguous substring of one signed token.
type AntiEntropyChunk struct {
	Kind      AntiEntropyItemKind `json:"kind"`
	TokenHash string              `json:"token_hash"`
	Offset    int                 `json:"offset"`
	Total     int                 `json:"total"`
	Data      string              `json:"data"`
}

// AntiEntropyBatch is bounded by its encoded JSON size. Start and Next make
// acknowledgement and retry behavior independent of transport framing.
type AntiEntropyBatch struct {
	V       int                `json:"v"`
	SpaceID string             `json:"space_id"`
	Start   AntiEntropyCursor  `json:"start"`
	Next    *AntiEntropyCursor `json:"next,omitempty"`
	Done    bool               `json:"done"`
	Ack     DurableAck         `json:"ack"`
	Chunks  []AntiEntropyChunk `json:"chunks"`
}

// AntiEntropyItem is emitted only after the complete token hash, signature,
// Space binding, and document structure have been verified.
type AntiEntropyItem struct {
	Kind      AntiEntropyItemKind
	TokenHash string
	Token     string
}

type antiEntropyPlanItem struct {
	kind  AntiEntropyItemKind
	hash  string
	token string
}

// AntiEntropyPlan is an immutable, deterministic transfer view. Create a new
// plan when local durable state changes.
type AntiEntropyPlan struct {
	spaceID  string
	ack      DurableAck
	items    []antiEntropyPlanItem
	maxBytes int
}

// PlanAntiEntropy snapshots the durable config delta selected by the remote
// vector and combines it with missing governance tokens.
func (d *DurableReplica) PlanAntiEntropy(genesis peerproto.VerifiedGenesis, memberships, revocations, rotations []string, remote AntiEntropyInventory, maxBatchBytes int) (*AntiEntropyPlan, error) {
	if d == nil {
		return nil, ErrInvalidAntiEntropy
	}
	return NewAntiEntropyPlan(genesis, d.StateForPeer(remote.Vector), memberships, revocations, rotations, remote, maxBatchBytes)
}

// BuildAntiEntropyInventory verifies governance tokens and advertises their
// content hashes with the local durable configuration frontier.
func BuildAntiEntropyInventory(genesis peerproto.VerifiedGenesis, vector VersionVector, memberships, revocations, rotations []string) (AntiEntropyInventory, error) {
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil || validateAntiEntropyVector(vector) != nil {
		return AntiEntropyInventory{}, ErrInvalidAntiEntropy
	}
	membershipHashes, err := verifiedTokenHashes(verifiedGenesis, AntiEntropyMembership, memberships)
	if err != nil {
		return AntiEntropyInventory{}, err
	}
	revocationHashes, err := verifiedTokenHashes(verifiedGenesis, AntiEntropyRevocation, revocations)
	if err != nil {
		return AntiEntropyInventory{}, err
	}
	rotationHashes, err := verifiedTokenHashes(verifiedGenesis, AntiEntropyRotation, rotations)
	if err != nil {
		return AntiEntropyInventory{}, err
	}
	return AntiEntropyInventory{
		V: AntiEntropyVersion, SpaceID: verifiedGenesis.Document.SpaceID, Vector: vector.Clone(),
		MembershipHashes: membershipHashes, RevocationHashes: revocationHashes, RotationHashes: rotationHashes,
	}, nil
}

// NewAntiEntropyPlan verifies an outbound durable state and removes governance
// tokens already named by the receiver inventory.
func NewAntiEntropyPlan(genesis peerproto.VerifiedGenesis, state SyncState, memberships, revocations, rotations []string, remote AntiEntropyInventory, maxBatchBytes int) (*AntiEntropyPlan, error) {
	verifiedGenesis, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil || validateAntiEntropyInventory(remote, verifiedGenesis.Document.SpaceID) != nil || state.Ack.SpaceID != verifiedGenesis.Document.SpaceID || validateAntiEntropyVector(state.Ack.Vector) != nil {
		return nil, ErrInvalidAntiEntropy
	}
	if maxBatchBytes == 0 {
		maxBatchBytes = DefaultAntiEntropyBatchSize
	}
	if maxBatchBytes < MinAntiEntropyBatchSize || maxBatchBytes > MaxAntiEntropyBatchSize {
		return nil, ErrAntiEntropyLimit
	}
	if len(memberships) > maxAntiEntropyMemberships || len(revocations) > maxEpochRotationCandidates || len(rotations) > maxEpochRotationCandidates {
		return nil, ErrAntiEntropyLimit
	}
	if err := validateAntiEntropySyncState(state, remote.Vector, verifiedGenesis.Document.SpaceID); err != nil {
		return nil, err
	}
	items := make([]antiEntropyPlanItem, 0, len(memberships)+len(state.Ops)+len(revocations)+len(rotations)+1)
	knownMemberships := hashSet(remote.MembershipHashes)
	membershipItems := make([]antiEntropyPlanItem, 0, len(memberships))
	for _, token := range memberships {
		if _, err := peerproto.VerifyGrantAtIssuance(token, verifiedGenesis); err != nil {
			return nil, ErrInvalidAntiEntropy
		}
		hash := tokenDigest(token)
		if _, known := knownMemberships[hash]; !known {
			membershipItems = append(membershipItems, antiEntropyPlanItem{kind: AntiEntropyMembership, hash: hash, token: token})
			knownMemberships[hash] = struct{}{}
		}
	}
	sort.Slice(membershipItems, func(i, j int) bool { return membershipItems[i].hash < membershipItems[j].hash })
	items = append(items, membershipItems...)
	if state.Snapshot != "" {
		verified, err := VerifySnapshot(state.Snapshot)
		if err != nil || verified.Document.SpaceID != verifiedGenesis.Document.SpaceID {
			return nil, ErrInvalidAntiEntropy
		}
		items = append(items, newAntiEntropyPlanItem(AntiEntropySnapshot, state.Snapshot))
	}
	for _, token := range state.Ops {
		verified, err := VerifyOp(token)
		if err != nil || verified.Document.SpaceID != verifiedGenesis.Document.SpaceID {
			return nil, ErrInvalidAntiEntropy
		}
		items = append(items, newAntiEntropyPlanItem(AntiEntropyOperation, token))
	}
	knownRevocations := hashSet(remote.RevocationHashes)
	revocationItems := make([]antiEntropyPlanItem, 0, len(revocations))
	for _, token := range revocations {
		verified, err := peerproto.VerifyRevocation(token, verifiedGenesis)
		if err != nil {
			return nil, ErrInvalidAntiEntropy
		}
		if _, known := knownRevocations[verified.Hash]; !known {
			revocationItems = append(revocationItems, antiEntropyPlanItem{kind: AntiEntropyRevocation, hash: verified.Hash, token: token})
			knownRevocations[verified.Hash] = struct{}{}
		}
	}
	sort.Slice(revocationItems, func(i, j int) bool { return revocationItems[i].hash < revocationItems[j].hash })
	items = append(items, revocationItems...)
	knownRotations := hashSet(remote.RotationHashes)
	rotationItems := make([]antiEntropyPlanItem, 0, len(rotations))
	for _, token := range rotations {
		verified, err := VerifyEpochRotation(token, verifiedGenesis)
		if err != nil {
			return nil, ErrInvalidAntiEntropy
		}
		if _, known := knownRotations[verified.Hash]; !known {
			rotationItems = append(rotationItems, antiEntropyPlanItem{kind: AntiEntropyRotation, hash: verified.Hash, token: token})
			knownRotations[verified.Hash] = struct{}{}
		}
	}
	sort.Slice(rotationItems, func(i, j int) bool { return rotationItems[i].hash < rotationItems[j].hash })
	items = append(items, rotationItems...)
	if len(items) > maxAntiEntropyItems {
		return nil, ErrAntiEntropyLimit
	}
	return &AntiEntropyPlan{
		spaceID: verifiedGenesis.Document.SpaceID,
		ack:     DurableAck{SpaceID: state.Ack.SpaceID, Vector: state.Ack.Vector.Clone()},
		items:   items, maxBytes: maxBatchBytes,
	}, nil
}

// Next returns the largest next batch that stays within the configured exact
// JSON byte limit. A nil cursor starts the plan.
func (plan *AntiEntropyPlan) Next(cursor *AntiEntropyCursor) (AntiEntropyBatch, error) {
	if plan == nil {
		return AntiEntropyBatch{}, ErrInvalidAntiEntropy
	}
	start := AntiEntropyCursor{}
	if cursor != nil {
		start = *cursor
	}
	if !plan.validCursor(start) {
		return AntiEntropyBatch{}, ErrAntiEntropyOrder
	}
	batch := AntiEntropyBatch{
		V: AntiEntropyVersion, SpaceID: plan.spaceID, Start: start,
		Ack:    DurableAck{SpaceID: plan.ack.SpaceID, Vector: plan.ack.Vector.Clone()},
		Chunks: []AntiEntropyChunk{},
	}
	position := start
	for position.Item < len(plan.items) {
		item := plan.items[position.Item]
		remaining := len(item.token) - position.Offset
		chunkLength := plan.largestChunk(batch, item, position, remaining)
		if chunkLength == 0 {
			if len(batch.Chunks) == 0 {
				return AntiEntropyBatch{}, ErrAntiEntropyLimit
			}
			break
		}
		batch.Chunks = append(batch.Chunks, AntiEntropyChunk{
			Kind: item.kind, TokenHash: item.hash, Offset: position.Offset,
			Total: len(item.token), Data: item.token[position.Offset : position.Offset+chunkLength],
		})
		position.Offset += chunkLength
		if position.Offset == len(item.token) {
			position.Item++
			position.Offset = 0
		}
	}
	batch.Done = position.Item == len(plan.items)
	if !batch.Done {
		next := position
		batch.Next = &next
	}
	encoded, err := json.Marshal(batch)
	if err != nil || len(encoded) > plan.maxBytes {
		return AntiEntropyBatch{}, ErrAntiEntropyLimit
	}
	return batch, nil
}

func (plan *AntiEntropyPlan) largestChunk(batch AntiEntropyBatch, item antiEntropyPlanItem, position AntiEntropyCursor, remaining int) int {
	low, high := 1, remaining
	best := 0
	for low <= high {
		middle := low + (high-low)/2
		candidate := batch
		candidate.Chunks = append(append([]AntiEntropyChunk(nil), batch.Chunks...), AntiEntropyChunk{
			Kind: item.kind, TokenHash: item.hash, Offset: position.Offset,
			Total: len(item.token), Data: item.token[position.Offset : position.Offset+middle],
		})
		after := position
		after.Offset += middle
		if after.Offset == len(item.token) {
			after.Item++
			after.Offset = 0
		}
		candidate.Done = after.Item == len(plan.items)
		if !candidate.Done {
			candidate.Next = &after
		}
		encoded, err := json.Marshal(candidate)
		if err == nil && len(encoded) <= plan.maxBytes {
			best = middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return best
}

func (plan *AntiEntropyPlan) validCursor(cursor AntiEntropyCursor) bool {
	if cursor.Item < 0 || cursor.Item > len(plan.items) || cursor.Offset < 0 {
		return false
	}
	if cursor.Item == len(plan.items) {
		return cursor.Offset == 0
	}
	return cursor.Offset < len(plan.items[cursor.Item].token)
}

type antiEntropyAssembly struct {
	kind  AntiEntropyItemKind
	hash  string
	total int
	data  []byte
}

// AntiEntropyAssembler validates ordered batches and emits complete signed
// tokens. Replaying the most recently accepted batch is an idempotent no-op.
type AntiEntropyAssembler struct {
	genesis      peerproto.VerifiedGenesis
	spaceID      string
	expected     AntiEntropyCursor
	current      *antiEntropyAssembly
	terminal     bool
	lastStart    AntiEntropyCursor
	lastDigest   [sha256.Size]byte
	hasLastBatch bool
	ack          *DurableAck
	lastKindRank int
	seenSnapshot bool
}

// NewAntiEntropyAssembler creates a receiver anchored to one Space genesis.
func NewAntiEntropyAssembler(genesis peerproto.VerifiedGenesis) (*AntiEntropyAssembler, error) {
	verified, err := peerproto.VerifyGenesis(genesis.Token)
	if err != nil {
		return nil, ErrInvalidAntiEntropy
	}
	return &AntiEntropyAssembler{genesis: verified, spaceID: verified.Document.SpaceID, lastKindRank: -1}, nil
}

// Add accepts one batch at the expected cursor. State advances only after the
// entire batch validates, so malformed input cannot poison a later retry.
func (assembler *AntiEntropyAssembler) Add(batch AntiEntropyBatch) ([]AntiEntropyItem, error) {
	if assembler == nil || validateAntiEntropyBatchHeader(batch, assembler.spaceID) != nil {
		return nil, ErrInvalidAntiEntropy
	}
	encoded, err := json.Marshal(batch)
	if err != nil || len(encoded) > MaxAntiEntropyBatchSize {
		return nil, ErrAntiEntropyLimit
	}
	digest := sha256.Sum256(encoded)
	if assembler.hasLastBatch && batch.Start == assembler.lastStart && digest == assembler.lastDigest {
		return []AntiEntropyItem{}, nil
	}
	if assembler.terminal || batch.Start != assembler.expected {
		return nil, ErrAntiEntropyOrder
	}
	if assembler.ack != nil && assembler.ack.Vector.Compare(batch.Ack.Vector) != VectorEqual {
		return nil, ErrAntiEntropyOrder
	}
	position := assembler.expected
	current := cloneAntiEntropyAssembly(assembler.current)
	emitted := make([]AntiEntropyItem, 0)
	lastKindRank := assembler.lastKindRank
	seenSnapshot := assembler.seenSnapshot
	for _, chunk := range batch.Chunks {
		if err := validateAntiEntropyChunk(chunk); err != nil {
			return nil, err
		}
		if current == nil {
			if chunk.Offset != 0 {
				return nil, ErrAntiEntropyOrder
			}
			current = &antiEntropyAssembly{kind: chunk.Kind, hash: chunk.TokenHash, total: chunk.Total}
		}
		if chunk.Kind != current.kind || chunk.TokenHash != current.hash || chunk.Total != current.total || chunk.Offset != len(current.data) || len(current.data)+len(chunk.Data) > current.total {
			return nil, ErrAntiEntropyOrder
		}
		current.data = append(current.data, chunk.Data...)
		position.Offset = len(current.data)
		if len(current.data) == current.total {
			item, err := assembler.verifyItem(current.kind, current.hash, string(current.data))
			if err != nil {
				return nil, err
			}
			rank := antiEntropyKindRank(item.Kind)
			if rank < lastKindRank || item.Kind == AntiEntropySnapshot && seenSnapshot {
				return nil, ErrAntiEntropyOrder
			}
			if item.Kind == AntiEntropySnapshot {
				seenSnapshot = true
			}
			lastKindRank = rank
			emitted = append(emitted, item)
			position.Item++
			position.Offset = 0
			current = nil
		}
	}
	if batch.Done {
		if batch.Next != nil || current != nil {
			return nil, ErrAntiEntropyOrder
		}
	} else if batch.Next == nil || *batch.Next != position {
		return nil, ErrAntiEntropyOrder
	}
	assembler.expected = position
	assembler.current = current
	assembler.terminal = batch.Done
	assembler.lastStart = batch.Start
	assembler.lastDigest = digest
	assembler.hasLastBatch = true
	if assembler.ack == nil {
		assembler.ack = &DurableAck{SpaceID: batch.Ack.SpaceID, Vector: batch.Ack.Vector.Clone()}
	}
	assembler.lastKindRank = lastKindRank
	assembler.seenSnapshot = seenSnapshot
	return emitted, nil
}

func (assembler *AntiEntropyAssembler) verifyItem(kind AntiEntropyItemKind, expectedHash, token string) (AntiEntropyItem, error) {
	if tokenDigest(token) != expectedHash {
		return AntiEntropyItem{}, ErrInvalidAntiEntropy
	}
	switch kind {
	case AntiEntropyMembership:
		if _, err := peerproto.VerifyGrantAtIssuance(token, assembler.genesis); err != nil {
			return AntiEntropyItem{}, ErrInvalidAntiEntropy
		}
	case AntiEntropySnapshot:
		verified, err := VerifySnapshot(token)
		if err != nil || verified.Document.SpaceID != assembler.spaceID {
			return AntiEntropyItem{}, ErrInvalidAntiEntropy
		}
	case AntiEntropyOperation:
		verified, err := VerifyOp(token)
		if err != nil || verified.Document.SpaceID != assembler.spaceID {
			return AntiEntropyItem{}, ErrInvalidAntiEntropy
		}
	case AntiEntropyRevocation:
		if _, err := peerproto.VerifyRevocation(token, assembler.genesis); err != nil {
			return AntiEntropyItem{}, ErrInvalidAntiEntropy
		}
	case AntiEntropyRotation:
		if _, err := VerifyEpochRotation(token, assembler.genesis); err != nil {
			return AntiEntropyItem{}, ErrInvalidAntiEntropy
		}
	default:
		return AntiEntropyItem{}, ErrInvalidAntiEntropy
	}
	return AntiEntropyItem{Kind: kind, TokenHash: expectedHash, Token: token}, nil
}

func verifiedTokenHashes(genesis peerproto.VerifiedGenesis, kind AntiEntropyItemKind, tokens []string) ([]string, error) {
	limit := maxEpochRotationCandidates
	if kind == AntiEntropyMembership {
		limit = maxAntiEntropyMemberships
	}
	if len(tokens) > limit {
		return nil, ErrAntiEntropyLimit
	}
	hashes := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		var hash string
		switch kind {
		case AntiEntropyMembership:
			if _, err := peerproto.VerifyGrantAtIssuance(token, genesis); err != nil {
				return nil, ErrInvalidAntiEntropy
			}
			hash = tokenDigest(token)
		case AntiEntropyRevocation:
			verified, err := peerproto.VerifyRevocation(token, genesis)
			if err != nil {
				return nil, ErrInvalidAntiEntropy
			}
			hash = verified.Hash
		case AntiEntropyRotation:
			verified, err := VerifyEpochRotation(token, genesis)
			if err != nil {
				return nil, ErrInvalidAntiEntropy
			}
			hash = verified.Hash
		default:
			return nil, ErrInvalidAntiEntropy
		}
		if _, duplicate := seen[hash]; duplicate {
			continue
		}
		seen[hash] = struct{}{}
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return hashes, nil
}

func validateAntiEntropyInventory(inventory AntiEntropyInventory, spaceID string) error {
	if inventory.V != AntiEntropyVersion || inventory.SpaceID != spaceID || validateAntiEntropyVector(inventory.Vector) != nil || len(inventory.MembershipHashes) > maxAntiEntropyMemberships || len(inventory.RevocationHashes) > maxEpochRotationCandidates || len(inventory.RotationHashes) > maxEpochRotationCandidates {
		return ErrInvalidAntiEntropy
	}
	for _, hashes := range [][]string{inventory.MembershipHashes, inventory.RevocationHashes, inventory.RotationHashes} {
		previous := ""
		for _, hash := range hashes {
			if validateAntiEntropyHash(hash) != nil || hash <= previous {
				return ErrInvalidAntiEntropy
			}
			previous = hash
		}
	}
	return nil
}

func validateAntiEntropyBatchHeader(batch AntiEntropyBatch, spaceID string) error {
	if batch.V != AntiEntropyVersion || batch.SpaceID != spaceID || batch.Ack.SpaceID != spaceID || validateAntiEntropyVector(batch.Ack.Vector) != nil || batch.Start.Item < 0 || batch.Start.Item > maxAntiEntropyItems || batch.Start.Offset < 0 || len(batch.Chunks) > maxAntiEntropyItems {
		return ErrInvalidAntiEntropy
	}
	if batch.Done && batch.Next != nil || !batch.Done && batch.Next == nil {
		return ErrInvalidAntiEntropy
	}
	if !batch.Done && len(batch.Chunks) == 0 {
		return ErrInvalidAntiEntropy
	}
	if batch.Next != nil && (batch.Next.Item < 0 || batch.Next.Item > maxAntiEntropyItems || batch.Next.Offset < 0) {
		return ErrInvalidAntiEntropy
	}
	return nil
}

func validateAntiEntropySyncState(state SyncState, remote VersionVector, spaceID string) error {
	base := remote.Clone()
	if state.Snapshot != "" {
		verified, err := VerifySnapshot(state.Snapshot)
		if err != nil || verified.Document.SpaceID != spaceID || !verified.Document.CoverVector.Covers(remote) || !state.Ack.Vector.Covers(verified.Document.CoverVector) {
			return ErrInvalidAntiEntropy
		}
		base = verified.Document.CoverVector.Clone()
	}
	frontier := base.Clone()
	seen := make(map[string]struct{}, len(state.Ops))
	for _, token := range state.Ops {
		verified, err := VerifyOp(token)
		if err != nil || verified.Document.SpaceID != spaceID {
			return ErrInvalidAntiEntropy
		}
		doc := verified.Document
		if _, duplicate := seen[doc.OpID]; duplicate || doc.Counter != frontier[doc.ActorDeviceID]+1 || state.Ack.Vector[doc.ActorDeviceID] < doc.Counter || !state.Ack.Vector.Covers(doc.CausalContext) {
			return ErrInvalidAntiEntropy
		}
		seen[doc.OpID] = struct{}{}
		frontier[doc.ActorDeviceID] = doc.Counter
	}
	for actor, counter := range state.Ack.Vector {
		if counter > base[actor] && frontier[actor] != counter {
			return ErrInvalidAntiEntropy
		}
	}
	return nil
}

func validateAntiEntropyChunk(chunk AntiEntropyChunk) error {
	if !chunk.Kind.valid() || validateAntiEntropyHash(chunk.TokenHash) != nil || chunk.Offset < 0 || chunk.Total <= 0 || chunk.Offset >= chunk.Total || len(chunk.Data) == 0 || len(chunk.Data) > chunk.Total-chunk.Offset || chunk.Total > maxAntiEntropyTokenSize(chunk.Kind) {
		return ErrInvalidAntiEntropy
	}
	return nil
}

func validateAntiEntropyVector(vector VersionVector) error {
	if vector == nil || len(vector) > maxCausalActors {
		return ErrInvalidAntiEntropy
	}
	for actor, counter := range vector {
		if !validPeerID(actor) || counter == 0 {
			return ErrInvalidAntiEntropy
		}
	}
	return nil
}

func validateAntiEntropyHash(value string) error {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != sha256.Size || encode(decoded) != value {
		return ErrInvalidAntiEntropy
	}
	return nil
}

func maxAntiEntropyTokenSize(kind AntiEntropyItemKind) int {
	switch kind {
	case AntiEntropyMembership:
		return 64 << 10
	case AntiEntropySnapshot:
		return maxSnapshotTokenBytes
	case AntiEntropyOperation:
		return maxOpTokenBytes
	case AntiEntropyRevocation:
		return 64 << 10
	case AntiEntropyRotation:
		return maxEpochRotationBytes
	default:
		return 0
	}
}

func newAntiEntropyPlanItem(kind AntiEntropyItemKind, token string) antiEntropyPlanItem {
	return antiEntropyPlanItem{kind: kind, hash: tokenDigest(token), token: token}
}

func tokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return encode(digest[:])
}

func hashSet(hashes []string) map[string]struct{} {
	out := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		out[hash] = struct{}{}
	}
	return out
}

func cloneAntiEntropyAssembly(assembly *antiEntropyAssembly) *antiEntropyAssembly {
	if assembly == nil {
		return nil
	}
	clone := *assembly
	clone.data = append([]byte(nil), assembly.data...)
	return &clone
}

func antiEntropyKindRank(kind AntiEntropyItemKind) int {
	switch kind {
	case AntiEntropyMembership:
		return 0
	case AntiEntropySnapshot:
		return 1
	case AntiEntropyOperation:
		return 2
	case AntiEntropyRevocation:
		return 3
	case AntiEntropyRotation:
		return 4
	default:
		return -1
	}
}
