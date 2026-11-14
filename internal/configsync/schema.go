package configsync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	SchemaVersion = 1

	CollectionPreferences   = "preferences"
	CollectionQuickTemplate = "quick_templates"
	CollectionSnippet       = "snippets"
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
// Collections includes retired namespaces so old replica records remain valid.
// Active sealed bundles split metadata from capability-gated vault records.
type RelayKeySpec struct {
	Key         string
	Mode        RelayValueMode
	Collections []string
	KeyClasses  []KeyClass
	Retired     bool
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
	shapeSnippetList  relayJSONShape = "snippet_list"
	shapeSealed       relayJSONShape = "sealed"
)

var relayKeySpecs = []RelayKeySpec{
	{Key: "ai_notifications_only", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "command_notify_threshold_seconds", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "locale_preference", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "notifications_enabled", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "pinned_session_ids", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeStringArray},
	{Key: "profiles_encrypted", Retired: true, Mode: RelaySealedBundle, Collections: []string{CollectionProfiles, CollectionProfileConfig, CollectionProfileEnv}, KeyClasses: []KeyClass{KeyClassSync, KeyClassVault}, shape: shapeSealed},
	{Key: "quick_templates", Mode: RelayRecordArray, Collections: []string{CollectionQuickTemplate}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeTemplateList},
	{Key: "shell_integration_enabled", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "shortcut_bindings", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeStringMap},
	{Key: "snippets", Mode: RelayRecordArray, Collections: []string{CollectionSnippet}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeSnippetList},
	{Key: "ssh_hosts_encrypted", Mode: RelaySealedBundle, Collections: []string{CollectionSSHHosts, CollectionSSHKeys, CollectionSSHCredential, CollectionSSHKeySecret}, KeyClasses: []KeyClass{KeyClassSync, KeyClassVault}, shape: shapeSealed},
	{Key: "terminal_cursor_blink", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeBool},
	{Key: "terminal_cursor_style", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "terminal_font_head", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "terminal_font_size", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "terminal_line_height", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeNumber},
	{Key: "terminal_scrollback", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeInt},
	{Key: "terminal_theme", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
	{Key: "traffic_time_zone", Mode: RelayScalar, Collections: []string{CollectionPreferences}, KeyClasses: []KeyClass{KeyClassSync}, shape: shapeString},
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
// schema, including retired compatibility tombstones.
func RelayKeys() []string {
	keys := make([]string, len(relayKeySpecs))
	for i := range relayKeySpecs {
		keys[i] = relayKeySpecs[i].Key
	}
	return keys
}

// ActiveRelayKeys excludes compatibility tombstones from current sync.
func ActiveRelayKeys() []string {
	var keys []string
	for _, spec := range relayKeySpecs {
		if !spec.Retired {
			keys = append(keys, spec.Key)
		}
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
	case shapeSnippetList:
		value = new([]relaySnippet)
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
	if spec.shape == shapeSnippetList {
		snippets := *value.(*[]relaySnippet)
		if snippets == nil {
			return nil, fmt.Errorf("%w: %s: value must be an array", ErrInvalidSchemaValue, key)
		}
		if err := validateSnippets(snippets); err != nil {
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

type relaySnippet struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
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

func validateSnippets(snippets []relaySnippet) error {
	if len(snippets) > 200 {
		return errors.New("at most 200 snippets are allowed")
	}
	seen := make(map[string]struct{}, len(snippets))
	for _, snippet := range snippets {
		if strings.TrimSpace(snippet.ID) == "" || strings.TrimSpace(snippet.Label) == "" || strings.TrimSpace(snippet.Text) == "" {
			return errors.New("snippet id, label and text are required")
		}
		if !utf8.ValidString(snippet.Label) || !utf8.ValidString(snippet.Text) {
			return fmt.Errorf("snippet %q must be valid UTF-8", snippet.ID)
		}
		if utf8.RuneCountInString(snippet.Label) > 80 {
			return fmt.Errorf("snippet %q label exceeds 80 Unicode code points", snippet.ID)
		}
		if len(snippet.Text) > 64<<10 {
			return fmt.Errorf("snippet %q text exceeds 65536 bytes", snippet.ID)
		}
		if _, duplicate := seen[snippet.ID]; duplicate {
			return fmt.Errorf("duplicate snippet id %q", snippet.ID)
		}
		seen[snippet.ID] = struct{}{}
	}
	encoded, err := json.Marshal(snippets)
	if err != nil {
		return err
	}
	if len(encoded) > 1<<20 {
		return errors.New("snippets collection exceeds 1048576 bytes")
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
// canonical records. Active sealed bundles are decoded by Desktop codecs.
func SplitRelayRecordArray(key string, raw json.RawMessage) ([]OrderedRecord, error) {
	spec, ok := RelaySpec(key)
	if !ok || spec.Mode != RelayRecordArray {
		return nil, fmt.Errorf("%w: %q is not a record array", ErrInvalidSchemaValue, key)
	}
	normalized, err := NormalizeRelayValue(key, raw)
	if err != nil {
		return nil, err
	}
	switch spec.shape {
	case shapeTemplateList:
		return splitOrderedRecords(normalized, CollectionQuickTemplate, func(item relayTemplate) string { return item.ID })
	case shapeSnippetList:
		return splitOrderedRecords(normalized, CollectionSnippet, func(item relaySnippet) string { return item.ID })
	default:
		return nil, fmt.Errorf("%w: %q has unsupported record shape", ErrInvalidSchemaValue, key)
	}
}

func splitOrderedRecords[T any](normalized json.RawMessage, collection string, id func(T) string) ([]OrderedRecord, error) {
	var items []T
	if err := json.Unmarshal(normalized, &items); err != nil {
		return nil, err
	}
	records := make([]OrderedRecord, len(items))
	for i, item := range items {
		payload, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		records[i] = OrderedRecord{Collection: collection, RecordID: id(item), Position: uint64(i), Payload: payload}
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
	switch spec.shape {
	case shapeTemplateList:
		return joinOrderedRecords(ordered, CollectionQuickTemplate, func(item relayTemplate) string { return item.ID }, validateTemplates)
	case shapeSnippetList:
		return joinOrderedRecords(ordered, CollectionSnippet, func(item relaySnippet) string { return item.ID }, validateSnippets)
	default:
		return nil, fmt.Errorf("%w: %q has unsupported record shape", ErrInvalidSchemaValue, key)
	}
}

func joinOrderedRecords[T any](ordered []OrderedRecord, collection string, id func(T) string, validate func([]T) error) (json.RawMessage, error) {
	items := make([]T, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for _, record := range ordered {
		if record.Collection != collection {
			return nil, fmt.Errorf("%w: record %q belongs to %q", ErrInvalidSchemaValue, record.RecordID, record.Collection)
		}
		var item T
		if err := decodeStrictJSON(record.Payload, &item); err != nil {
			return nil, fmt.Errorf("%w: record %q: %v", ErrInvalidSchemaValue, record.RecordID, err)
		}
		if id(item) != record.RecordID {
			return nil, fmt.Errorf("%w: payload id does not match record id %q", ErrInvalidSchemaValue, record.RecordID)
		}
		if _, duplicate := seen[record.RecordID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate record id %q", ErrInvalidSchemaValue, record.RecordID)
		}
		seen[record.RecordID] = struct{}{}
		items = append(items, item)
	}
	if err := validate(items); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSchemaValue, err)
	}
	return json.Marshal(items)
}
