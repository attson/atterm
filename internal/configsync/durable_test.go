package configsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

func openTestDurable(t *testing.T, path, spaceID string) *DurableReplica {
	t.Helper()
	store, err := OpenDurableReplica(path, spaceID, 1, NewClockWithSource(func() time.Time {
		return time.UnixMilli(replicaNowMS)
	}, 10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestDurableReplicaSurvivesRestartAndCompaction(t *testing.T) {
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "config-replica.json")
	identity := testIdentity(t)
	remote := testIdentity(t)
	store := openTestDurable(t, path, spaceID)

	first, ack, err := store.Append(identity, testConfigMutation("theme", KindSet, "dark"))
	if err != nil {
		t.Fatal(err)
	}
	if ack.Vector[identity.PeerID()] != 1 || first.Document.Counter != 1 {
		t.Fatalf("first append ack=%#v op=%+v", ack, first.Document)
	}
	remoteToken := signTestOp(t, remote, spaceID, 1, replicaNowMS+10, ack.Vector, "locale", KindSet, "zh-CN", 1)
	if result, ack, err := store.Apply(remoteToken); err != nil || !result.Stored || ack.Vector[remote.PeerID()] != 1 {
		t.Fatalf("remote apply result=%+v ack=%#v err=%v", result, ack, err)
	}

	reopened := openTestDurable(t, path, spaceID)
	theme, _ := reopened.Get("preferences", "theme")
	locale, _ := reopened.Get("preferences", "locale")
	if string(theme.Payload) != "dark" || string(locale.Payload) != "zh-CN" {
		t.Fatalf("reopened records: theme=%+v locale=%+v", theme, locale)
	}

	snapshot, compactAck, err := reopened.Compact(identity)
	if err != nil {
		t.Fatal(err)
	}
	verifiedSnapshot, err := VerifySnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(compactAck.Vector, verifiedSnapshot.Document.CoverVector) {
		t.Fatalf("compact ack=%#v cover=%#v", compactAck.Vector, verifiedSnapshot.Document.CoverVector)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted durableState
	if err := strictDurableJSON(blob, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Snapshot == "" || len(persisted.Ops) != 0 {
		t.Fatalf("compacted disk state=%+v", persisted)
	}

	afterCompact := openTestDurable(t, path, spaceID)
	third, _, err := afterCompact.Append(identity, testConfigMutation("theme", KindSet, "light"))
	if err != nil {
		t.Fatal(err)
	}
	if third.Document.Counter != 2 {
		t.Fatalf("counter after compaction=%d want=2", third.Document.Counter)
	}
	behind := afterCompact.StateForPeer(nil)
	if behind.Snapshot == "" || len(behind.Ops) != 1 {
		t.Fatalf("state for peer behind snapshot=%t ops=%d", behind.Snapshot != "", len(behind.Ops))
	}
	atBoundary := afterCompact.StateForPeer(verifiedSnapshot.Document.CoverVector)
	if atBoundary.Snapshot != "" || len(atBoundary.Ops) != 1 || atBoundary.Ops[0] != third.Token {
		t.Fatalf("state at boundary snapshot=%t ops=%d", atBoundary.Snapshot != "", len(atBoundary.Ops))
	}
}

func TestDurableReplicaCrossProcessCounterAllocation(t *testing.T) {
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "config-replica.json")
	identity := testIdentity(t)
	stores := []*DurableReplica{openTestDurable(t, path, spaceID), openTestDurable(t, path, spaceID)}

	operations := make([]VerifiedOp, len(stores))
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			operations[index], _, errs[index] = stores[index].Append(identity, testConfigMutation("record-"+string(rune('a'+index)), KindSet, "value"))
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	counters := []int{int(operations[0].Document.Counter), int(operations[1].Document.Counter)}
	sort.Ints(counters)
	if !reflect.DeepEqual(counters, []int{1, 2}) {
		t.Fatalf("allocated counters=%v", counters)
	}
	if err := stores[0].Reload(); err != nil {
		t.Fatal(err)
	}
	if stores[0].Vector()[identity.PeerID()] != 2 {
		t.Fatalf("reloaded vector=%#v", stores[0].Vector())
	}
}

func TestDurableReplicaAdoptsSnapshotBeforeTail(t *testing.T) {
	spaceID := uuid.NewString()
	identity := testIdentity(t)
	sourcePath := filepath.Join(t.TempDir(), "source.json")
	source := openTestDurable(t, sourcePath, spaceID)
	if _, _, err := source.Append(identity, testConfigMutation("locale", KindSet, "en")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.Compact(identity); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.Append(identity, testConfigMutation("locale", KindSet, "zh-CN")); err != nil {
		t.Fatal(err)
	}
	transfer := source.StateForPeer(nil)

	destination := openTestDurable(t, filepath.Join(t.TempDir(), "destination.json"), spaceID)
	if _, err := destination.AdoptSnapshot(transfer.Snapshot); err != nil {
		t.Fatal(err)
	}
	for _, token := range transfer.Ops {
		if _, _, err := destination.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	locale, _ := destination.Get("preferences", "locale")
	if string(locale.Payload) != "zh-CN" || destination.Vector().Compare(source.Vector()) != VectorEqual {
		t.Fatalf("destination record=%+v vector=%#v source=%#v", locale, destination.Vector(), source.Vector())
	}
}

func TestDurableReplicaFailsClosedOnCorruptState(t *testing.T) {
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "config-replica.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"space_id":"`+spaceID+`","schema_version":1,"ops":[],"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDurableReplica(path, spaceID, 1, nil); !errors.Is(err, ErrDurableStoreInvalid) {
		t.Fatalf("corrupt store error=%v", err)
	}
}

func TestDurableReplicaFileIsPrivate(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem mode check")
	}
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "config-replica.json")
	store := openTestDurable(t, path, spaceID)
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Append(identity, testConfigMutation("locale", KindSet, "en")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store mode=%o want=600", info.Mode().Perm())
	}
}

func TestDurableReplicaEncryptsBeforePersisting(t *testing.T) {
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "config-replica.json")
	store := openTestDurable(t, path, spaceID)
	identity := testIdentity(t)
	key, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("plaintext-must-not-reach-disk")
	if _, _, err := store.AppendEncrypted(identity, key, Mutation{
		Collection: "preferences", RecordID: "locale", Kind: KindSet, Payload: plaintext,
	}); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, plaintext) {
		t.Fatal("durable replica contains plaintext config payload")
	}
	reopened := openTestDurable(t, path, spaceID)
	record, ok := reopened.Get("preferences", "locale")
	if !ok {
		t.Fatal("encrypted durable record missing")
	}
	opened, err := OpenRecordPayload(key, record)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("opened durable payload=%q err=%v", opened, err)
	}
}

func TestDurableReplicaAppendEncryptedBatchIsAtomic(t *testing.T) {
	spaceID := uuid.NewString()
	path := filepath.Join(t.TempDir(), "replica.json")
	store := openTestDurable(t, path, spaceID)
	identity := testIdentity(t)
	syncKey, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	vaultKey, err := GenerateEpochKey(KeyClassVault, 1)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []EncryptedMutation{
		{Key: syncKey, Mutation: Mutation{
			Collection: CollectionProfiles, RecordID: "profile-1", Kind: KindSet,
			KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte(`{"position":"0000000000000000","value":{"id":"profile-1","name":"Work"}}`),
		}},
		{Key: vaultKey, Mutation: Mutation{
			Collection: CollectionProfileEnv, RecordID: "profile-1", Kind: KindSet,
			KeyClass: KeyClassVault, KeyEpoch: 1, Payload: []byte(`{"env":{"TOKEN":"secret"},"id":"profile-1"}`),
		}},
	}
	operations, ack, err := store.AppendEncryptedBatch(identity, mutations)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 2 || operations[0].Document.Counter != 1 || operations[1].Document.Counter != 2 || ack.Vector[identity.PeerID()] != 2 {
		t.Fatalf("operations=%+v ack=%+v", operations, ack)
	}

	bad := append([]EncryptedMutation(nil), mutations...)
	bad[0].Mutation.RecordID = "profile-2"
	bad[1].Mutation.Collection = "INVALID COLLECTION"
	if _, _, err := store.AppendEncryptedBatch(identity, bad); err == nil {
		t.Fatal("batch with invalid second mutation succeeded")
	}
	if vector := store.Vector(); vector[identity.PeerID()] != 2 {
		t.Fatalf("failed batch advanced vector: %v", vector)
	}
	if _, ok := store.Get(CollectionProfiles, "profile-2"); ok {
		t.Fatal("first mutation from failed batch reached durable view")
	}
	reopened := openTestDurable(t, path, spaceID)
	if vector := reopened.Vector(); vector[identity.PeerID()] != 2 {
		t.Fatalf("failed batch reached disk: %v", vector)
	}
}
