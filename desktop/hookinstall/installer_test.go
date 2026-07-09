package hookinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustReadSettings(t *testing.T, home string) ClaudeSettings {
	t.Helper()
	c, err := readClaudeSettings(home)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return c
}

func TestInstall_FreshHome(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 1 {
		t.Fatalf("want 1 Notification entry; got %d: %+v", len(c.Hooks.Notification), c.Hooks.Notification)
	}
	if len(c.Hooks.PreToolUse) != 1 {
		t.Fatalf("want 1 PreToolUse entry; got %d: %+v", len(c.Hooks.PreToolUse), c.Hooks.PreToolUse)
	}
	link := attermHookSymlink(home)
	for _, e := range c.Hooks.Notification {
		for _, h := range e.Hooks {
			if h.Command != link {
				t.Errorf("Notification entry command = %q; want %q", h.Command, link)
			}
		}
	}
	pre := c.Hooks.PreToolUse[0]
	if pre.Matcher != "" {
		t.Errorf("PreToolUse matcher = %q; want \"\" (one entry covers AskUserQuestion + streaming)", pre.Matcher)
	}
	if len(pre.Hooks) != 1 || pre.Hooks[0].Command != link {
		t.Errorf("PreToolUse hooks = %+v; want one command pointing at %q", pre.Hooks, link)
	}
}

// The AI streaming card needs four claude-code hook events to populate body
// content: UserPromptSubmit (👤), Stop (🤖 final), PreToolUse + PostToolUse
// (🛠 calls). The installer must own all four slots; without them the anchor
// card stays stuck on "(waiting for output)" even after the user types.
func TestInstall_PopulatesAllStreamingHookSlots(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(claudeSettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{
		`"Notification"`,
		`"PreToolUse"`,
		`"UserPromptSubmit"`,
		`"Stop"`,
		`"PostToolUse"`,
	} {
		if !strings.Contains(string(data), slot) {
			t.Errorf("settings.json missing %s slot: %s", slot, data)
		}
	}
	link := attermHookSymlink(home)
	// Each owned slot must point at the atterm-hook symlink.
	count := strings.Count(string(data), link)
	if count < 5 {
		t.Errorf("settings.json references atterm-hook %d times; want >=5: %s", count, data)
	}
}

// Uninstall must strip the atterm-owned entry from every owned slot, not just
// the original two — otherwise the user is left with dead UserPromptSubmit/
// Stop/PostToolUse entries pointing at a removed symlink.
func TestUninstall_StripsAllStreamingHookSlots(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeSettingsPath(home))
	link := attermHookSymlink(home)
	if strings.Contains(string(data), link) {
		t.Errorf("uninstall left atterm-hook path in settings.json: %s", data)
	}
}

func TestInstall_PreservesExternalNotificationHook(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"Notification":[{"matcher":"","hooks":[{"type":"command","command":"/usr/local/bin/myhook"}]}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 2 {
		t.Fatalf("want 2 entries; got %d", len(c.Hooks.Notification))
	}
	if c.Hooks.Notification[0].Hooks[0].Command != "/usr/local/bin/myhook" {
		t.Errorf("external hook lost: %+v", c.Hooks.Notification[0])
	}
}

func TestInstall_MigratesLegacyAttermEntries(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	link := attermHookSymlink(home)
	// Settings left behind by a pre-fix atterm: object-typed matcher and a
	// top-level command — the shape Claude Code's /doctor rejected.
	raw := `{"hooks":{"Notification":[` +
		`{"matcher":{"type":"permission_prompt"},"command":"` + link + `"},` +
		`{"matcher":{"type":"idle_prompt","tool":"AskUserQuestion"},"command":"` + link + `"}` +
		`]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatalf("install over legacy entries should not error: %v", err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 1 {
		t.Fatalf("legacy entries not collapsed; want 1, got %d: %+v",
			len(c.Hooks.Notification), c.Hooks.Notification)
	}
	e := c.Hooks.Notification[0]
	if e.Matcher != "" || len(e.Hooks) != 1 || e.Hooks[0].Command != link {
		t.Errorf("migrated entry has wrong shape: %+v", e)
	}
	// The legacy object-typed matcher must be gone from disk.
	data, _ := os.ReadFile(claudeSettingsPath(home))
	if strings.Contains(string(data), `"matcher":{`) {
		t.Errorf("legacy object matcher survived on disk: %s", data)
	}
}

func TestInstall_PreservesOtherHookKinds(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	// Legacy-shape entry: object matcher + top-level command. The
	// HookEntry unmarshaler normalizes it; we just need to confirm the
	// command (a non-atterm one) survives the install.
	raw := `{"hooks":{"PreToolUse":[{"matcher":{"type":"x"},"command":"/u/y"}],"SessionStart":[{"matcher":"","hooks":[{"type":"command","command":"/u/onstart"}]}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeSettingsPath(home))
	if !strings.Contains(string(data), `"SessionStart"`) || !strings.Contains(string(data), `/u/onstart`) {
		t.Errorf("SessionStart dropped: %s", data)
	}
	c := mustReadSettings(t, home)
	// The user's external PreToolUse(/u/y) entry must survive AND our
	// AskUserQuestion entry must be appended.
	if len(c.Hooks.PreToolUse) != 2 {
		t.Fatalf("want 2 PreToolUse entries (external + atterm); got %d: %+v", len(c.Hooks.PreToolUse), c.Hooks.PreToolUse)
	}
	if c.Hooks.PreToolUse[0].Hooks[0].Command != "/u/y" {
		t.Errorf("external PreToolUse hook lost: %+v", c.Hooks.PreToolUse[0])
	}
	if c.Hooks.PreToolUse[1].Matcher != "" {
		t.Errorf("atterm PreToolUse entry should use empty matcher: %+v", c.Hooks.PreToolUse[1])
	}
}

func TestInstall_PreservesExternalPreToolUseHook(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"PreToolUse":[{"matcher":"AskUserQuestion","hooks":[{"type":"command","command":"/usr/local/bin/competing"}]}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.PreToolUse) != 2 {
		t.Fatalf("want 2 PreToolUse entries (external + atterm); got %d", len(c.Hooks.PreToolUse))
	}
	if c.Hooks.PreToolUse[0].Hooks[0].Command != "/usr/local/bin/competing" {
		t.Errorf("competing external AskUserQuestion hook lost: %+v", c.Hooks.PreToolUse[0])
	}
}

func TestUninstall_RemovesPreToolUseEntries(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.PreToolUse) == 0 {
		t.Fatalf("install did not create a PreToolUse entry: %+v", c)
	}
	// Add an external PreToolUse entry post-install — it must survive uninstall.
	c.Hooks.PreToolUse = append([]HookEntry{
		entry("AskUserQuestion", "/u/bin/competing"),
	}, c.Hooks.PreToolUse...)
	if err := writeClaudeSettings(home, c); err != nil {
		t.Fatal(err)
	}

	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	c = mustReadSettings(t, home)
	if len(c.Hooks.PreToolUse) != 1 {
		t.Fatalf("want 1 external PreToolUse entry left; got %d: %+v", len(c.Hooks.PreToolUse), c.Hooks.PreToolUse)
	}
	if c.Hooks.PreToolUse[0].Hooks[0].Command != "/u/bin/competing" {
		t.Errorf("uninstall took the wrong PreToolUse entry: %+v", c.Hooks.PreToolUse[0])
	}
}

func TestInstall_IdempotentSkipsWrite(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	path := claudeSettingsPath(home)
	first, _ := os.Stat(path)
	// Run again; mtime must not change.
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(path)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Errorf("settings.json rewritten on idempotent Install")
	}
}

func TestInstall_RefusesToOverwriteInvalidJSON(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	path := claudeSettingsPath(home)
	os.WriteFile(path, []byte("not json"), 0o644)
	err := installAt(home)
	if err == nil {
		t.Errorf("expected error on invalid JSON")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not json" {
		t.Errorf("invalid JSON overwritten: %s", data)
	}
}

func TestUninstall_RemovesAttermEntriesOnly(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	// Add an external entry post-install.
	c := mustReadSettings(t, home)
	c.Hooks.Notification = append([]HookEntry{
		entry("", "/u/bin/mine"),
	}, c.Hooks.Notification...)
	if err := writeClaudeSettings(home, c); err != nil {
		t.Fatal(err)
	}

	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	c = mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 1 {
		t.Fatalf("want 1 external entry left; got %d", len(c.Hooks.Notification))
	}
	if c.Hooks.Notification[0].Hooks[0].Command != "/u/bin/mine" {
		t.Errorf("uninstall took the wrong entry: %+v", c.Hooks.Notification[0])
	}
	if _, err := os.Lstat(attermHookSymlink(home)); err == nil {
		t.Errorf("symlink not removed")
	}
}

func TestUninstall_DoesNotDeleteVersionedBinaries(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	if err := uninstallAt(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("versioned binary removed: %v", err)
	}
}
