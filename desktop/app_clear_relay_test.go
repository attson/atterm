package main

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

// seededRelayApp returns an App with the cfgStore pre-populated with every
// Relay* field set to a non-zero value plus two unrelated fields (theme,
// shell) that must survive the clear.
func seededRelayApp(t *testing.T) *App {
	t.Helper()
	a := newRelayTestApp(t)
	// Use safekeyring's file-backed test store so keychain calls don't hit
	// the real OS keychain.
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
	if err := a.cfgStore.Set(appConfig{
		RelayURL:              "wss://r.example.com",
		RelaySessionToken:     "atk_test",
		RelaySessionExpiresAt: 1_700_000_000,
		RelayLastEmail:        "u@example.com",
		RelaySessionUserID:    "user-abc",
		AllowInsecureRelay:    true,
		DisableE2EE:           true,
		RemotePermission:      "full",
		RelayPaused:           true,
		TerminalTheme:         "solarized",
		DefaultShell:          "/bin/zsh",
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	return a
}

func TestClearRelayConfig_ZerosAllRelayFields(t *testing.T) {
	a := seededRelayApp(t)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "" || cfg.RelaySessionToken != "" || cfg.RelaySessionExpiresAt != 0 ||
		cfg.RelayLastEmail != "" || cfg.RelaySessionUserID != "" ||
		cfg.AllowInsecureRelay || cfg.DisableE2EE || cfg.RemotePermission != "" || cfg.RelayPaused {
		t.Fatalf("relay fields not zeroed: %+v", cfg)
	}
	if cfg.TerminalTheme != "solarized" || cfg.DefaultShell != "/bin/zsh" {
		t.Fatalf("unrelated fields mutated: theme=%q shell=%q", cfg.TerminalTheme, cfg.DefaultShell)
	}
}

func TestClearRelayConfig_DeletesPasswordKeychainSlot(t *testing.T) {
	a := seededRelayApp(t)
	if err := saveRelayPassword("wss://r.example.com", "u@example.com", "hunter2"); err != nil {
		t.Fatalf("saveRelayPassword: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	got, err := loadRelayPassword("wss://r.example.com", "u@example.com")
	if err != nil {
		t.Fatalf("loadRelayPassword: %v", err)
	}
	if got != "" {
		t.Fatalf("password slot not cleared: %q", got)
	}
}

func TestClearRelayConfig_DeletesAccountKeyKeychainSlot(t *testing.T) {
	a := seededRelayApp(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := saveAccountKey("wss://r.example.com", "user-abc", key); err != nil {
		t.Fatalf("saveAccountKey: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	got, err := loadAccountKey("wss://r.example.com", "user-abc")
	if err != nil {
		t.Fatalf("loadAccountKey: %v", err)
	}
	if got != nil {
		t.Fatalf("account_key slot not cleared: got %d bytes", len(got))
	}
}

func TestClearRelayConfig_ZerosInMemoryAccountKey(t *testing.T) {
	a := seededRelayApp(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	a.setAccountKeyInMemory(key)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	if got := a.accountKeySnapshot(); len(got) != 0 {
		t.Fatalf("in-memory account_key not zeroed: %d bytes", len(got))
	}
}

func TestClearRelayConfig_TolerantOfMissingKeychainSlots(t *testing.T) {
	a := seededRelayApp(t)
	// No slot pre-seed; both keychain deletes must silently succeed.
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
}

func TestClearRelayConfig_Idempotent(t *testing.T) {
	a := seededRelayApp(t)
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig #1: %v", err)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig #2 (idempotent): %v", err)
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "" {
		t.Fatalf("second clear mutated URL: %q", cfg.RelayURL)
	}
}

func TestClearRelayConfig_EmitsEvents(t *testing.T) {
	a := seededRelayApp(t)
	var got []string
	a.eventsEmitter = func(_ context.Context, name string, _ ...interface{}) {
		got = append(got, name)
	}
	if err := a.ClearRelayConfig(); err != nil {
		t.Fatalf("ClearRelayConfig: %v", err)
	}
	want := map[string]bool{
		"account-key:changed":  false, // side-effect of setAccountKey(nil)
		"relay-config-changed": false,
		"e2ee-mode-changed":    false,
		"relay:auth-info":      false,
	}
	for _, n := range got {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("event %q not emitted; got %v", n, got)
		}
	}
}
