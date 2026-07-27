package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

// newRelayTestApp creates a minimal App wired for relay/uplink tests.
// A temp dir is used so cfgStore.Set disk writes succeed.
func newRelayTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))
	a := &App{
		cfgStore: &configStore{},
		ctx:      context.Background(),
	}
	return a
}

// newTestApp creates a minimal App with prefsSync initialized for
// preference sync tests.
func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("LocalAppData", filepath.Join(root, "local"))
	a := &App{
		cfgStore: &configStore{},
		ctx:      context.Background(),
	}
	adapter := newAppConfigAdapter(a.cfgStore)
	relayClient := newHTTPRelayClient(a.cfgStore)
	a.prefsSync = prefssync.NewEngine(adapter, relayClient)
	return a
}

// TestSetUplinkPaused_TogglesWithoutWipingConfig verifies the
// "disconnect erases config" fix: pausing/unpausing via SetUplinkPaused
// must not touch the persisted URL/token, only the pause flag.
func TestSetUplinkPaused_TogglesWithoutWipingConfig(t *testing.T) {
	a := newRelayTestApp(t)

	// Seed the store with a relay config (URL/token).
	if err := a.SetRelayConfig(RelayConfig{
		URL:              "wss://x",
		Token:            "atk_test",
		RemotePermission: "full",
	}); err != nil {
		t.Fatalf("SetRelayConfig: %v", err)
	}

	// After SetRelayConfig the uplink struct should be non-nil (Connected).
	rc := a.GetRelayConfig()
	if rc.URL != "wss://x" {
		t.Fatalf("URL = %q; want %q", rc.URL, "wss://x")
	}
	if rc.Token != "atk_test" {
		t.Fatalf("Token = %q; want %q", rc.Token, "atk_test")
	}

	// Pause — uplink must stop (Connected=false) but URL/token must survive.
	if err := a.SetUplinkPaused(true); err != nil {
		t.Fatalf("SetUplinkPaused(true): %v", err)
	}
	rc = a.GetRelayConfig()
	if rc.URL != "wss://x" {
		t.Fatalf("after pause: URL = %q; want %q", rc.URL, "wss://x")
	}
	if rc.Token != "atk_test" {
		t.Fatalf("after pause: Token = %q; want %q", rc.Token, "atk_test")
	}
	if rc.Connected {
		t.Fatal("after pause: Connected = true; want false")
	}
	if !rc.Paused {
		t.Fatal("after pause: Paused = false; want true")
	}

	// Unpause — uplink restarts (Connected=true), URL/token still intact.
	if err := a.SetUplinkPaused(false); err != nil {
		t.Fatalf("SetUplinkPaused(false): %v", err)
	}
	rc = a.GetRelayConfig()
	if rc.URL != "wss://x" {
		t.Fatalf("after unpause: URL = %q; want %q", rc.URL, "wss://x")
	}
	if rc.Token != "atk_test" {
		t.Fatalf("after unpause: Token = %q; want %q", rc.Token, "atk_test")
	}
	if !rc.Connected {
		t.Fatal("after unpause: Connected = false; want true")
	}
	if rc.Paused {
		t.Fatal("after unpause: Paused = true; want false")
	}
}

func TestAppLocalePreference(t *testing.T) {
	a := newRelayTestApp(t)
	initial := appConfig{
		RelayURL:         "wss://relay.example",
		RelaySessionToken:       "atk_locale_test",
		RemotePermission: "control",
		RelayPaused:      true,
		TerminalTheme:    terminalThemeNord,
	}
	if err := a.cfgStore.Set(initial); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	for _, pref := range []string{localePreferenceSystem, localePreferenceEnglish, localePreferenceChineseSimplified} {
		if err := a.SetLocalePreference(pref); err != nil {
			t.Fatalf("SetLocalePreference(%q): %v", pref, err)
		}
		if got := a.GetLocalePreference(); got != pref {
			t.Fatalf("GetLocalePreference() = %q, want %q", got, pref)
		}
		cfg := a.cfgStore.Get()
		if cfg.RelayURL != initial.RelayURL || cfg.RelaySessionToken != initial.RelaySessionToken || cfg.RemotePermission != initial.RemotePermission || cfg.RelayPaused != initial.RelayPaused || cfg.TerminalTheme != initial.TerminalTheme {
			t.Fatalf("SetLocalePreference(%q) changed unrelated config: %+v", pref, cfg)
		}
	}

	beforeInvalid := a.cfgStore.Get()
	if err := a.SetLocalePreference("fr"); err == nil {
		t.Fatal("SetLocalePreference(\"fr\") error = nil, want error")
	}
	afterInvalid := a.cfgStore.Get()
	if !reflect.DeepEqual(afterInvalid, beforeInvalid) {
		t.Fatalf("invalid locale preference changed config: got %+v, want %+v", afterInvalid, beforeInvalid)
	}
}

func TestBeforeCloseEmitsAndPreventsByDefault(t *testing.T) {
	a := &App{}
	emitted := 0
	prevent := a.beforeClose(context.Background(), func() { emitted++ })
	if !prevent {
		t.Fatalf("prevent = false; want true (first close should be intercepted)")
	}
	if emitted != 1 {
		t.Fatalf("emitted = %d; want 1", emitted)
	}
}

func TestBeforeCloseAllowsQuitWhenApproved(t *testing.T) {
	a := &App{}
	a.quitApproved.Store(true)
	emitted := 0
	prevent := a.beforeClose(context.Background(), func() { emitted++ })
	if prevent {
		t.Fatalf("prevent = true; want false (approved quits must not be intercepted)")
	}
	if emitted != 0 {
		t.Fatalf("emitted = %d; want 0 (no event should fire when approved)", emitted)
	}
}

func TestPinnedSessionIds_EmptyDefault(t *testing.T) {
	a := newRelayTestApp(t)
	got := a.GetPinnedSessionIds()
	if got == nil {
		t.Fatal("GetPinnedSessionIds() = nil; want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("GetPinnedSessionIds() = %v; want []", got)
	}
}

func TestPinnedSessionIds_RoundTrip(t *testing.T) {
	a := newRelayTestApp(t)
	ids := []string{"aaa", "bbb", "ccc"}
	if err := a.SetPinnedSessionIds(ids); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	got := a.GetPinnedSessionIds()
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("GetPinnedSessionIds() = %v; want %v", got, ids)
	}
}

func TestPinnedSessionIds_DedupeDropsEmpty(t *testing.T) {
	a := newRelayTestApp(t)
	input := []string{"aaa", "", "bbb", "aaa", "ccc", ""}
	if err := a.SetPinnedSessionIds(input); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	got := a.GetPinnedSessionIds()
	want := []string{"aaa", "bbb", "ccc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetPinnedSessionIds() = %v; want %v", got, want)
	}
}

func TestPinnedSessionIds_NilClears(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.SetPinnedSessionIds([]string{"aaa"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := a.SetPinnedSessionIds(nil); err != nil {
		t.Fatalf("SetPinnedSessionIds(nil): %v", err)
	}
	got := a.GetPinnedSessionIds()
	if len(got) != 0 {
		t.Fatalf("after clear: got %v; want []", got)
	}
}

func TestPinnedSessionIds_MarksPrefDirty(t *testing.T) {
	a := newTestApp(t)
	// After Set, prefsMeta[pinned_session_ids].Dirty must be true.
	if err := a.SetPinnedSessionIds([]string{"sid-x"}); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	m := a.cfgStore.Get().PrefsMeta["pinned_session_ids"]
	if !m.Dirty {
		t.Fatalf("prefs_meta.pinned_session_ids.dirty = false; want true")
	}
	if m.UpdatedAtLocal <= 0 {
		t.Fatalf("UpdatedAtLocal = %d; want > 0", m.UpdatedAtLocal)
	}
}

func TestIsPrefCustomized_PinnedSessionIds(t *testing.T) {
	fn := isPrefCustomized(appConfig{PinnedSessionIDs: []string{"sid"}})
	if !fn("pinned_session_ids") {
		t.Fatal("expected true when list has entries")
	}
	empty := isPrefCustomized(appConfig{})
	if empty("pinned_session_ids") {
		t.Fatal("expected false when list empty")
	}
}
