package main

import (
	"encoding/json"
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
