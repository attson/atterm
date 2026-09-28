package configsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// PlainRecord is a decrypted, schema-validated canonical entity. Position is
// used only by ordered collections; it is carried inside the encrypted record
// payload so all replicas deterministically materialize the same legacy array.
type PlainRecord struct {
	Collection string
	RecordID   string
	KeyClass   KeyClass
	Position   string
	Value      json.RawMessage
}

// RecordRef identifies an entity previously represented by one Relay value.
// Compatibility state retains these refs so a later whole-array snapshot may
// delete only records that Relay actually knew about, never Peer-only records
// that had not reached the legacy service yet.
type RecordRef struct {
	Collection string   `json:"collection"`
	RecordID   string   `json:"record_id"`
	KeyClass   KeyClass `json:"key_class"`
}

// RecordMutation is a plaintext mutation plan. The owner of the epoch keys
// must pass it through DurableReplica.AppendEncrypted before acknowledging an
// import or applying its materialized value to app config.
type RecordMutation struct {
	Collection string
	RecordID   string
	Kind       Kind
	KeyClass   KeyClass
	Payload    json.RawMessage
}

// SealedRelayCodec is implemented at the platform boundary where the legacy
// account_key and concrete profile/SSH types already live. configsync never
// receives that account key; it only receives validated plaintext records.
type SealedRelayCodec interface {
	DecodeSealedRelayValue(key string, normalized json.RawMessage) ([]PlainRecord, error)
	EncodeSealedRelayValue(key string, records []PlainRecord) (json.RawMessage, error)
}

type orderedRecordPayload struct {
	Position string          `json:"position"`
	Value    json.RawMessage `json:"value"`
}

// DecodeRelayRecords splits one legacy value into canonical records. Scalars
// become one preferences record, ordered arrays become one record per stable
// entity id, and sealed bundles delegate only their account-key opening step.
func DecodeRelayRecords(value RelayValue, codec SealedRelayCodec) ([]PlainRecord, error) {
	spec, ok := RelaySpec(value.Key)
	if !ok {
		return nil, fmt.Errorf("%w: unknown relay key %q", ErrInvalidSchemaValue, value.Key)
	}
	normalized, err := NormalizeRelayValue(value.Key, value.Value)
	if err != nil {
		return nil, err
	}
	var records []PlainRecord
	switch spec.Mode {
	case RelayScalar:
		records = []PlainRecord{{
			Collection: CollectionPreferences,
			RecordID:   value.Key,
			KeyClass:   KeyClassSync,
			Value:      normalized,
		}}
	case RelayRecordArray:
		ordered, err := SplitRelayRecordArray(value.Key, normalized)
		if err != nil {
			return nil, err
		}
		records = make([]PlainRecord, len(ordered))
		for i, record := range ordered {
			records[i] = PlainRecord{
				Collection: record.Collection,
				RecordID:   record.RecordID,
				KeyClass:   KeyClassSync,
				Position:   legacyPosition(record.Position),
				Value:      record.Payload,
			}
		}
	case RelaySealedBundle:
		if codec == nil {
			return nil, fmt.Errorf("%w: no sealed codec for %q", ErrInvalidSchemaValue, value.Key)
		}
		records, err = codec.DecodeSealedRelayValue(value.Key, normalized)
		if err != nil {
			return nil, fmt.Errorf("decode sealed relay key %s: %w", value.Key, err)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported relay mode for %q", ErrInvalidSchemaValue, value.Key)
	}
	if err := validateRecordSetForRelayKey(value.Key, records); err != nil {
		return nil, err
	}
	return clonePlainRecords(records), nil
}

// PlanRelayRecordImport compares a full Relay value with the records that the
// previous value for this key represented. It emits remove-wins tombstones for
// disappeared legacy records, but intentionally does not inspect or delete
// unrelated records currently present in the canonical replica.
func PlanRelayRecordImport(value RelayValue, previous []RecordRef, codec SealedRelayCodec) ([]RecordMutation, []RecordRef, error) {
	records, err := DecodeRelayRecords(value, codec)
	if err != nil {
		return nil, nil, err
	}
	incoming := make(map[string]PlainRecord, len(records))
	mutations := make([]RecordMutation, 0, len(records)+len(previous))
	refs := make([]RecordRef, 0, len(records))
	for _, record := range records {
		key := canonicalRecordKey(record.Collection, record.RecordID)
		incoming[key] = record
		mutation, err := SetRecordMutation(record)
		if err != nil {
			return nil, nil, err
		}
		mutations = append(mutations, mutation)
		refs = append(refs, RecordRef{Collection: record.Collection, RecordID: record.RecordID, KeyClass: record.KeyClass})
	}
	seenPrevious := make(map[string]struct{}, len(previous))
	for _, ref := range previous {
		if err := validateRecordRefForRelayKey(value.Key, ref); err != nil {
			return nil, nil, err
		}
		key := canonicalRecordKey(ref.Collection, ref.RecordID)
		if _, duplicate := seenPrevious[key]; duplicate {
			return nil, nil, fmt.Errorf("%w: duplicate previous record %q", ErrInvalidSchemaValue, key)
		}
		seenPrevious[key] = struct{}{}
		if _, stillPresent := incoming[key]; stillPresent {
			continue
		}
		mutations = append(mutations, RecordMutation{
			Collection: ref.Collection, RecordID: ref.RecordID,
			Kind: KindDelete, KeyClass: ref.KeyClass,
		})
	}
	sortRecordMutations(mutations)
	sortRecordRefs(refs)
	return mutations, refs, nil
}

// SetRecordMutation validates one canonical plaintext record and wraps ordered
// collection metadata into the payload encrypted by the durable replica.
func SetRecordMutation(record PlainRecord) (RecordMutation, error) {
	payload, err := marshalCanonicalRecordPayload(record)
	if err != nil {
		return RecordMutation{}, err
	}
	return RecordMutation{
		Collection: record.Collection,
		RecordID:   record.RecordID,
		Kind:       KindSet,
		KeyClass:   record.KeyClass,
		Payload:    payload,
	}, nil
}

// EncodeRelayRecords materializes current canonical winners into one legacy
// Relay value. Deleted records must be omitted by the caller.
func EncodeRelayRecords(key string, records []PlainRecord, codec SealedRelayCodec) (json.RawMessage, []RecordRef, error) {
	spec, ok := RelaySpec(key)
	if !ok {
		return nil, nil, fmt.Errorf("%w: unknown relay key %q", ErrInvalidSchemaValue, key)
	}
	if err := validateRecordSetForRelayKey(key, records); err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	var err error
	switch spec.Mode {
	case RelayScalar:
		if len(records) != 1 {
			return nil, nil, fmt.Errorf("%w: scalar %q has %d records", ErrInvalidSchemaValue, key, len(records))
		}
		raw = append(json.RawMessage(nil), records[0].Value...)
	case RelayRecordArray:
		ordered := make([]OrderedRecord, len(records))
		for i, record := range records {
			position, err := parseLegacyPosition(record.Position)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: record %q position: %v", ErrInvalidSchemaValue, record.RecordID, err)
			}
			ordered[i] = OrderedRecord{
				Collection: record.Collection, RecordID: record.RecordID,
				Position: position, Payload: append(json.RawMessage(nil), record.Value...),
			}
		}
		raw, err = JoinRelayRecordArray(key, ordered)
	case RelaySealedBundle:
		if codec == nil {
			return nil, nil, fmt.Errorf("%w: no sealed codec for %q", ErrInvalidSchemaValue, key)
		}
		raw, err = codec.EncodeSealedRelayValue(key, clonePlainRecords(records))
	default:
		err = fmt.Errorf("unsupported relay mode")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("encode relay key %s: %w", key, err)
	}
	normalized, err := NormalizeRelayValue(key, raw)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]RecordRef, len(records))
	for i, record := range records {
		refs[i] = RecordRef{Collection: record.Collection, RecordID: record.RecordID, KeyClass: record.KeyClass}
	}
	sortRecordRefs(refs)
	return normalized, refs, nil
}

// OpenPlainRecord validates and unwraps a decrypted replica record.
func OpenPlainRecord(record Record, key EpochKey) (PlainRecord, error) {
	if record.Deleted {
		return PlainRecord{}, fmt.Errorf("%w: cannot open tombstone", ErrInvalidSchemaValue)
	}
	plaintext, err := OpenRecordPayload(key, record)
	if err != nil {
		return PlainRecord{}, err
	}
	plain, err := unmarshalCanonicalRecordPayload(record.Collection, record.RecordID, record.KeyClass, plaintext)
	if err != nil {
		return PlainRecord{}, err
	}
	return plain, nil
}

func marshalCanonicalRecordPayload(record PlainRecord) (json.RawMessage, error) {
	if err := validatePlainRecord(record); err != nil {
		return nil, err
	}
	if record.Position == "" {
		return append(json.RawMessage(nil), record.Value...), nil
	}
	return json.Marshal(orderedRecordPayload{Position: record.Position, Value: record.Value})
}

func unmarshalCanonicalRecordPayload(collection, recordID string, class KeyClass, raw json.RawMessage) (PlainRecord, error) {
	record := PlainRecord{Collection: collection, RecordID: recordID, KeyClass: class}
	if collection == CollectionQuickTemplate || collection == CollectionProfiles || collection == CollectionSSHHosts || collection == CollectionSSHKeys {
		var ordered orderedRecordPayload
		if err := decodeStrictJSON(raw, &ordered); err != nil {
			return PlainRecord{}, fmt.Errorf("%w: ordered record %q: %v", ErrInvalidSchemaValue, recordID, err)
		}
		record.Position = ordered.Position
		record.Value = append(json.RawMessage(nil), ordered.Value...)
	} else {
		record.Value = append(json.RawMessage(nil), raw...)
	}
	if err := validatePlainRecord(record); err != nil {
		return PlainRecord{}, err
	}
	return record, nil
}

func validateRecordSetForRelayKey(key string, records []PlainRecord) error {
	spec, ok := RelaySpec(key)
	if !ok {
		return fmt.Errorf("%w: unknown relay key %q", ErrInvalidSchemaValue, key)
	}
	allowedCollections := make(map[string]struct{}, len(spec.Collections))
	allowedClasses := make(map[KeyClass]struct{}, len(spec.KeyClasses))
	for _, collection := range spec.Collections {
		allowedCollections[collection] = struct{}{}
	}
	for _, class := range spec.KeyClasses {
		allowedClasses[class] = struct{}{}
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, ok := allowedCollections[record.Collection]; !ok {
			return fmt.Errorf("%w: relay key %q cannot contain collection %q", ErrInvalidSchemaValue, key, record.Collection)
		}
		if _, ok := allowedClasses[record.KeyClass]; !ok {
			return fmt.Errorf("%w: relay key %q cannot contain key class %q", ErrInvalidSchemaValue, key, record.KeyClass)
		}
		if spec.Mode == RelayScalar && record.RecordID != key {
			return fmt.Errorf("%w: scalar %q record id %q", ErrInvalidSchemaValue, key, record.RecordID)
		}
		if err := validatePlainRecord(record); err != nil {
			return err
		}
		canonical := canonicalRecordKey(record.Collection, record.RecordID)
		if _, duplicate := seen[canonical]; duplicate {
			return fmt.Errorf("%w: duplicate record %q", ErrInvalidSchemaValue, canonical)
		}
		seen[canonical] = struct{}{}
	}
	if spec.Mode == RelayScalar && len(records) != 1 {
		return fmt.Errorf("%w: scalar %q has %d records", ErrInvalidSchemaValue, key, len(records))
	}
	return nil
}

func validateRecordRefForRelayKey(key string, ref RecordRef) error {
	spec, ok := RelaySpec(key)
	if !ok || !validName(ref.Collection, 64) || !validRecordID(ref.RecordID) || !ref.KeyClass.valid() {
		return fmt.Errorf("%w: invalid previous record for %q", ErrInvalidSchemaValue, key)
	}
	collectionAllowed := false
	for _, collection := range spec.Collections {
		if collection == ref.Collection {
			collectionAllowed = true
			break
		}
	}
	classAllowed := false
	for _, class := range spec.KeyClasses {
		if class == ref.KeyClass {
			classAllowed = true
			break
		}
	}
	expectedClass, _, knownCollection := canonicalCollectionRule(ref.Collection)
	if !collectionAllowed || !classAllowed || !knownCollection || expectedClass != ref.KeyClass {
		return fmt.Errorf("%w: previous record %q is outside relay key %q", ErrInvalidSchemaValue, ref.RecordID, key)
	}
	if spec.Mode == RelayScalar && ref.RecordID != key {
		return fmt.Errorf("%w: scalar %q previous record id %q", ErrInvalidSchemaValue, key, ref.RecordID)
	}
	return nil
}

func validatePlainRecord(record PlainRecord) error {
	if !validName(record.Collection, 64) || !validRecordID(record.RecordID) || !record.KeyClass.valid() {
		return fmt.Errorf("%w: record identity", ErrInvalidSchemaValue)
	}
	expectedClass, ordered, ok := canonicalCollectionRule(record.Collection)
	if !ok || expectedClass != record.KeyClass {
		return fmt.Errorf("%w: collection %q and key class %q", ErrInvalidSchemaValue, record.Collection, record.KeyClass)
	}
	if ordered != (record.Position != "") {
		return fmt.Errorf("%w: collection %q position", ErrInvalidSchemaValue, record.Collection)
	}
	if ordered && !validPosition(record.Position) {
		return fmt.Errorf("%w: invalid position for %q", ErrInvalidSchemaValue, record.RecordID)
	}
	if len(record.Value) == 0 {
		return fmt.Errorf("%w: empty record value", ErrInvalidSchemaValue)
	}
	switch record.Collection {
	case CollectionPreferences:
		normalized, err := NormalizeRelayValue(record.RecordID, record.Value)
		if err != nil {
			return err
		}
		spec, _ := RelaySpec(record.RecordID)
		if spec.Mode != RelayScalar || !bytes.Equal(normalized, record.Value) {
			return fmt.Errorf("%w: non-canonical scalar %q", ErrInvalidSchemaValue, record.RecordID)
		}
	case CollectionQuickTemplate:
		var template relayTemplate
		if err := decodeStrictJSON(record.Value, &template); err != nil || validateTemplates([]relayTemplate{template}) != nil || template.ID != record.RecordID {
			return fmt.Errorf("%w: quick template %q", ErrInvalidSchemaValue, record.RecordID)
		}
	default:
		if err := validateEntityObject(record.Value, record.RecordID, record.Collection); err != nil {
			return err
		}
	}
	return nil
}

func validateEntityObject(raw json.RawMessage, recordID, collection string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return fmt.Errorf("%w: %s record %q is not an object", ErrInvalidSchemaValue, collection, recordID)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %s record %q trailing JSON", ErrInvalidSchemaValue, collection, recordID)
	}
	if id, _ := object["id"].(string); id != recordID {
		return fmt.Errorf("%w: %s payload id does not match %q", ErrInvalidSchemaValue, collection, recordID)
	}
	canonical, err := json.Marshal(object)
	if err != nil || !bytes.Equal(canonical, raw) {
		return fmt.Errorf("%w: %s record %q is not canonical", ErrInvalidSchemaValue, collection, recordID)
	}
	return nil
}

func canonicalCollectionRule(collection string) (KeyClass, bool, bool) {
	switch collection {
	case CollectionPreferences, CollectionProfileConfig:
		return KeyClassSync, false, true
	case CollectionQuickTemplate, CollectionProfiles, CollectionSSHHosts, CollectionSSHKeys:
		return KeyClassSync, true, true
	case CollectionProfileEnv, CollectionSSHCredential, CollectionSSHKeySecret:
		return KeyClassVault, false, true
	default:
		return "", false, false
	}
}

func legacyPosition(index uint64) string {
	return fmt.Sprintf("%016x", index)
}

func parseLegacyPosition(position string) (uint64, error) {
	if !validPosition(position) {
		return 0, errors.New("invalid position")
	}
	return strconv.ParseUint(position, 16, 64)
}

func validPosition(position string) bool {
	if len(position) != 16 {
		return false
	}
	_, err := strconv.ParseUint(position, 16, 64)
	return err == nil
}

func canonicalRecordKey(collection, recordID string) string {
	return collection + "\x00" + recordID
}

func clonePlainRecords(records []PlainRecord) []PlainRecord {
	out := make([]PlainRecord, len(records))
	copy(out, records)
	for i := range out {
		out[i].Value = append(json.RawMessage(nil), out[i].Value...)
	}
	return out
}

// CanonicalEntityJSON converts a platform-owned entity struct into the stable
// object encoding required by PlainRecord.Value and verifies its id field.
func CanonicalEntityJSON(recordID string, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal entity %q: %v", ErrInvalidSchemaValue, recordID, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: entity %q is not an object", ErrInvalidSchemaValue, recordID)
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal canonical entity %q: %v", ErrInvalidSchemaValue, recordID, err)
	}
	if id, _ := object["id"].(string); id != recordID {
		return nil, fmt.Errorf("%w: entity payload id does not match %q", ErrInvalidSchemaValue, recordID)
	}
	return canonical, nil
}

// PositionForIndex maps a legacy array rank to a stable sortable position.
func PositionForIndex(index uint64) string { return legacyPosition(index) }

// IndexFromPosition parses a position previously returned by PositionForIndex.
func IndexFromPosition(position string) (uint64, error) { return parseLegacyPosition(position) }

func sortRecordMutations(mutations []RecordMutation) {
	sort.Slice(mutations, func(i, j int) bool {
		if mutations[i].Collection != mutations[j].Collection {
			return mutations[i].Collection < mutations[j].Collection
		}
		if mutations[i].RecordID != mutations[j].RecordID {
			return mutations[i].RecordID < mutations[j].RecordID
		}
		return mutations[i].Kind < mutations[j].Kind
	})
}

func sortRecordRefs(refs []RecordRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Collection != refs[j].Collection {
			return refs[i].Collection < refs[j].Collection
		}
		return refs[i].RecordID < refs[j].RecordID
	})
}
