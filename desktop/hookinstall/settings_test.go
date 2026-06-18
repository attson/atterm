package hookinstall

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMergeAttermEntries(t *testing.T) {
	desired := []HookEntry{
		{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/H/.atterm/bin/atterm-hook"},
		{Matcher: HookMatcher{Type: "idle_prompt", Tool: "AskUserQuestion"}, Command: "/H/.atterm/bin/atterm-hook"},
	}
	cases := []struct {
		name     string
		existing []HookEntry
		want     []HookEntry
	}{
		{
			name:     "empty existing",
			existing: nil,
			want:     desired,
		},
		{
			name: "existing has only external hooks — appended",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
		{
			name: "existing has stale atterm entries — replaced",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/old/.atterm/bin/atterm-hook"},
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
		{
			name: "idempotent: running on already-installed produces same output",
			existing: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
			want: []HookEntry{
				{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/usr/local/bin/myhook"},
				desired[0], desired[1],
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeAttermEntries(c.existing, desired, isAttermHookCommand)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestReadWriteSettings_Roundtrip(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(claudeDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	path := claudeSettingsPath(home)
	// Pre-populate with other top-level keys that must be preserved.
	raw := `{"theme":"dark","hooks":{"Notification":[{"matcher":{"type":"permission_prompt"},"command":"/u/bin/x"}]}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Hooks.Notification) != 1 || cfg.Hooks.Notification[0].Command != "/u/bin/x" {
		t.Errorf("read lost the existing entry: %+v", cfg.Hooks.Notification)
	}

	cfg.Hooks.Notification = append(cfg.Hooks.Notification, HookEntry{
		Matcher: HookMatcher{Type: "idle_prompt"}, Command: "/u/bin/y",
	})
	if err := writeClaudeSettings(home, cfg); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(path)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("written JSON unreadable: %v", err)
	}
	if _, ok := probe["theme"]; !ok {
		t.Errorf("write dropped the unrelated top-level field theme; got %s", out)
	}
}

func TestReadClaudeSettings_MissingFile(t *testing.T) {
	home := t.TempDir()
	cfg, err := readClaudeSettings(home)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Hooks.Notification != nil {
		t.Errorf("missing file should yield empty cfg; got %+v", cfg)
	}
}

func TestReadClaudeSettings_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home), []byte("not json"), 0o644)
	_, err := readClaudeSettings(home)
	if err == nil {
		t.Errorf("invalid JSON should surface an error")
	}
}

func TestWriteClaudeSettings_AtomicTempCleared(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	cfg := ClaudeSettings{}
	if err := writeClaudeSettings(home, cfg); err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(claudeDir(home), "settings.json.atterm-tmp-*"))
	if len(entries) != 0 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestExtraPreservesLargeIntegers(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	// 12345678901234567 > 2^53 (9007199254740992); float64 loses the last bit.
	raw := `{"timestamp":12345678901234567,"hooks":{}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	cfg, err := readClaudeSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeSettings(home, cfg); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(claudeSettingsPath(home))
	if !strings.Contains(string(out), "12345678901234567") {
		t.Errorf("large integer corrupted by round-trip: %s", out)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	raw := `{"theme":"dark","model":"opus","hooks":{"Notification":[{"matcher":{"type":"x"},"command":"/u/y"}]}}`
	var cfg ClaudeSettings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	a, _ := json.MarshalIndent(cfg, "", "  ")
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if !bytes.Equal(a, b) {
		t.Errorf("non-deterministic marshal:\n%s\nvs\n%s", a, b)
	}
}
