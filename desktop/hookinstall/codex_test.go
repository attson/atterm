package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLink = "/Users/x/.atterm/bin/atterm-hook"

func readCodexDoc(t *testing.T, home string) codexHooksDocument {
	t.Helper()
	raw, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	doc, _ := readCodexHooksDocument(raw)
	if doc.Hooks == nil {
		t.Fatalf("hooks.json has no top-level hooks object:\n%s", raw)
	}
	return doc
}

func TestInstallCodex_CoversIdentityAndStateEvents(t *testing.T) {
	home := t.TempDir()
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	for _, event := range codexHookEvents {
		entries, ok := doc.Hooks[event]
		if !ok || len(entries) != 1 {
			t.Fatalf("event %s: entries = %v, want exactly one", event, entries)
		}
		cmd := entries[0].Hooks[0].Command
		if !strings.Contains(cmd, "--agent codex") {
			t.Fatalf("event %s: command %q must name its agent", event, cmd)
		}
	}
}

// Runs on every launch, so a second call must neither change the file nor
// stack a second copy of our entry.
func TestInstallCodex_Idempotent(t *testing.T) {
	home := t.TempDir()
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("second install rewrote the file:\n%s\n---\n%s", first, second)
	}
}

// A version bump changes the binary path; the old line must be replaced, not
// joined by a second one that points at a binary that may be gone.
func TestInstallCodex_ReplacesOurOlderEntry(t *testing.T) {
	home := t.TempDir()
	if err := installCodexAt(home, "/Users/x/.atterm/bin/atterm-hook-old"); err != nil {
		t.Fatal(err)
	}
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatal(err)
	}
	for event, entries := range readCodexDoc(t, home).Hooks {
		if len(entries) != 1 {
			t.Fatalf("event %s: %d entries, want 1", event, len(entries))
		}
		if strings.Contains(entries[0].Hooks[0].Command, "atterm-hook-old") {
			t.Fatalf("event %s still points at the old binary", event)
		}
	}
}

// The file is the user's; codex loads every entry in it.
func TestInstallCodex_PreservesForeignEntries(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"description":"user hooks","schema_version":2,"custom":{"enabled":true},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}],` +
		`"SessionStart":[{"hooks":[{"type":"command","command":"/usr/bin/false"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	if doc.Description != "user hooks" {
		t.Fatalf("description = %q, want preserved metadata", doc.Description)
	}
	var custom struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(doc.Extra["custom"], &custom); err != nil {
		t.Fatalf("decode preserved custom metadata: %v", err)
	}
	if string(doc.Extra["schema_version"]) != "2" || !custom.Enabled {
		t.Fatalf("unknown top-level metadata was not preserved: %v", doc.Extra)
	}
	if len(doc.Hooks["Stop"]) != 2 {
		t.Fatalf("Stop entries = %v, want the user's plus ours", doc.Hooks["Stop"])
	}
	if doc.Hooks["Stop"][0].Hooks[0].Command != "/usr/bin/true" {
		t.Fatalf("clobbered the user's Stop hook: %v", doc.Hooks["Stop"])
	}
	// SessionStart is now also ours for authoritative recovery identity, but
	// the user's existing handler must remain first and untouched.
	if len(doc.Hooks["SessionStart"]) != 2 || doc.Hooks["SessionStart"][0].Hooks[0].Command != "/usr/bin/false" {
		t.Fatalf("did not preserve the user's SessionStart hook: %v", doc.Hooks["SessionStart"])
	}
}

func TestInstallCodex_MigratesLegacyFlatDocument(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"note":"keep me","Stop":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	if len(doc.Hooks["Stop"]) != 2 || doc.Hooks["Stop"][0].Hooks[0].Command != "/usr/bin/true" {
		t.Fatalf("legacy Stop hook was not migrated: %v", doc.Hooks["Stop"])
	}
	if string(doc.Extra["note"]) != `"keep me"` {
		t.Fatalf("legacy metadata was not preserved: %v", doc.Extra)
	}
}

// A hand-broken file must not stop the install; we start clean rather than
// refusing to run.
func TestInstallCodex_SurvivesUnparseableFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	if len(readCodexDoc(t, home).Hooks) != len(codexHookEvents) {
		t.Fatal("install did not rebuild the file")
	}
}

func TestUninstallCodex_LeavesOnlyForeignEntries(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatal(err)
	}
	if err := uninstallCodexAt(home); err != nil {
		t.Fatalf("uninstallCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	if len(doc.Hooks) != 1 || len(doc.Hooks["Stop"]) != 1 || doc.Hooks["Stop"][0].Hooks[0].Command != "/usr/bin/true" {
		t.Fatalf("uninstall should leave exactly the user's own hook, got %v", doc)
	}
}

func TestUninstallCodex_NoFileIsFine(t *testing.T) {
	if err := uninstallCodexAt(t.TempDir()); err != nil {
		t.Fatalf("uninstall with no file: %v", err)
	}
}

// Both installers name their agent. Leaving claude on the implicit default
// would make the two paths differ for no reason, and the receiving end would
// have to guess which CLI called it.
func TestAttermHookCommand_NamesItsAgent(t *testing.T) {
	if got := attermHookCommand(testLink, "claude-code"); got != testLink+" --agent claude-code" {
		t.Fatalf("claude command = %q", got)
	}
	if got := attermHookCommand(testLink, "codex"); got != testLink+" --agent codex" {
		t.Fatalf("codex command = %q", got)
	}
}

// The health check stats the binary, not the whole command line, and a home
// directory is allowed to contain spaces.
func TestHookCommandBinary_StripsFlagsNotPaths(t *testing.T) {
	cases := map[string]string{
		"/a/b/atterm-hook --agent codex":              "/a/b/atterm-hook",
		"/My Files/.atterm/bin/atterm-hook --agent x": "/My Files/.atterm/bin/atterm-hook",
		"/a/b/atterm-hook":                            "/a/b/atterm-hook",
	}
	for in, want := range cases {
		if got := hookCommandBinary(in); got != want {
			t.Fatalf("hookCommandBinary(%q) = %q, want %q", in, got, want)
		}
	}
}
