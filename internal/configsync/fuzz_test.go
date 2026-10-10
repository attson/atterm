package configsync

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

func FuzzVerifySyncDocuments(f *testing.F) {
	now := time.Unix(1_800_000_000, 0)
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		f.Fatal(err)
	}
	spaceID := uuid.NewString()
	clock := NewClockWithSource(func() time.Time { return now }, DefaultMaxFutureSkew)
	replica, err := NewReplica(spaceID, 1, clock)
	if err != nil {
		f.Fatal(err)
	}
	op, err := replica.Append(identity, Mutation{
		Collection: "preferences", RecordID: "terminal_theme", Kind: KindSet,
		KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte("nord"),
	})
	if err != nil {
		f.Fatal(err)
	}
	snapshot, err := replica.SignSnapshot(identity)
	if err != nil {
		f.Fatal(err)
	}
	compatibility, err := json.Marshal(RelayCompatibilityState{
		Version: relayCompatibilityStateVersion,
		RealmID: "fuzz-realm",
		Keys:    map[string]RelayKeyState{},
	})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(uint8(0), []byte(op.Token))
	f.Add(uint8(1), []byte(snapshot))
	f.Add(uint8(2), compatibility)
	f.Add(uint8(0), []byte{})
	f.Add(uint8(2), []byte(`{"version":1,"realm_id":"x","keys":{},"extra":true}`))

	f.Fuzz(func(t *testing.T, parser uint8, data []byte) {
		switch parser % 3 {
		case 0:
			_, _ = VerifyOp(string(data))
		case 1:
			_, _ = VerifySnapshot(string(data))
		case 2:
			_, _ = ParseRelayCompatibilityState(data)
		}
	})
}
