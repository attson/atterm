package configsync

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

const replicaNowMS = int64(1_800_000_000_000)

func newTestReplica(t *testing.T, spaceID string, schema uint32) *Replica {
	t.Helper()
	r, err := NewReplica(spaceID, schema, NewClockWithSource(func() time.Time {
		return time.UnixMilli(replicaNowMS)
	}, 10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func signTestOp(t *testing.T, id *peercrypto.Identity, spaceID string, counter uint64, physical int64, causal VersionVector, record string, kind Kind, payload string, schema uint32) string {
	t.Helper()
	token, err := SignOp(id, spaceID, counter, Timestamp{PhysicalMS: physical}, causal, Mutation{
		SchemaVersion: schema, Collection: "preferences", RecordID: record,
		Kind: kind, KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestReplicaRejectsCounterForkAndLocalRollback(t *testing.T) {
	spaceID := uuid.NewString()
	id := testIdentity(t)
	r := newTestReplica(t, spaceID, 1)
	one := signTestOp(t, id, spaceID, 1, replicaNowMS, nil, "locale", KindSet, "en", 1)
	fork := signTestOp(t, id, spaceID, 1, replicaNowMS, nil, "theme", KindSet, "dark", 1)
	if _, err := r.Apply(one); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(fork); !errors.Is(err, ErrCounterFork) {
		t.Fatalf("fork error = %v", err)
	}
	if _, err := r.AppendAtCounter(id, 1, testConfigMutation("theme", KindSet, "dark")); !errors.Is(err, ErrCounterRollback) {
		t.Fatalf("rollback error = %v", err)
	}
	if result, err := r.Apply(one); err != nil || !result.Duplicate || result.Stored {
		t.Fatalf("duplicate result=%+v err=%v", result, err)
	}
	verified, err := VerifyOp(one)
	if err != nil {
		t.Fatal(err)
	}
	resigned := resignTestDocument(t, id, verified.Document)
	if result, err := r.Apply(resigned); err != nil || !result.Duplicate {
		t.Fatalf("re-signed identical operation result=%+v err=%v", result, err)
	}
}

func TestReplicaAcceptsOutOfOrderOperationsWithoutAdvancingPastHole(t *testing.T) {
	spaceID := uuid.NewString()
	id := testIdentity(t)
	r := newTestReplica(t, spaceID, 1)
	op1 := signTestOp(t, id, spaceID, 1, replicaNowMS, nil, "locale", KindSet, "en", 1)
	op2 := signTestOp(t, id, spaceID, 2, replicaNowMS+1, VersionVector{id.PeerID(): 1}, "locale", KindSet, "zh-CN", 1)
	if _, err := r.Apply(op2); err != nil {
		t.Fatal(err)
	}
	if len(r.Vector()) != 0 {
		t.Fatalf("vector advanced across a hole: %#v", r.Vector())
	}
	if _, err := r.Apply(op1); err != nil {
		t.Fatal(err)
	}
	if got := r.Vector()[id.PeerID()]; got != 2 {
		t.Fatalf("contiguous counter = %d, want 2", got)
	}
	record, _ := r.Get("preferences", "locale")
	if string(record.Payload) != "zh-CN" {
		t.Fatalf("materialized payload = %q", record.Payload)
	}
}

func TestReplicaRemoveWinsConcurrentSet(t *testing.T) {
	spaceID := uuid.NewString()
	setter := testIdentity(t)
	remover := testIdentity(t)
	set := signTestOp(t, setter, spaceID, 1, replicaNowMS+200, nil, "theme", KindSet, "dark", 1)
	del := signTestOp(t, remover, spaceID, 1, replicaNowMS+100, nil, "theme", KindDelete, "", 1)

	for _, order := range [][]string{{set, del}, {del, set}} {
		r := newTestReplica(t, spaceID, 1)
		for _, token := range order {
			if _, err := r.Apply(token); err != nil {
				t.Fatal(err)
			}
		}
		record, ok := r.Get("preferences", "theme")
		if !ok || !record.Deleted {
			t.Fatalf("concurrent delete did not win: %+v", record)
		}
	}

	afterDelete := signTestOp(t, setter, spaceID, 2, replicaNowMS+50, VersionVector{
		setter.PeerID(): 1, remover.PeerID(): 1,
	}, "theme", KindSet, "light", 1)
	r := newTestReplica(t, spaceID, 1)
	for _, token := range []string{del, set, afterDelete} {
		if _, err := r.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	record, _ := r.Get("preferences", "theme")
	if record.Deleted || string(record.Payload) != "light" {
		t.Fatalf("causal set did not resurrect deleted record: %+v", record)
	}
}

func TestReplicaStoresUnknownSchemaWithoutMaterializing(t *testing.T) {
	spaceID := uuid.NewString()
	id := testIdentity(t)
	r := newTestReplica(t, spaceID, 1)
	token := signTestOp(t, id, spaceID, 1, replicaNowMS, nil, "future", KindSet, "opaque", 2)
	result, err := r.Apply(token)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stored || result.Materialized {
		t.Fatalf("unknown schema result = %+v", result)
	}
	if _, ok := r.Get("preferences", "future"); ok {
		t.Fatal("unknown schema operation was materialized")
	}
	if got := r.MissingTokens(nil); !reflect.DeepEqual(got, []string{token}) {
		t.Fatalf("stored tokens = %#v", got)
	}
}

func TestReplicaRejectsFutureHLC(t *testing.T) {
	spaceID := uuid.NewString()
	id := testIdentity(t)
	r, err := NewReplica(spaceID, 1, NewClockWithSource(func() time.Time {
		return time.UnixMilli(replicaNowMS)
	}, time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	token := signTestOp(t, id, spaceID, 1, replicaNowMS+time.Minute.Milliseconds()+1, nil, "theme", KindSet, "dark", 1)
	if _, err := r.Apply(token); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("future HLC error = %v", err)
	}
	if len(r.MissingTokens(nil)) != 0 {
		t.Fatal("rejected future operation was stored")
	}
}

func TestReplicaConvergesUnderShuffledDuplicateDelivery(t *testing.T) {
	spaceID := uuid.NewString()
	ids := []*peercrypto.Identity{testIdentity(t), testIdentity(t), testIdentity(t)}
	var tokens []string
	for actorIndex, id := range ids {
		causal := make(VersionVector)
		for counter := uint64(1); counter <= 4; counter++ {
			kind := KindSet
			payload := string(rune('a' + actorIndex))
			if counter == 4 && actorIndex == 1 {
				kind, payload = KindDelete, ""
			}
			tokens = append(tokens, signTestOp(t, id, spaceID, counter, replicaNowMS+int64(actorIndex*10)+int64(counter), causal, "shared", kind, payload, 1))
			causal = VersionVector{id.PeerID(): counter}
		}
	}

	baseline := newTestReplica(t, spaceID, 1)
	for _, token := range tokens {
		if _, err := baseline.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
	wantRecord, _ := baseline.Get("preferences", "shared")
	wantVector := baseline.Vector()

	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		delivery := append(append([]string(nil), tokens...), tokens[rng.Intn(len(tokens))])
		rng.Shuffle(len(delivery), func(i, j int) { delivery[i], delivery[j] = delivery[j], delivery[i] })
		replica := newTestReplica(t, spaceID, 1)
		for _, token := range delivery {
			if _, err := replica.Apply(token); err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}
		}
		gotRecord, _ := replica.Get("preferences", "shared")
		if !reflect.DeepEqual(gotRecord, wantRecord) || !reflect.DeepEqual(replica.Vector(), wantVector) {
			t.Fatalf("seed %d did not converge: record=%+v vector=%#v", seed, gotRecord, replica.Vector())
		}
	}
}

func TestReplicaThreeDevicePropagation(t *testing.T) {
	spaceID := uuid.NewString()
	ids := []*peercrypto.Identity{testIdentity(t), testIdentity(t), testIdentity(t)}
	replicas := []*Replica{newTestReplica(t, spaceID, 1), newTestReplica(t, spaceID, 1), newTestReplica(t, spaceID, 1)}

	if _, err := replicas[0].Append(ids[0], testConfigMutation("locale", KindSet, "en")); err != nil {
		t.Fatal(err)
	}
	syncReplicas(t, replicas[0], replicas[1])
	if _, err := replicas[1].Append(ids[1], testConfigMutation("locale", KindSet, "zh-CN")); err != nil {
		t.Fatal(err)
	}
	syncReplicas(t, replicas[1], replicas[2])
	if _, err := replicas[2].Append(ids[2], testConfigMutation("theme", KindSet, "dark")); err != nil {
		t.Fatal(err)
	}
	syncReplicas(t, replicas[2], replicas[0])
	syncReplicas(t, replicas[0], replicas[1])

	for i, replica := range replicas {
		locale, _ := replica.Get("preferences", "locale")
		theme, _ := replica.Get("preferences", "theme")
		if string(locale.Payload) != "zh-CN" || string(theme.Payload) != "dark" {
			t.Fatalf("replica %d view: locale=%+v theme=%+v", i, locale, theme)
		}
		if replica.Vector().Compare(replicas[0].Vector()) != VectorEqual {
			t.Fatalf("replica %d vector=%#v want=%#v", i, replica.Vector(), replicas[0].Vector())
		}
	}
}

func syncReplicas(t *testing.T, source, destination *Replica) {
	t.Helper()
	for _, token := range source.MissingTokens(destination.Vector()) {
		if _, err := destination.Apply(token); err != nil {
			t.Fatal(err)
		}
	}
}
