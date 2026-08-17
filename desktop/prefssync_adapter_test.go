package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

func newTestConfigStore(t *testing.T) *configStore {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// os.UserConfigDir consults XDG_CONFIG_HOME on Linux (and other
	// platform-specific vars) BEFORE falling back to $HOME/.config. CI runners
	// set XDG_CONFIG_HOME, so without clearing it every test's configStore
	// would share one config.json and cross-contaminate (host/key rows leaking
	// between tests). Point every config-dir env at the per-test temp dir so
	// each configStore is hermetic.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("LocalAppData", filepath.Join(tmp, "local")) // Windows
	return loadConfig()
}

func TestAdapter_PinnedSessionIds_RoundTrip(t *testing.T) {
	cs := newTestConfigStore(t)
	a := newAppConfigAdapter(cs, func() []byte { return nil })

	want := []string{"sid-a", "sid-b"}
	raw, _ := json.Marshal(want)
	if err := a.WriteValue("pinned_session_ids", raw); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	got, ok := a.ReadValue("pinned_session_ids")
	if !ok {
		t.Fatal("ReadValue returned ok=false")
	}
	var back []string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 || back[0] != "sid-a" || back[1] != "sid-b" {
		t.Fatalf("got %v; want %v", back, want)
	}

	// Adapter must expose the new key.
	found := false
	for _, k := range a.Keys() {
		if k == "pinned_session_ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Keys() missing pinned_session_ids: %v", a.Keys())
	}

	_ = prefssync.Meta{} // silence unused import if none other
}

func TestAdapterRoundTripsL1Keys(t *testing.T) {
	keys := []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	}
	for _, k := range keys {
		if !slices.Contains(prefssync.SyncedKeys(), k) {
			t.Errorf("%s is not in SyncedKeys()", k)
		}
	}
}

func TestIsPrefCustomizedCoversL1Keys(t *testing.T) {
	blink := false
	customized := appConfig{
		TerminalTheme:       "nord",
		TerminalFontHead:    "JetBrains Mono",
		TerminalFontSize:    16,
		TerminalLineHeight:  1.2,
		TerminalCursorStyle: "bar",
		TerminalCursorBlink: &blink,
		TerminalScrollback:  9000,
		DefaultShell:        "/bin/zsh",
		ShortcutBindings:    map[string]string{"tab.new": "Mod+KeyP"},
	}
	isCustom := isPrefCustomized(customized)
	isVirgin := isPrefCustomized(appConfig{})
	for _, k := range []string{
		"terminal_theme", "terminal_font_head", "terminal_font_size",
		"terminal_line_height", "terminal_cursor_style", "terminal_cursor_blink",
		"terminal_scrollback", "default_shell", "shortcut_bindings",
	} {
		if !isCustom(k) {
			t.Errorf("%s: explicitly set value must count as customized — otherwise first login pulls a remote value over it", k)
		}
		if isVirgin(k) {
			t.Errorf("%s: an untouched config must not count as customized", k)
		}
	}
}
