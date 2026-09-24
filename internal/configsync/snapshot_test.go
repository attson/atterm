package configsync

import (
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSignedSnapshotRoundTripAndPostSnapshotMutation(t *testing.T) {
	spaceID := uuid.NewString()
	creator := testIdentity(t)
	other := testIdentity(t)
	source := newTestReplica(t, spaceID, 1)
	first, err := source.Append(creator, Mutation{Collection: "preferences", RecordID: "theme", Kind: KindSet, Payload: []byte("dark")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Append(creator, Mutation{Collection: "preferences", RecordID: "locale", Kind: KindSet, Payload: []byte("en")}); err != nil {
		t.Fatal(err)
	}
	deleteLocale := signTestOp(t, other, spaceID, 1, replicaNowMS+100, source.Vector(), "locale", KindDelete, "", 1)
	if _, err := source.Apply(deleteLocale); err != nil {
		t.Fatal(err)
	}

	snapshot, err := source.SignSnapshot(creator)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifySnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Document.CreatorDeviceID != creator.PeerID() || len(verified.RecordOps) != 2 {
		t.Fatalf("unexpected snapshot: %+v", verified.Document)
	}

	restored := newTestReplica(t, spaceID, 1)
	if err := restored.InstallSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Vector(), source.Vector()) {
		t.Fatalf("restored vector=%#v want=%#v", restored.Vector(), source.Vector())
	}
	theme, _ := restored.Get("preferences", "theme")
	locale, _ := restored.Get("preferences", "locale")
	if string(theme.Payload) != "dark" || !locale.Deleted {
		t.Fatalf("restored records: theme=%+v locale=%+v", theme, locale)
	}

	if result, err := restored.Apply(first.Token); err != nil || !result.Duplicate || !result.Compacted {
		t.Fatalf("covered operation result=%+v err=%v", result, err)
	}
	fork := signTestOp(t, creator, spaceID, 1, replicaNowMS, nil, "different", KindSet, "value", 1)
	if _, err := restored.Apply(fork); !errors.Is(err, ErrCounterFork) {
		t.Fatalf("compacted winner fork error=%v", err)
	}
	created, err := restored.Append(creator, Mutation{Collection: "preferences", RecordID: "theme", Kind: KindSet, Payload: []byte("light")})
	if err != nil {
		t.Fatal(err)
	}
	if created.Document.Counter != 3 {
		t.Fatalf("post-snapshot counter=%d want=3", created.Document.Counter)
	}
}

func TestSnapshotRetainsUnknownSchemas(t *testing.T) {
	spaceID := uuid.NewString()
	creator := testIdentity(t)
	futureActor := testIdentity(t)
	replica := newTestReplica(t, spaceID, 1)
	future := signTestOp(t, futureActor, spaceID, 1, replicaNowMS, nil, "future", KindSet, "opaque", 2)
	if _, err := replica.Apply(future); err != nil {
		t.Fatal(err)
	}
	snapshot, err := replica.SignSnapshot(creator)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifySnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.RecordOps) != 0 || len(verified.RetainedOps) != 1 || verified.RetainedOps[0].Token != future {
		t.Fatalf("unexpected retained operations: %+v", verified)
	}
	restored := newTestReplica(t, spaceID, 1)
	if err := restored.InstallSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	nextSnapshot, err := restored.SignSnapshot(creator)
	if err != nil {
		t.Fatal(err)
	}
	next, err := VerifySnapshot(nextSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.RetainedOps) != 1 || next.RetainedOps[0].Token != future {
		t.Fatal("unknown-schema operation was lost during repeated compaction")
	}
}

func TestSnapshotRejectsIncompleteHistoryAndMutation(t *testing.T) {
	spaceID := uuid.NewString()
	creator := testIdentity(t)
	replica := newTestReplica(t, spaceID, 1)
	op2 := signTestOp(t, creator, spaceID, 2, replicaNowMS, VersionVector{creator.PeerID(): 1}, "locale", KindSet, "zh-CN", 1)
	if _, err := replica.Apply(op2); err != nil {
		t.Fatal(err)
	}
	if _, err := replica.SignSnapshot(creator); !errors.Is(err, ErrIncompleteHistory) {
		t.Fatalf("incomplete history snapshot error=%v", err)
	}

	complete := newTestReplica(t, spaceID, 1)
	if _, err := complete.Append(creator, Mutation{Collection: "preferences", RecordID: "locale", Kind: KindSet, Payload: []byte("en")}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := complete.SignSnapshot(creator)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(snapshot, ".")
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 1
	parts[1] = encode(raw)
	if _, err := VerifySnapshot(strings.Join(parts, ".")); err == nil {
		t.Fatal("mutated snapshot verified")
	}
}
