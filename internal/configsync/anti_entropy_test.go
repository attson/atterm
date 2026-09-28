package configsync

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

type antiEntropyFixture struct {
	rotationFixture *rotationFixture
	state           SyncState
	membership      peerproto.VerifiedGrant
	revocation      peerproto.VerifiedRevocation
	rotation        VerifiedEpochRotation
	expected        []AntiEntropyItem
}

func newAntiEntropyFixture(t *testing.T) antiEntropyFixture {
	t.Helper()
	fixture := newRotationFixture(t)
	clock := NewClockWithSource(func() time.Time { return fixture.now }, time.Hour)
	replica, err := NewReplica(fixture.genesis.Document.SpaceID, 1, clock)
	if err != nil {
		t.Fatal(err)
	}
	first, err := replica.Append(fixture.creator, Mutation{
		Collection: "preferences", RecordID: "large", Kind: KindSet,
		KeyClass: KeyClassSync, KeyEpoch: 1, Payload: make([]byte, 12<<10),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := replica.SignSnapshot(fixture.creator)
	if err != nil {
		t.Fatal(err)
	}
	second, err := replica.Append(fixture.creator, Mutation{
		Collection: "preferences", RecordID: "after-snapshot", Kind: KindSet,
		KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte("tail"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Document.Counter != 1 || second.Document.Counter != 2 {
		t.Fatalf("unexpected counters first=%d second=%d", first.Document.Counter, second.Document.Counter)
	}
	target, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	revocation, err := peerproto.NewRevocation(
		fixture.creator, fixture.genesis, fixture.creatorMembership,
		peerproto.RevocationMember, target.PeerID(), fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	rotation, err := NewEpochRotation(
		fixture.creator, fixture.genesis, fixture.creatorMembership, nil,
		key, fixture.members, fixture.now,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := SyncState{
		Snapshot: snapshot, Ops: []string{second.Token},
		Ack: DurableAck{SpaceID: fixture.genesis.Document.SpaceID, Vector: replica.Vector()},
	}
	return antiEntropyFixture{
		rotationFixture: fixture, state: state, revocation: revocation, rotation: rotation,
		membership: fixture.creatorMembership,
		expected: []AntiEntropyItem{
			{Kind: AntiEntropyMembership, TokenHash: tokenDigest(fixture.creatorMembership.Token), Token: fixture.creatorMembership.Token},
			{Kind: AntiEntropySnapshot, TokenHash: tokenDigest(snapshot), Token: snapshot},
			{Kind: AntiEntropyOperation, TokenHash: tokenDigest(second.Token), Token: second.Token},
			{Kind: AntiEntropyRevocation, TokenHash: revocation.Hash, Token: revocation.Token},
			{Kind: AntiEntropyRotation, TokenHash: rotation.Hash, Token: rotation.Token},
		},
	}
}

func TestAntiEntropyBatchesAreBoundedVerifiedAndRetryable(t *testing.T) {
	fixture := newAntiEntropyFixture(t)
	remote, err := BuildAntiEntropyInventory(fixture.rotationFixture.genesis, VersionVector{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const batchSize = 2048
	plan, err := NewAntiEntropyPlan(
		fixture.rotationFixture.genesis, fixture.state,
		[]string{fixture.membership.Token},
		[]string{fixture.revocation.Token}, []string{fixture.rotation.Token}, remote, batchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewAntiEntropyAssembler(fixture.rotationFixture.genesis)
	if err != nil {
		t.Fatal(err)
	}
	var cursor *AntiEntropyCursor
	var received []AntiEntropyItem
	batchCount := 0
	for {
		batch, err := plan.Next(cursor)
		if err != nil {
			t.Fatal(err)
		}
		batchCount++
		encoded, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > batchSize {
			t.Fatalf("batch bytes=%d limit=%d", len(encoded), batchSize)
		}
		items, err := assembler.Add(batch)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, items...)
		retryItems, err := assembler.Add(batch)
		if err != nil || len(retryItems) != 0 {
			t.Fatalf("retry items=%d err=%v", len(retryItems), err)
		}
		if batch.Done {
			break
		}
		next := *batch.Next
		cursor = &next
	}
	if batchCount < 2 {
		t.Fatal("large snapshot was not split across batches")
	}
	if !slices.EqualFunc(received, fixture.expected, func(left, right AntiEntropyItem) bool {
		return left.Kind == right.Kind && left.TokenHash == right.TokenHash && left.Token == right.Token
	}) {
		t.Fatalf("received=%+v expected=%+v", received, fixture.expected)
	}
}

func TestAntiEntropyInventoryFiltersKnownGovernanceTokens(t *testing.T) {
	fixture := newAntiEntropyFixture(t)
	remote, err := BuildAntiEntropyInventory(
		fixture.rotationFixture.genesis, VersionVector{},
		[]string{fixture.membership.Token, fixture.membership.Token},
		[]string{fixture.revocation.Token, fixture.revocation.Token},
		[]string{fixture.rotation.Token, fixture.rotation.Token},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(remote.MembershipHashes) != 1 || len(remote.RevocationHashes) != 1 || len(remote.RotationHashes) != 1 {
		t.Fatalf("inventory did not deduplicate hashes: %+v", remote)
	}
	plan, err := NewAntiEntropyPlan(
		fixture.rotationFixture.genesis, fixture.state,
		[]string{fixture.membership.Token},
		[]string{fixture.revocation.Token}, []string{fixture.rotation.Token}, remote, MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	assembler, _ := NewAntiEntropyAssembler(fixture.rotationFixture.genesis)
	items, err := assembler.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Kind != AntiEntropySnapshot || items[1].Kind != AntiEntropyOperation {
		t.Fatalf("known governance tokens were resent: %+v", items)
	}
}

func TestAntiEntropyRejectsTamperAndOutOfOrder(t *testing.T) {
	fixture := newAntiEntropyFixture(t)
	remote, _ := BuildAntiEntropyInventory(fixture.rotationFixture.genesis, VersionVector{}, nil, nil, nil)
	plan, err := NewAntiEntropyPlan(
		fixture.rotationFixture.genesis, fixture.state,
		[]string{fixture.membership.Token},
		[]string{fixture.revocation.Token}, []string{fixture.rotation.Token}, remote, MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	whole, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !whole.Done || len(whole.Chunks) == 0 {
		t.Fatalf("unexpected whole batch: %+v", whole)
	}
	tampered := whole
	tampered.Chunks = append([]AntiEntropyChunk(nil), whole.Chunks...)
	tampered.Chunks[0].Data = "x" + tampered.Chunks[0].Data[1:]
	assembler, _ := NewAntiEntropyAssembler(fixture.rotationFixture.genesis)
	if _, err := assembler.Add(tampered); !errors.Is(err, ErrInvalidAntiEntropy) {
		t.Fatalf("tampered batch error=%v", err)
	}
	items, err := assembler.Add(whole)
	if err != nil || len(items) != len(fixture.expected) {
		t.Fatalf("valid retry after tamper items=%d err=%v", len(items), err)
	}

	splitPlan, err := NewAntiEntropyPlan(
		fixture.rotationFixture.genesis, fixture.state,
		[]string{fixture.membership.Token},
		[]string{fixture.revocation.Token}, []string{fixture.rotation.Token}, remote, MinAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := splitPlan.Next(nil)
	if err != nil || first.Done || first.Next == nil {
		t.Fatalf("first split batch=%+v err=%v", first, err)
	}
	second, err := splitPlan.Next(first.Next)
	if err != nil {
		t.Fatal(err)
	}
	outOfOrder, _ := NewAntiEntropyAssembler(fixture.rotationFixture.genesis)
	if _, err := outOfOrder.Add(second); !errors.Is(err, ErrAntiEntropyOrder) {
		t.Fatalf("out-of-order batch error=%v", err)
	}
	if _, err := outOfOrder.Add(first); err != nil {
		t.Fatal(err)
	}
	changedAck := second
	changedAck.Ack.Vector = VersionVector{}
	if _, err := outOfOrder.Add(changedAck); !errors.Is(err, ErrAntiEntropyOrder) {
		t.Fatalf("changed ack error=%v", err)
	}
}

func TestAntiEntropyRejectsNonCanonicalInventoryAndCursor(t *testing.T) {
	fixture := newAntiEntropyFixture(t)
	remote, _ := BuildAntiEntropyInventory(fixture.rotationFixture.genesis, VersionVector{}, nil, nil, nil)
	remote.MembershipHashes = []string{tokenDigest(fixture.membership.Token), tokenDigest(fixture.membership.Token)}
	if _, err := NewAntiEntropyPlan(fixture.rotationFixture.genesis, fixture.state, nil, nil, nil, remote, 0); !errors.Is(err, ErrInvalidAntiEntropy) {
		t.Fatalf("duplicate membership inventory hash error=%v", err)
	}
	remote.MembershipHashes = nil
	remote.RevocationHashes = []string{fixture.revocation.Hash, fixture.revocation.Hash}
	if _, err := NewAntiEntropyPlan(fixture.rotationFixture.genesis, fixture.state, nil, nil, nil, remote, 0); !errors.Is(err, ErrInvalidAntiEntropy) {
		t.Fatalf("duplicate inventory hash error=%v", err)
	}
	remote.RevocationHashes = nil
	plan, err := NewAntiEntropyPlan(fixture.rotationFixture.genesis, fixture.state, nil, nil, nil, remote, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Next(&AntiEntropyCursor{Item: 0, Offset: -1}); !errors.Is(err, ErrAntiEntropyOrder) {
		t.Fatalf("invalid cursor error=%v", err)
	}
	concurrent, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	remote.Vector = VersionVector{concurrent.PeerID(): 1}
	if _, err := NewAntiEntropyPlan(fixture.rotationFixture.genesis, fixture.state, nil, nil, nil, remote, 0); !errors.Is(err, ErrInvalidAntiEntropy) {
		t.Fatalf("snapshot over concurrent receiver error=%v", err)
	}
}

func TestDurableReplicaPlansOnlyMissingTail(t *testing.T) {
	fixture := newRotationFixture(t)
	store := openTestDurable(t, filepath.Join(t.TempDir(), "replica.json"), fixture.genesis.Document.SpaceID)
	first, _, err := store.Append(fixture.creator, testConfigMutation("first", KindSet, "one"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Append(fixture.creator, testConfigMutation("second", KindSet, "two"))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := BuildAntiEntropyInventory(
		fixture.genesis, VersionVector{fixture.creator.PeerID(): first.Document.Counter}, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanAntiEntropy(fixture.genesis, nil, nil, nil, remote, 0)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	assembler, _ := NewAntiEntropyAssembler(fixture.genesis)
	items, err := assembler.Add(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != AntiEntropyOperation || items[0].Token != second.Token {
		t.Fatalf("planned items=%+v", items)
	}
}

func TestAntiEntropyRejectsTamperedMembershipWithoutAdvancing(t *testing.T) {
	fixture := newAntiEntropyFixture(t)
	remote, err := BuildAntiEntropyInventory(fixture.rotationFixture.genesis, VersionVector{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewAntiEntropyPlan(
		fixture.rotationFixture.genesis, fixture.state, []string{fixture.membership.Token}, nil, nil,
		remote, MaxAntiEntropyBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := plan.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Chunks) == 0 || batch.Chunks[0].Kind != AntiEntropyMembership {
		t.Fatalf("membership was not first: %+v", batch.Chunks)
	}
	tampered := batch
	tampered.Chunks = append([]AntiEntropyChunk(nil), batch.Chunks...)
	tampered.Chunks[0].Data = "x" + tampered.Chunks[0].Data[1:]
	assembler, err := NewAntiEntropyAssembler(fixture.rotationFixture.genesis)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembler.Add(tampered); !errors.Is(err, ErrInvalidAntiEntropy) {
		t.Fatalf("tampered membership error=%v", err)
	}
	items, err := assembler.Add(batch)
	if err != nil || len(items) == 0 || items[0].Kind != AntiEntropyMembership {
		t.Fatalf("valid retry items=%+v err=%v", items, err)
	}
}
