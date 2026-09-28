package configsync

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeRelayValueCoversStrictShapes(t *testing.T) {
	valid := map[string]string{
		"locale_preference":                ` "zh-CN" `,
		"quick_templates":                  `[{"text":"yes","label":"Yes","id":"one"}]`,
		"notifications_enabled":            `true`,
		"ai_notifications_only":            `false`,
		"command_notify_threshold_seconds": `10`,
		"shell_integration_enabled":        `true`,
		"pinned_session_ids":               `["s2","s1"]`,
		"ssh_hosts_encrypted":              `"YWJj"`,
		"terminal_theme":                   `"nord"`,
		"terminal_font_head":               `"JetBrains Mono"`,
		"terminal_font_size":               `16`,
		"terminal_line_height":             `1.2`,
		"terminal_cursor_style":            `"bar"`,
		"terminal_cursor_blink":            `true`,
		"terminal_scrollback":              `9000`,
		"default_shell":                    `"/bin/zsh"`,
		"shortcut_bindings":                `{"tab.new":"Mod+T"}`,
		"profiles_encrypted":               `"ZGVm"`,
	}
	if len(valid) != len(RelayKeys()) {
		t.Fatalf("valid fixture keys=%d schema keys=%d", len(valid), len(RelayKeys()))
	}
	for key, raw := range valid {
		t.Run(key, func(t *testing.T) {
			normalized, err := NormalizeRelayValue(key, json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			var decoded any
			if err := json.Unmarshal(normalized, &decoded); err != nil {
				t.Fatalf("normalized JSON: %v", err)
			}
		})
	}

	invalid := map[string]string{
		"locale_preference":                `12`,
		"quick_templates":                  `[{"id":"dup","label":"a","text":"a"},{"id":"dup","label":"b","text":"b"}]`,
		"notifications_enabled":            `"true"`,
		"command_notify_threshold_seconds": `1.5`,
		"pinned_session_ids":               `["ok",3]`,
		"profiles_encrypted":               `"not base64!"`,
		"shortcut_bindings":                `{"tab.new":4}`,
		"terminal_font_size":               `16 true`,
	}
	for key, raw := range invalid {
		t.Run("invalid_"+key, func(t *testing.T) {
			if _, err := NormalizeRelayValue(key, json.RawMessage(raw)); !errors.Is(err, ErrInvalidSchemaValue) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestNormalizeRelayValueHasSemanticStableHashInput(t *testing.T) {
	left, err := NormalizeRelayValue("shortcut_bindings", json.RawMessage(` { "z": "last", "a": "first" } `))
	if err != nil {
		t.Fatal(err)
	}
	right, err := NormalizeRelayValue("shortcut_bindings", json.RawMessage(`{"a":"first","z":"last"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) || relayValueHash(left) != relayValueHash(right) {
		t.Fatalf("normalization differs left=%s right=%s", left, right)
	}
}

func TestRelayKeySpecsSeparatePortableAndVaultCollections(t *testing.T) {
	profiles, ok := RelaySpec("profiles_encrypted")
	if !ok || profiles.Mode != RelaySealedBundle {
		t.Fatalf("profiles spec=%+v ok=%v", profiles, ok)
	}
	if !reflect.DeepEqual(profiles.Collections, []string{CollectionProfiles, CollectionProfileEnv}) ||
		!reflect.DeepEqual(profiles.KeyClasses, []KeyClass{KeyClassSync, KeyClassVault}) {
		t.Fatalf("profiles spec=%+v", profiles)
	}
	ssh, ok := RelaySpec("ssh_hosts_encrypted")
	if !ok || !reflect.DeepEqual(ssh.Collections, []string{
		CollectionSSHHosts, CollectionSSHKeys, CollectionSSHCredential, CollectionSSHKeySecret,
	}) || !reflect.DeepEqual(ssh.KeyClasses, []KeyClass{KeyClassSync, KeyClassVault}) {
		t.Fatalf("ssh spec=%+v ok=%v", ssh, ok)
	}
	if scalar, _ := RelaySpec("terminal_theme"); scalar.Mode != RelayScalar || scalar.Collections[0] != CollectionPreferences {
		t.Fatalf("scalar spec=%+v", scalar)
	}
}

func TestSplitAndJoinRelayRecordArrayUsesStableIDsAndOrder(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"second","label":"Second","text":"2"},
		{"id":"first","label":"First","text":"1","hotkey":"Alt+1"}
	]`)
	records, err := SplitRelayRecordArray("quick_templates", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].RecordID != "second" || records[0].Position != 0 || records[1].RecordID != "first" || records[1].Position != 1 {
		t.Fatalf("records=%+v", records)
	}
	// Input order does not matter when materializing; position then stable ID
	// determines the whole-array compatibility form.
	records[0], records[1] = records[1], records[0]
	joined, err := JoinRelayRecordArray("quick_templates", records)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeRelayValue("quick_templates", raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(joined) != string(normalized) {
		t.Fatalf("joined=%s normalized=%s", joined, normalized)
	}
}

func TestJoinRelayRecordArrayRejectsPayloadIdentityMismatch(t *testing.T) {
	_, err := JoinRelayRecordArray("quick_templates", []OrderedRecord{{
		Collection: CollectionQuickTemplate,
		RecordID:   "outer",
		Payload:    json.RawMessage(`{"id":"inner","label":"x","text":"x"}`),
	}})
	if !errors.Is(err, ErrInvalidSchemaValue) {
		t.Fatalf("error=%v", err)
	}
}
