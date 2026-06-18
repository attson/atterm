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
	if len(c.Hooks.Notification) != 2 {
		t.Fatalf("want 2 entries; got %d: %+v", len(c.Hooks.Notification), c.Hooks.Notification)
	}
	link := attermHookSymlink(home)
	for _, e := range c.Hooks.Notification {
		if e.Command != link {
			t.Errorf("entry command = %q; want %q", e.Command, link)
		}
	}
}

func TestInstall_PreservesExternalNotificationHook(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"Notification":[{"matcher":{"type":"permission_prompt"},"command":"/usr/local/bin/myhook"}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	c := mustReadSettings(t, home)
	if len(c.Hooks.Notification) != 3 {
		t.Fatalf("want 3 entries; got %d", len(c.Hooks.Notification))
	}
	if c.Hooks.Notification[0].Command != "/usr/local/bin/myhook" {
		t.Errorf("external hook lost: %+v", c.Hooks.Notification[0])
	}
}

func TestInstall_PreservesOtherHookKinds(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(claudeDir(home), 0o700)
	raw := `{"hooks":{"PreToolUse":[{"matcher":{"type":"x"},"command":"/u/y"}]}}`
	os.WriteFile(claudeSettingsPath(home), []byte(raw), 0o644)

	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(claudeSettingsPath(home))
	if !strings.Contains(string(data), `"PreToolUse"`) {
		t.Errorf("PreToolUse dropped: %s", data)
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
		{Matcher: HookMatcher{Type: "permission_prompt"}, Command: "/u/bin/mine"},
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
	if c.Hooks.Notification[0].Command != "/u/bin/mine" {
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
