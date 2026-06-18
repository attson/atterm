package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/desktop/hookinstall"
)

// TestHookInstall_StartupInstallsByDefault verifies that on a fresh
// home, calling hookinstall.Install (the same call startup makes
// when enabled=true) materializes the symlink + writes settings.json.
//
// We test the hookinstall surface directly rather than driving
// app.startup, because startup pulls in the relay host, configStore,
// logging, etc. — overkill for asserting "install happened".
func TestHookInstall_DefaultEnabledIntegration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	c := appConfig{}
	if !c.HookAutoInstallEnabledOrDefault() {
		t.Fatal("default should be true")
	}

	if err := hookinstall.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	settings := filepath.Join(tmp, ".claude", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Errorf("settings.json not created: %v", err)
	}
	link := filepath.Join(tmp, ".atterm", "bin", "atterm-hook")
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink not created: %v", err)
	}
}
