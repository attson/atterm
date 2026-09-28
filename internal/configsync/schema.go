package configsync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	SchemaVersion = 1

	CollectionPreferences   = "preferences"
	CollectionQuickTemplate = "quick_templates"
	CollectionProfiles      = "profiles"
	CollectionProfileConfig = "profile_config"
	CollectionProfileEnv    = "profile_env"
	CollectionSSHHosts      = "ssh_hosts"
	CollectionSSHKeys       = "ssh_keys"
	CollectionSSHCredential = "ssh_credentials"
	CollectionSSHKeySecret  = "ssh_key_secrets"
)

var ErrInvalidSchemaValue = errors.New("configsync: invalid schema value")

// RelayValueMode describes how one legacy Relay preference is represented in
// the canonical replica. Scalar values occupy one preferences record. Record
// arrays and sealed blobs need collection-aware codecs so entities can merge
// independently instead of preserving the legacy whole-value LWW behavior.
type RelayValueMode string

const (
	RelayScalar       RelayValueMode = "scalar"
	RelayRecordArray  RelayValueMode = "record_array"
	RelaySealedBundle RelayValueMode = "sealed_bundle"
)

// RelayKeySpec is the compatibility contract for one legacy preference key.
// Collections is explicit for sealed bundles because profiles and SSH data
// split portable metadata from capability-gated vault records.
type RelayKeySpec struct {
	Key         string
	Mode        RelayValueMode
	Collections []string
	KeyClasses  []KeyClass
	shape       relayJSONShape
}

type relayJSONShape string

const (
	shapeString       relayJSONShape = "string"
	shapeBool         relayJSONShape = "bool"
	shapeInt          relayJSONShape = "int"
	shapeNumber       relayJSONShape = "number"
	shapeStringArray  relayJSONShape = "string_array"
	shapeStringMap    relayJSONShape = "string_map"
	shapeTemplateList relayJSONShape = "template_list"
	shapeSealed       relayJSONShape = "sealed"
)

var relayKeySpecs = []RelayKeySpec{
	{Key: "ai_notifications_only", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "command_notify_threshold_seconds", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "default_shell", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "locale_preference", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "notifications_enabled", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "pinned_session_ids", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeStringArray},
	{Key: "profiles_encrypted", Mode: RelaySealedBundle, Collections: []string{CollectionProfiles, CollectionProfileConfig, CollectionProfileEnv}, KeyClasses: []KeyClass{KeyClassSync, KeyClassVault}, shape: shapeSealed},
	{Key: "quick_templates", Mode: RelayRecordArray, Collections: []string{CollectionQuickTemplate}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeTemplateList},
	{Key: "shell_integration_enabled", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "shortcut_bindings", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeStringMap},
	{Key: "ssh_hosts_encrypted", Mode: RelaySealedBundle, Collections: []string{CollectionSSHHosts, CollectionSSHKeys, CollectionSSHCredential, CollectionSSHKeySecret}, KeyClasses: []KeyClass{KeyClassSync, KeyClassVault}, shape: shapeSealed},
	{Key: "terminal_cursor_blink", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "terminal_cursor_style", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "terminal_font_head", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "terminal_font_size", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "terminal_line_height", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeNumber},
	{Key: "terminal_scrollback", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "terminal_theme", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
}

// RelayKeySpecs returns detached, key-sorted compatibility descriptors.
func RelayKeySpecs() []RelayKeySpec {
	out := make([]RelayKeySpec, len(relayKeySpecs))
	for i, spec := range relayKeySpecs {
		out[i] = spec
		out[i].Collections = append([]string(nil), spec.Collections...)
		out[i].KeyClasses = append([]KeyClass(nil), spec.KeyClasses...)
	}
	return out
}

// RelayKeys returns every legacy Relay key understood by the canonical
// schema. Keep this contract pinned to prefssync.SyncedKeys in tests.
func RelayKeys() []string {
	keys := make([]string, len(relayKeySpecs))
	for i := range relayKeySpecs {
		keys[i] = relayKeySpecs[i].Key
	}
	return keys
}

// RelaySpec returns a detached descriptor for key.
func RelaySpec(key string) (RelayKeySpec, bool) {
	i := sort.Search(len(relayKeySpecs), func(i int) bool { return relayKeySpecs[i].Key >= key })
	if i == len(relayKeySpecs) || relayKeySpecs[i].Key != key {
		return RelayKeySpec{}, false
	}
	spec := relayKeySpecs[i]
	spec.Collections = append([]string(nil), spec.Collections...)
	spec.KeyClasses = append([]KeyClass(nil), spec.KeyClasses...)
	return spec, true
}

// NormalizeRelayValue strictly validates one Relay value and returns stable
// JSON bytes. Hashing these bytes makes object-key order and whitespace
// irrelevant when detecting Relay echoes.
func NormalizeRelayValue(key string, raw json.RawMessage) (json.RawMessage, error) {
	spec, ok := RelaySpec(key)
	if !ok {
		return nil, fmt.Errorf("%w: unknown relay key %q", ErrInvalidSchemaValue, key)
	}
	var value any
	switch spec.shape {
	case shapeString:
		value = new(string)
	case shapeBool:
		value = new(bool)
	case shapeInt:
		value = new(int64)
	case shapeNumber:
		value = new(float64)
	case shapeStringArray:
		value = new([]string)
	case shapeStringMap:
		value = new(map[string]string)
	case shapeTemplateList:
		value = new([]relayTemplate)
	case shapeSealed:
		value = new(string)
	default:
		return nil, fmt.Errorf("%w: unsupported shape for %q", ErrInvalidSchemaValue, key)
	}
	if err := decodeStrictJSON(raw, value); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidSchemaValue, key, err)
	}
	if spec.shape == shapeTemplateList {
		if err := validateTemplates(*value.(*[]relayTemplate)); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidSchemaValue, key, err)
		}
	}
	if spec.shape == shapeSealed {
		encoded := *value.(*string)
		if encoded == "" {
			return nil, fmt.Errorf("%w: %s: empty sealed value", ErrInvalidSchemaValue, key)
		}
		if _, err := base64.StdEncoding.Strict().DecodeString(encoded); err != nil {
			return nil, fmt.Errorf("%w: %s: sealed value encoding", ErrInvalidSchemaValue, key)
		}
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: marshal: %v", ErrInvalidSchemaValue, key, err)
	}
	return normalized, nil
}

type relayTemplate struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Hotkey string `json:"hotkey,omitempty"`
}

func validateTemplates(templates []relayTemplate) error {
	seen := make(map[string]struct{}, len(templates))
	for _, template := range templates {
		if template.ID == "" || template.Label == "" {
			return errors.New("template id and label are required")
		}
		if _, duplicate := seen[template.ID]; duplicate {
			return fmt.Errorf("duplicate template id %q", template.ID)
		}
		seen[template.ID] = struct{}{}
	}
	return nil
}

func decodeStrictJSON(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return errors.New("empty JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// OrderedRecord is the plaintext schema-layer form of one entity in an
// ordered collection. Position is the legacy array rank; record ID is the
// stable merge key. Concurrent equal positions are ordered by record ID.
type OrderedRecord struct {
	Collection string
	RecordID   string
	Position   uint64
	Payload    json.RawMessage
}

// SplitRelayRecordArray converts a validated legacy array into independent
// canonical records. Currently quick_templates is the only plaintext legacy
// record array; sealed profiles and SSH bundles are decoded by Desktop codecs.
func SplitRelayRecordArray(key string, raw json.RawMessage) ([]OrderedRecord, error) {
	spec, ok := RelaySpec(key)
	if !ok || spec.Mode != RelayRecordArray {
		return nil, fmt.Errorf("%w: %q is not a record array", ErrInvalidSchemaValue, key)
	}
	normalized, err := NormalizeRelayValue(key, raw)
	if err != nil {
		return nil, err
	}
	var templates []relayTemplate
	if err := json.Unmarshal(normalized, &templates); err != nil {
		return nil, err
	}
	records := make([]OrderedRecord, len(templates))
	for i, template := range templates {
		payload, err := json.Marshal(template)
		if err != nil {
			return nil, err
		}
		records[i] = OrderedRecord{
			Collection: CollectionQuickTemplate,
			RecordID:   template.ID,
			Position:   uint64(i),
			Payload:    payload,
		}
	}
	return records, nil
}

// JoinRelayRecordArray materializes independent records back into the legacy
// array shape used by Relay. Tombstones must be removed by the caller.
func JoinRelayRecordArray(key string, records []OrderedRecord) (json.RawMessage, error) {
	spec, ok := RelaySpec(key)
	if !ok || spec.Mode != RelayRecordArray {
		return nil, fmt.Errorf("%w: %q is not a record array", ErrInvalidSchemaValue, key)
	}
	ordered := append([]OrderedRecord(nil), records...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return ordered[i].RecordID < ordered[j].RecordID
	})
	templates := make([]relayTemplate, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for _, record := range ordered {
		if record.Collection != CollectionQuickTemplate {
			return nil, fmt.Errorf("%w: record %q belongs to %q", ErrInvalidSchemaValue, record.RecordID, record.Collection)
		}
		var template relayTemplate
		if err := decodeStrictJSON(record.Payload, &template); err != nil {
			return nil, fmt.Errorf("%w: record %q: %v", ErrInvalidSchemaValue, record.RecordID, err)
		}
		if template.ID != record.RecordID {
			return nil, fmt.Errorf("%w: payload id does not match record id %q", ErrInvalidSchemaValue, record.RecordID)
		}
		if _, duplicate := seen[record.RecordID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate record id %q", ErrInvalidSchemaValue, record.RecordID)
		}
		seen[record.RecordID] = struct{}{}
		templates = append(templates, template)
	}
	if err := validateTemplates(templates); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchemaValue, err)
	}
	return json.Marshal(templates)
}
