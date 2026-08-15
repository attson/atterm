package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testLink = "/Users/x/.atterm/bin/atterm-hook"

func readCodexDoc(t *testing.T, home string) map[string][]codexHookEntry {
	t.Helper()
	raw, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	doc := map[string][]codexHookEntry{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v\n%s", err, raw)
	}
	return doc
}

func TestInstallCodex_CoversEveryStateEvent(t *testing.T) {
	home := t.TempDir()
	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	for _, event := range codexHookEvents {
		entries, ok := doc[event]
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
	for event, entries := range readCodexDoc(t, home) {
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
	existing := `{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}],` +
		`"SessionStart":[{"hooks":[{"type":"command","command":"/usr/bin/false"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installCodexAt(home, testLink); err != nil {
		t.Fatalf("installCodexAt: %v", err)
	}
	doc := readCodexDoc(t, home)
	if len(doc["Stop"]) != 2 {
		t.Fatalf("Stop entries = %v, want the user's plus ours", doc["Stop"])
	}
	if doc["Stop"][0].Hooks[0].Command != "/usr/bin/true" {
		t.Fatalf("clobbered the user's Stop hook: %v", doc["Stop"])
	}
	// An event we do not own must survive untouched.
	if len(doc["SessionStart"]) != 1 || doc["SessionStart"][0].Hooks[0].Command != "/usr/bin/false" {
		t.Fatalf("touched an event we do not own: %v", doc["SessionStart"])
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
	if len(readCodexDoc(t, home)) != len(codexHookEvents) {
		t.Fatal("install did not rebuild the file")
	}
}

func TestUninstallCodex_LeavesOnlyForeignEntries(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}]}`
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
	if len(doc) != 1 || len(doc["Stop"]) != 1 || doc["Stop"][0].Hooks[0].Command != "/usr/bin/true" {
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
