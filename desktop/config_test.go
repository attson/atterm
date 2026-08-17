package main

import (
	"encoding/json"
	"sync"
	"testing"
)

// TestAppConfig_DeserializeOldJSON_RelayPausedDefaultsFalse verifies that old
// config.json files without a "relay_paused" key deserialize with
// RelayPaused == false. This ensures zero-value semantics are preserved and no
// migration code is needed for existing installs.
func TestAppConfig_DeserializeOldJSON_RelayPausedDefaultsFalse(t *testing.T) {
	raw := `{"relay_url":"wss://x","relay_session_token":"atk_abc123"}`
	var cfg appConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if cfg.RelayURL != "wss://x" {
		t.Fatalf("RelayURL = %q; want %q", cfg.RelayURL, "wss://x")
	}
	if cfg.RelaySessionToken != "atk_abc123" {
		t.Fatalf("RelaySessionToken = %q; want %q", cfg.RelaySessionToken, "atk_abc123")
	}
	if cfg.RelayPaused {
		t.Fatal("RelayPaused = true; want false for old config.json without the key")
	}
}

func TestPrefsMeta_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	store := loadConfig()
	c := store.Get()
	c.PrefsMeta = map[string]prefsMetaEntry{
		"locale_preference": {UpdatedAtLocal: 1700, Dirty: true},
	}
	if err := store.Set(c); err != nil {
		t.Fatalf("Set: %v", err)
	}

	store2 := loadConfig()
	got := store2.Get().PrefsMeta["locale_preference"]
	if got.UpdatedAtLocal != 1700 || !got.Dirty {
		t.Fatalf("round-trip lost data: %+v", got)
	}
}

func TestHookAutoInstallEnabledOrDefault(t *testing.T) {
	c := appConfig{}
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("nil pointer should default to true (fresh installs opt in)")
	}
	v := false
	c.HookAutoInstallEnabled = &v
	if c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("explicit false should disable")
	}
	t2 := true
	c.HookAutoInstallEnabled = &t2
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Errorf("explicit true should enable")
	}
}

func TestLocalePreferenceOrDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  appConfig
		want string
	}{
		{name: "empty defaults to system", cfg: appConfig{}, want: localePreferenceSystem},
		{name: "system allowed", cfg: appConfig{LocalePreference: localePreferenceSystem}, want: localePreferenceSystem},
		{name: "english allowed", cfg: appConfig{LocalePreference: localePreferenceEnglish}, want: localePreferenceEnglish},
		{name: "simplified chinese allowed", cfg: appConfig{LocalePreference: localePreferenceChineseSimplified}, want: localePreferenceChineseSimplified},
		{name: "unknown falls back to system", cfg: appConfig{LocalePreference: "fr"}, want: localePreferenceSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.LocalePreferenceOrDefault(); got != tt.want {
				t.Fatalf("LocalePreferenceOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Regression: Get() returned appConfig by value, but PrefsMeta and
// PrefsSeedMarkers are maps — a struct copy duplicates the header, not the
// backing table. Every caller (updatePref, the prefssync adapter's WriteMeta,
// the login seed path) then mutated the store's live map *outside* the mutex.
// A pin drives that from three goroutines within a few hundred ms (the UI
// binding, the background Push, and the relay prefs-watch Pull), which in Go
// is a fatal "concurrent map writes" or a corrupted hash table.
func TestConfigStore_GetReturnsIndependentMaps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store := loadConfig()
	c := store.Get()
	c.PrefsMeta = map[string]prefsMetaEntry{"pinned_session_ids": {UpdatedAtLocal: 1, Dirty: true}}
	c.PrefsSeedMarkers = map[string]bool{"user-1": true}
	if err := store.Set(c); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A snapshot handed to a caller must not alias the stored maps.
	snap := store.Get()
	snap.PrefsMeta["pinned_session_ids"] = prefsMetaEntry{UpdatedAtLocal: 999}
	snap.PrefsSeedMarkers["user-1"] = false

	got := store.Get()
	if e := got.PrefsMeta["pinned_session_ids"]; e.UpdatedAtLocal != 1 || !e.Dirty {
		t.Fatalf("snapshot mutation leaked into the store: %+v", e)
	}
	if !got.PrefsSeedMarkers["user-1"] {
		t.Fatal("snapshot mutation leaked into PrefsSeedMarkers")
	}
}

func TestConfigStore_SetCopiesCallerMaps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store := loadConfig()
	meta := map[string]prefsMetaEntry{"locale_preference": {UpdatedAtLocal: 7}}
	markers := map[string]bool{"user-1": true}
	c := store.Get()
	c.PrefsMeta = meta
	c.PrefsSeedMarkers = markers
	if err := store.Set(c); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The caller still holds the map it passed in; writing to it afterwards
	// must not reach the store.
	meta["locale_preference"] = prefsMetaEntry{UpdatedAtLocal: 42}
	markers["user-1"] = false

	got := store.Get()
	if got.PrefsMeta["locale_preference"].UpdatedAtLocal != 7 {
		t.Fatalf("caller map still aliases the store: %+v", got.PrefsMeta)
	}
	if !got.PrefsSeedMarkers["user-1"] {
		t.Fatal("caller map still aliases PrefsSeedMarkers")
	}
}

// The exact shape of the production race: read-modify-write of PrefsMeta from
// several goroutines, as updatePref / WriteMeta / Pull / Push all do. Run with
// -race; before the fix this trips the detector (or panics outright).
func TestConfigStore_ConcurrentMetaWrites(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store := loadConfig()
	keys := []string{"pinned_session_ids", "quick_templates", "locale_preference", "notifications_enabled"}

	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				c := store.Get()
				if c.PrefsMeta == nil {
					c.PrefsMeta = map[string]prefsMetaEntry{}
				}
				c.PrefsMeta[key] = prefsMetaEntry{UpdatedAtLocal: int64(i*100 + j), Dirty: j%2 == 0}
				_ = store.Set(c)
			}
		}(i, key)
	}
	wg.Wait()

	if len(store.Get().PrefsMeta) == 0 {
		t.Fatal("expected meta entries after concurrent writes")
	}
}

func TestTerminalAppearanceDefaults(t *testing.T) {
	var c appConfig
	if got := c.TerminalFontSizeOrDefault(); got != 13 {
		t.Errorf("font size default = %d, want 13", got)
	}
	if got := c.TerminalLineHeightOrDefault(); got != 1.0 {
		t.Errorf("line height default = %v, want 1.0", got)
	}
	if got := c.TerminalCursorStyleOrDefault(); got != "block" {
		t.Errorf("cursor style default = %q, want block", got)
	}
	if got := c.TerminalCursorBlinkOrDefault(); got != true {
		t.Errorf("cursor blink default = %v, want true", got)
	}
	if got := c.TerminalScrollbackOrDefault(); got != 5000 {
		t.Errorf("scrollback default = %d, want 5000", got)
	}
	if got := c.TerminalFontHeadOrDefault(); got != "" {
		t.Errorf("font head default = %q, want empty (system default)", got)
	}
}

func TestTerminalAppearanceClamping(t *testing.T) {
	cases := []struct {
		name string
		set  func(*appConfig)
		want func(appConfig) bool
	}{
		{"font size below floor", func(c *appConfig) { c.TerminalFontSize = 2 },
			func(c appConfig) bool { return c.TerminalFontSizeOrDefault() == 8 }},
		{"font size above ceiling", func(c *appConfig) { c.TerminalFontSize = 999 },
			func(c appConfig) bool { return c.TerminalFontSizeOrDefault() == 32 }},
		{"line height below floor", func(c *appConfig) { c.TerminalLineHeight = 0.1 },
			func(c appConfig) bool { return c.TerminalLineHeightOrDefault() == 1.0 }},
		{"line height above ceiling", func(c *appConfig) { c.TerminalLineHeight = 9 },
			func(c appConfig) bool { return c.TerminalLineHeightOrDefault() == 2.0 }},
		{"scrollback above ceiling", func(c *appConfig) { c.TerminalScrollback = 10_000_000 },
			func(c appConfig) bool { return c.TerminalScrollbackOrDefault() == 20000 }},
		{"scrollback below floor", func(c *appConfig) { c.TerminalScrollback = -5 },
			func(c appConfig) bool { return c.TerminalScrollbackOrDefault() == 5000 }},
		{"unknown cursor style falls back", func(c *appConfig) { c.TerminalCursorStyle = "spiral" },
			func(c appConfig) bool { return c.TerminalCursorStyleOrDefault() == "block" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c appConfig
			tc.set(&c)
			if !tc.want(c) {
				t.Errorf("clamp failed for %s", tc.name)
			}
		})
	}
}

// TestTerminalCursorBlinkOrDefault_ExplicitValues covers the reason
// TerminalCursorBlink is a *bool rather than bool: a plain bool's zero value
// (false) cannot distinguish "never set" from "user explicitly disabled
// blink". TestTerminalAppearanceDefaults already covers the nil case; this
// covers both explicit states so a regression to plain bool, or an accessor
// slip like `return c.TerminalCursorBlink != nil`, fails loudly.
func TestTerminalCursorBlinkOrDefault_ExplicitValues(t *testing.T) {
	off := false
	c := appConfig{TerminalCursorBlink: &off}
	if got := c.TerminalCursorBlinkOrDefault(); got != false {
		t.Errorf("explicit false = %v, want false (must not fall back to default)", got)
	}

	on := true
	c = appConfig{TerminalCursorBlink: &on}
	if got := c.TerminalCursorBlinkOrDefault(); got != true {
		t.Errorf("explicit true = %v, want true", got)
	}
}

func TestMigrateShortcutBindings(t *testing.T) {
	t.Run("mirrors bindings into the new field without clearing the legacy slot", func(t *testing.T) {
		c := appConfig{}
		c.Plugins.Shortcuts.Bindings = map[string]string{"tab.new": "Mod+KeyP"}
		changed := migrateShortcutBindings(&c)
		if !changed {
			t.Fatal("expected migration to report a change")
		}
		if c.ShortcutBindings["tab.new"] != "Mod+KeyP" {
			t.Errorf("binding not mirrored: %v", c.ShortcutBindings)
		}
		// The frontend still reads/writes the legacy slot until Task 3's swap;
		// clearing it here would blank every custom shortcut in the interim.
		if c.Plugins.Shortcuts.Bindings["tab.new"] != "Mod+KeyP" {
			t.Errorf("legacy slot must stay intact while the frontend still uses it: %v", c.Plugins.Shortcuts.Bindings)
		}
	})

	t.Run("is idempotent — a mirrored config survives a second run", func(t *testing.T) {
		c := appConfig{ShortcutBindings: map[string]string{"tab.new": "Mod+KeyP"}}
		c.Plugins.Shortcuts.Bindings = map[string]string{"tab.new": "Mod+KeyP"}
		changed := migrateShortcutBindings(&c)
		if changed {
			t.Error("expected no change when both slots already agree")
		}
		if c.ShortcutBindings["tab.new"] != "Mod+KeyP" {
			t.Error("idempotent run must not alter the mirrored value")
		}
		if c.Plugins.Shortcuts.Bindings["tab.new"] != "Mod+KeyP" {
			t.Error("idempotent run must not alter the legacy slot")
		}
	})

	t.Run("legacy slot wins while it is still live", func(t *testing.T) {
		c := appConfig{ShortcutBindings: map[string]string{"tab.new": "Mod+KeyN"}}
		c.Plugins.Shortcuts.Bindings = map[string]string{"tab.new": "Mod+KeyP"}
		changed := migrateShortcutBindings(&c)
		if !changed {
			t.Fatal("expected migration to report a change when the slots disagree")
		}
		if c.ShortcutBindings["tab.new"] != "Mod+KeyP" {
			t.Errorf("legacy slot must win (it is the only one the UI writes today), got %v", c.ShortcutBindings)
		}
		if c.Plugins.Shortcuts.Bindings["tab.new"] != "Mod+KeyP" {
			t.Error("legacy slot must remain untouched")
		}
	})

	t.Run("no-op on an empty config", func(t *testing.T) {
		c := appConfig{}
		if migrateShortcutBindings(&c) {
			t.Error("empty config needs no migration")
		}
	})
}

func TestDefaultShellExistenceCheck(t *testing.T) {
	t.Run("absolute path that does not exist falls back to auto", func(t *testing.T) {
		c := appConfig{DefaultShell: "/nonexistent/bin/fish"}
		if got := c.DefaultShellOrDefault(); got != defaultShellAuto {
			t.Errorf("got %q, want %q", got, defaultShellAuto)
		}
	})

	t.Run("absolute path that exists is used", func(t *testing.T) {
		c := appConfig{DefaultShell: "/bin/sh"}
		if got := c.DefaultShellOrDefault(); got != "/bin/sh" {
			t.Errorf("got %q, want /bin/sh", got)
		}
	})

	t.Run("non-absolute value is passed through unchecked", func(t *testing.T) {
		c := appConfig{DefaultShell: "fish"}
		if got := c.DefaultShellOrDefault(); got != "fish" {
			t.Errorf("got %q, want fish — PATH-resolved shells must not be validated", got)
		}
	})
}
