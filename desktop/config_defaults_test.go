package main

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateConfigDir points the OS config-dir lookup at a temp directory so a
// test sees a machine with no config.json — i.e. a genuine first run.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)                                  // darwin, linux fallback
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "cfg")) // linux
	t.Setenv("AppData", filepath.Join(tmp, "AppData"))     // windows
	return tmp
}

// Regression: on the very first run there is no config.json, os.ReadFile
// fails, and loadConfig used to return early — skipping applyDefaults and
// handing the frontend a zero-valued plugin block. Because
// ValidatePluginConfig rejects panelWidthPx=0, enabling ANY plugin then
// failed with "fileExplorer.panelWidthPx out of bounds [240, 2000]" until the
// app was restarted.
func TestLoadConfigAppliesPluginDefaultsWithoutConfigFile(t *testing.T) {
	isolateConfigDir(t)

	// Precondition: the file really is absent, so this exercises the early
	// return rather than a stale config left by another test.
	if p := configPath(); p != "" {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected no config file at %s (err=%v)", p, err)
		}
	}

	s := loadConfig()
	got := s.Get().Plugins

	if err := ValidatePluginConfig(got); err != nil {
		t.Fatalf("first-run plugin config must be valid, got %v", err)
	}
	if got.FileExplorer.PanelWidthPx == 0 {
		t.Fatal("fileExplorer.panelWidthPx left at zero — applyDefaults did not run")
	}
	if got.Pet.WindowX != -1 || got.Pet.WindowY != -1 {
		t.Fatalf("pet position sentinel not applied: (%d,%d)", got.Pet.WindowX, got.Pet.WindowY)
	}
}

// The same guarantee has to hold through a round trip: whatever the frontend
// receives on first run must come back accepted, which is the actual user
// action that was failing (toggling a plugin in Settings).
func TestFirstRunPluginToggleRoundTrips(t *testing.T) {
	isolateConfigDir(t)

	s := loadConfig()
	next := s.Get().Plugins
	next.Pet.Enabled = true // what SettingsPlugins sends when the box is ticked

	if err := ValidatePluginConfig(next); err != nil {
		t.Fatalf("enabling a plugin on first run must be accepted, got %v", err)
	}
}
