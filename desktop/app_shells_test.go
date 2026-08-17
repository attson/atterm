package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListShellsSkipsInvalidEnvShell(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-shell")
	t.Setenv("SHELL", missing)

	got := NewApp(nil, nil).ListShells()
	for _, shell := range got {
		if shell == missing {
			t.Fatalf("ListShells returned invalid SHELL %q in %v", missing, got)
		}
	}
}

func TestListShellsUsesConfiguredDefaultShellFirst(t *testing.T) {
	custom := writeExecutableShell(t)
	t.Setenv("SHELL", "")

	got := newThemeTestApp(t, appConfig{DefaultShell: custom}).ListShells()
	if len(got) == 0 || got[0] != custom {
		t.Fatalf("ListShells()[0] = %q; want configured shell %q (all=%v)", first(got), custom, got)
	}
}

func TestDefaultShellConfigUsesAutoForEmptyOrUnknown(t *testing.T) {
	tests := []appConfig{
		{},
		{DefaultShell: ""},
		{DefaultShell: "auto"},
		{DefaultShell: " AUTO "},
	}
	for _, cfg := range tests {
		if got := cfg.DefaultShellOrDefault(); got != defaultShellAuto {
			t.Fatalf("DefaultShellOrDefault(%q) = %q; want %q", cfg.DefaultShell, got, defaultShellAuto)
		}
	}
}

func TestSetDefaultShellPersistsAndPreservesConfig(t *testing.T) {
	custom := writeExecutableShell(t)
	a := newThemeTestApp(t, appConfig{
		RelayURL:          "wss://relay.example.com",
		RelaySessionToken: "secret-token",
		TerminalTheme:     terminalThemeNord,
	})

	if err := a.SetDefaultShell(custom); err != nil {
		t.Fatalf("SetDefaultShell() error = %v", err)
	}
	if got := a.GetDefaultShell(); got != custom {
		t.Fatalf("GetDefaultShell() = %q; want %q", got, custom)
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL != "wss://relay.example.com" || cfg.RelaySessionToken != "secret-token" || cfg.TerminalTheme != terminalThemeNord {
		t.Fatalf("SetDefaultShell changed unrelated config: %#v", cfg)
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		t.Fatalf("ReadFile(configPath()) error = %v", err)
	}
	if !strings.Contains(string(data), `"default_shell": "`+strings.ReplaceAll(custom, `\`, `\\`)+`"`) {
		t.Fatalf("persisted config missing default shell: %s", data)
	}
}

func TestSetDefaultShellRejectsMissingShell(t *testing.T) {
	a := newThemeTestApp(t, appConfig{})
	missing := filepath.Join(t.TempDir(), "missing-shell")
	if err := a.SetDefaultShell(missing); err == nil {
		t.Fatalf("SetDefaultShell(%q) error = nil; want error", missing)
	}
	if got := a.GetDefaultShell(); got != defaultShellAuto {
		t.Fatalf("GetDefaultShell() = %q; want %q", got, defaultShellAuto)
	}
}

func writeExecutableShell(t *testing.T) string {
	t.Helper()
	name := "custom-shell"
	body := "#!/bin/sh\n"
	if runtime.GOOS == "windows" {
		name = "custom-shell.cmd"
		body = "@echo off\r\n"
	}
	custom := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(custom, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile(custom shell): %v", err)
	}
	return custom
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
