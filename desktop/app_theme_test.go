package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func newThemeTestApp(t *testing.T, cfg appConfig) *App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	// Keep Linux CI and developer machines away from real user config dirs.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	return &App{cfgStore: &configStore{cfg: cfg}}
}

func TestGetTerminalThemeReturnsDefault(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	if got := a.GetTerminalTheme(); got != terminalThemeClassic {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeClassic)
	}
}

func TestSetTerminalThemePersistsAndPreservesConfig(t *testing.T) {
	autoCheck := false
	a := newThemeTestApp(t, appConfig{
		RelayURL:           "wss://relay.example.com",
		RelaySessionToken:         "secret-token",
		AllowInsecureRelay: true,
		RemotePermission:   proto.RemotePermissionControl,
		AutoCheckUpdates:   &autoCheck,
		LastCheckAt:        123,
		SkipVersion:        "v9.9.9",
		TerminalTheme:      terminalThemeClassic,
	})

	if err := a.SetTerminalTheme(terminalThemeNord); err != nil {
		t.Fatalf("SetTerminalTheme() error = %v", err)
	}
	if got := a.GetTerminalTheme(); got != terminalThemeNord {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeNord)
	}

	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "wss://relay.example.com" ||
		cfg.RelaySessionToken != "secret-token" ||
		!cfg.AllowInsecureRelay ||
		cfg.RemotePermission != proto.RemotePermissionControl ||
		cfg.AutoCheckUpdates != &autoCheck ||
		cfg.LastCheckAt != 123 ||
		cfg.SkipVersion != "v9.9.9" {
		t.Fatalf("SetTerminalTheme changed unrelated config: %#v", cfg)
	}
	if cfg.TerminalTheme != terminalThemeNord {
		t.Fatalf("TerminalTheme = %q; want %q", cfg.TerminalTheme, terminalThemeNord)
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("ReadFile(configPath()) error = %v", err)
	}
	if !strings.Contains(string(data), `"terminal_theme": "nord"`) {
		t.Fatalf("persisted config missing terminal theme: %s", data)
	}
}

func TestSetTerminalThemeRejectsUnknownTheme(t *testing.T) {
	a := newThemeTestApp(t, appConfig{TerminalTheme: terminalThemeClassic})

	if err := a.SetTerminalTheme("gruvbox"); err == nil {
		t.Fatalf("SetTerminalTheme(unknown) error = nil; want error")
	}
	if got := a.GetTerminalTheme(); got != terminalThemeClassic {
		t.Fatalf("GetTerminalTheme() = %q; want %q", got, terminalThemeClassic)
	}
}
