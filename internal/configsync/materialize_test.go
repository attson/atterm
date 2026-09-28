package configsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

type fakeSealedRelayCodec struct {
	decoded map[string][]PlainRecord
	encoded map[string]json.RawMessage
}

func (f *fakeSealedRelayCodec) DecodeSealedRelayValue(key string, _ json.RawMessage) ([]PlainRecord, error) {
	return clonePlainRecords(f.decoded[key]), nil
}

func (f *fakeSealedRelayCodec) EncodeSealedRelayValue(key string, records []PlainRecord) (json.RawMessage, error) {
	if f.encoded == nil {
		return nil, errors.New("missing encoded fixture")
	}
	return append(json.RawMessage(nil), f.encoded[key]...), nil
}

func TestPlanRelayRecordImportDeletesOnlyPreviouslyKnownRelayRecords(t *testing.T) {
	previous := []RecordRef{
		{Collection: CollectionQuickTemplate, RecordID: "kept", KeyClass: KeyClassSync},
		{Collection: CollectionQuickTemplate, RecordID: "removed", KeyClass: KeyClassSync},
	}
	value := RelayValue{Key: "quick_templates", Value: json.RawMessage(`[
		{"id":"new","label":"New","text":"new"},
		{"id":"kept","label":"Kept","text":"updated"}
	]`), UpdatedAt: 100}
	mutations, refs, err := PlanRelayRecordImport(value, previous, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := mutationKindsByID(mutations); !reflect.DeepEqual(got, map[string]Kind{
		"kept": KindSet, "new": KindSet, "removed": KindDelete,
	}) {
		t.Fatalf("mutations=%+v", mutations)
	}
	if len(refs) != 2 || refs[0].RecordID != "kept" || refs[1].RecordID != "new" {
		t.Fatalf("refs=%+v", refs)
	}
	for _, mutation := range mutations {
		if mutation.RecordID == "peer-only" {
			t.Fatal("planner invented a delete for a record absent from previous Relay state")
		}
	}
}

func TestRelayQuickTemplatesRoundTripIndependentRecords(t *testing.T) {
	value := RelayValue{Key: "quick_templates", Value: json.RawMessage(`[
		{"id":"b","label":"B","text":"two"},
		{"id":"a","label":"A","text":"one","hotkey":"Alt+1"}
	]`), UpdatedAt: 100}
	records, err := DecodeRelayRecords(value, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].RecordID != "b" || records[0].Position != "0000000000000000" || records[1].RecordID != "a" || records[1].Position != "0000000000000001" {
		t.Fatalf("records=%+v", records)
	}
	records[0], records[1] = records[1], records[0]
	raw, refs, err := EncodeRelayRecords("quick_templates", records, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalized, _ := NormalizeRelayValue(value.Key, value.Value)
	if string(raw) != string(normalized) {
		t.Fatalf("raw=%s normalized=%s", raw, normalized)
	}
	if len(refs) != 2 || refs[0].RecordID != "a" || refs[1].RecordID != "b" {
		t.Fatalf("refs=%+v", refs)
	}
}

func TestSealedRelayCodecMustSeparatePortableAndVaultRecords(t *testing.T) {
	codec := &fakeSealedRelayCodec{decoded: map[string][]PlainRecord{
		"profiles_encrypted": {
			{Collection: CollectionProfiles, RecordID: "profile-1", KeyClass: KeyClassSync, Position: legacyPosition(0), Value: json.RawMessage(`{"id":"profile-1","name":"Work"}`)},
			{Collection: CollectionProfileEnv, RecordID: "profile-1", KeyClass: KeyClassVault, Value: json.RawMessage(`{"env":{"TOKEN":"secret"},"id":"profile-1"}`)},
		},
	}}
	records, err := DecodeRelayRecords(RelayValue{Key: "profiles_encrypted", Value: json.RawMessage(`"YWJj"`)}, codec)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].KeyClass != KeyClassSync || records[1].KeyClass != KeyClassVault {
		t.Fatalf("records=%+v", records)
	}

	codec.decoded["profiles_encrypted"][1].KeyClass = KeyClassSync
	if _, err := DecodeRelayRecords(RelayValue{Key: "profiles_encrypted", Value: json.RawMessage(`"YWJj"`)}, codec); !errors.Is(err, ErrInvalidSchemaValue) {
		t.Fatalf("wrong vault class error=%v", err)
	}
}

func TestOpenPlainRecordAuthenticatesThenValidatesSchema(t *testing.T) {
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	clock := NewClockWithSource(func() time.Time { return time.UnixMilli(1000) }, time.Hour)
	replica, err := NewReplica(uuid.NewString(), SchemaVersion, clock)
	if err != nil {
		t.Fatal(err)
	}
	plain := PlainRecord{
		Collection: CollectionQuickTemplate, RecordID: "template-1", KeyClass: KeyClassSync,
		Position: legacyPosition(0), Value: json.RawMessage(`{"id":"template-1","label":"Run","text":"go test"}`),
	}
	payload, err := marshalCanonicalRecordPayload(plain)
	if err != nil {
		t.Fatal(err)
	}
	_, err = replica.AppendEncrypted(identity, key, Mutation{
		Collection: plain.Collection, RecordID: plain.RecordID, Kind: KindSet,
		KeyClass: plain.KeyClass, KeyEpoch: key.Epoch, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := replica.Get(plain.Collection, plain.RecordID)
	if !ok {
		t.Fatal("materialized record missing")
	}
	opened, err := OpenPlainRecord(record, key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened, plain) {
		t.Fatalf("opened=%+v plain=%+v", opened, plain)
	}
}

func TestOpenPlainRecordRestoresEveryOrderedCollection(t *testing.T) {
	for _, collection := range []string{CollectionQuickTemplate, CollectionProfiles, CollectionSSHHosts, CollectionSSHKeys} {
		t.Run(collection, func(t *testing.T) {
			identity := testIdentity(t)
			key, err := GenerateEpochKey(KeyClassSync, 1)
			if err != nil {
				t.Fatal(err)
			}
			recordID := "record-1"
			value := json.RawMessage(`{"id":"record-1","name":"example"}`)
			if collection == CollectionQuickTemplate {
				value = json.RawMessage(`{"id":"record-1","label":"Example","text":"echo ok"}`)
			}
			mutation, err := SetRecordMutation(PlainRecord{
				Collection: collection, RecordID: recordID, KeyClass: KeyClassSync,
				Position: PositionForIndex(4), Value: value,
			})
			if err != nil {
				t.Fatal(err)
			}
			replica := newTestReplica(t, uuid.NewString(), SchemaVersion)
			if _, err := replica.AppendEncrypted(identity, key, Mutation{
				SchemaVersion: SchemaVersion, Collection: mutation.Collection,
				RecordID: mutation.RecordID, Kind: mutation.Kind, Payload: mutation.Payload,
			}); err != nil {
				t.Fatal(err)
			}
			record, ok := replica.Get(collection, recordID)
			if !ok {
				t.Fatal("ordered record missing")
			}
			opened, err := OpenPlainRecord(record, key)
			if err != nil {
				t.Fatal(err)
			}
			if opened.Position != PositionForIndex(4) || !bytes.Equal(opened.Value, value) {
				t.Fatalf("opened record=%+v", opened)
			}
		})
	}
}

func TestMaterializeRelayValueUsesWinnersTombstonesAndEpochs(t *testing.T) {
	identity := testIdentity(t)
	key, err := GenerateEpochKey(KeyClassSync, 1)
	if err != nil {
		t.Fatal(err)
	}
	replica := newTestReplica(t, uuid.NewString(), SchemaVersion)
	appendTemplate := func(id, label string) {
		t.Helper()
		mutation, err := SetRecordMutation(PlainRecord{
			Collection: CollectionQuickTemplate, RecordID: id, KeyClass: KeyClassSync,
			Position: PositionForIndex(uint64(len(replica.Records(CollectionQuickTemplate)))),
			Value:    json.RawMessage(`{"id":"` + id + `","label":"` + label + `","text":"echo ok"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := replica.AppendEncrypted(identity, key, Mutation{
			Collection: mutation.Collection, RecordID: mutation.RecordID, Kind: mutation.Kind, Payload: mutation.Payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendTemplate("a", "A")
	appendTemplate("b", "B")
	if _, err := replica.AppendEncrypted(identity, key, Mutation{
		Collection: CollectionQuickTemplate, RecordID: "a", Kind: KindDelete,
	}); err != nil {
		t.Fatal(err)
	}
	records := replica.Records(CollectionQuickTemplate)
	latest := records[0].HLC.PhysicalMS
	if records[1].HLC.PhysicalMS > latest {
		latest = records[1].HLC.PhysicalMS
	}
	resolve := func(class KeyClass, epoch uint64) (EpochKey, bool) {
		return key, class == key.Class && epoch == key.Epoch
	}
	value, ok, err := MaterializeRelayValue("quick_templates", records, resolve, nil)
	if err != nil || !ok {
		t.Fatalf("materialize ok=%t err=%v", ok, err)
	}
	if string(value.Value) != `[{"id":"b","label":"B","text":"echo ok"}]` || value.UpdatedAt != latest || len(value.Records) != 1 || value.Records[0].RecordID != "b" {
		t.Fatalf("materialized value=%+v", value)
	}
	if _, _, err := MaterializeRelayValue("quick_templates", records, func(KeyClass, uint64) (EpochKey, bool) {
		return EpochKey{}, false
	}, nil); !errors.Is(err, ErrInvalidEpochKey) {
		t.Fatalf("missing epoch error=%v", err)
	}

	scalarMutation, err := SetRecordMutation(PlainRecord{
		Collection: CollectionPreferences, RecordID: "terminal_theme", KeyClass: KeyClassSync, Value: json.RawMessage(`"nord"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replica.AppendEncrypted(identity, key, Mutation{
		Collection: scalarMutation.Collection, RecordID: scalarMutation.RecordID, Kind: scalarMutation.Kind, Payload: scalarMutation.Payload,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := replica.AppendEncrypted(identity, key, Mutation{
		Collection: CollectionPreferences, RecordID: "terminal_theme", Kind: KindDelete,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := MaterializeRelayValue("terminal_theme", replica.Records(CollectionPreferences), resolve, nil); err != nil || ok {
		t.Fatalf("deleted scalar materialized ok=%t err=%v", ok, err)
	}
}

func TestPlanRecordReplacementEmitsOnlyChangedAndDeletedRecords(t *testing.T) {
	previous := []PlainRecord{
		{Collection: CollectionProfiles, RecordID: "same", KeyClass: KeyClassSync, Position: PositionForIndex(0), Value: json.RawMessage(`{"id":"same","name":"Same"}`)},
		{Collection: CollectionProfiles, RecordID: "changed", KeyClass: KeyClassSync, Position: PositionForIndex(1), Value: json.RawMessage(`{"id":"changed","name":"Before"}`)},
		{Collection: CollectionProfileEnv, RecordID: "removed", KeyClass: KeyClassVault, Value: json.RawMessage(`{"env":{"TOKEN":"old"},"id":"removed"}`)},
	}
	current := []PlainRecord{
		previous[0],
		{Collection: CollectionProfiles, RecordID: "changed", KeyClass: KeyClassSync, Position: PositionForIndex(1), Value: json.RawMessage(`{"id":"changed","name":"After"}`)},
		{Collection: CollectionProfiles, RecordID: "added", KeyClass: KeyClassSync, Position: PositionForIndex(2), Value: json.RawMessage(`{"id":"added","name":"Added"}`)},
	}
	mutations, err := PlanRecordReplacement(current, previous)
	if err != nil {
		t.Fatal(err)
	}
	if got := mutationKindsByID(mutations); !reflect.DeepEqual(got, map[string]Kind{
		"added": KindSet, "changed": KindSet, "removed": KindDelete,
	}) {
		t.Fatalf("mutations=%+v", mutations)
	}
	for _, mutation := range mutations {
		if mutation.RecordID == "same" {
			t.Fatal("unchanged record produced a mutation")
		}
	}
	if _, err := PlanRecordReplacement(append(current, current[0]), previous); !errors.Is(err, ErrInvalidSchemaValue) {
		t.Fatalf("duplicate current error=%v", err)
	}
}

func TestReplicaRecordsReturnsStableDetachedCollection(t *testing.T) {
	id, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replica := newTestReplica(t, uuid.NewString(), SchemaVersion)
	for _, recordID := range []string{"z", "a"} {
		if _, err := replica.Append(id, Mutation{
			Collection: CollectionQuickTemplate, RecordID: recordID, Kind: KindSet,
			KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte(recordID),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := replica.Append(id, Mutation{
		Collection: CollectionPreferences, RecordID: "locale_preference", Kind: KindSet,
		KeyClass: KeyClassSync, KeyEpoch: 1, Payload: []byte("zh-CN"),
	}); err != nil {
		t.Fatal(err)
	}
	records := replica.Records(CollectionQuickTemplate)
	if len(records) != 2 || records[0].RecordID != "a" || records[1].RecordID != "z" {
		t.Fatalf("records=%+v", records)
	}
	records[0].Payload[0] = 'X'
	again, _ := replica.Get(CollectionQuickTemplate, "a")
	if string(again.Payload) != "a" {
		t.Fatalf("Records payload aliases replica: %q", again.Payload)
	}
}

func mutationKindsByID(mutations []RecordMutation) map[string]Kind {
	out := make(map[string]Kind, len(mutations))
	for _, mutation := range mutations {
		out[mutation.RecordID] = mutation.Kind
	}
	return out
}
