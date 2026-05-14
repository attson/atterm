package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newAppWithTempCfg(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	// Ensure configPath() resolves under our temp dir.
	if err := os.MkdirAll(filepath.Join(dir, "atterm"), 0o755); err != nil {
		t.Fatalf("mkdir cfg: %v", err)
	}
	return &App{cfgStore: loadConfig()}
}

func TestGetShellIntegrationEnabledDefaultsTrue(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetShellIntegrationEnabled(); !got {
		t.Fatalf("GetShellIntegrationEnabled() = false; want true (default)")
	}
}

func TestSetShellIntegrationEnabledPersists(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetShellIntegrationEnabled(false); err != nil {
		t.Fatalf("SetShellIntegrationEnabled(false): %v", err)
	}
	if got := a.GetShellIntegrationEnabled(); got {
		t.Fatalf("after Set(false), Get() = true")
	}
}

func TestGetCommandNotifyThresholdSecondsDefaultsTo10(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetCommandNotifyThresholdSeconds(); got != 10 {
		t.Fatalf("GetCommandNotifyThresholdSeconds() = %d; want 10", got)
	}
}

func TestSetCommandNotifyThresholdSecondsPersistsAndClamps(t *testing.T) {
	a := newAppWithTempCfg(t)
	if err := a.SetCommandNotifyThresholdSeconds(45); err != nil {
		t.Fatalf("SetCommandNotifyThresholdSeconds(45): %v", err)
	}
	if got := a.GetCommandNotifyThresholdSeconds(); got != 45 {
		t.Fatalf("after Set(45), Get() = %d; want 45", got)
	}
	if err := a.SetCommandNotifyThresholdSeconds(9999); err != nil {
		t.Fatalf("SetCommandNotifyThresholdSeconds(9999): %v", err)
	}
	if got := a.GetCommandNotifyThresholdSeconds(); got != 600 {
		t.Fatalf("after Set(9999), Get() = %d; want 600 (clamped)", got)
	}
}
