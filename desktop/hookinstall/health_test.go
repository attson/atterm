package hookinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheck_Healthy(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	s := checkAt(home, true /* enabled */)
	if !s.BinaryOK {
		t.Errorf("BinaryOK = false; LastError=%q", s.LastError)
	}
	if !s.SettingsOK {
		t.Errorf("SettingsOK = false; LastError=%q", s.LastError)
	}
	if s.LastError != "" {
		t.Errorf("LastError = %q; want empty", s.LastError)
	}
	if s.BinaryVersion != embeddedHash {
		t.Errorf("BinaryVersion = %q; want %q", s.BinaryVersion, embeddedHash)
	}
}

func TestCheck_DisabledShortCircuits(t *testing.T) {
	home := t.TempDir()
	s := checkAt(home, false)
	if s.Enabled {
		t.Errorf("Enabled = true; want false")
	}
	// Disabled state still reports correct paths so UI can show them.
	if s.BinaryPath == "" || s.SettingsPath == "" {
		t.Errorf("paths empty: %+v", s)
	}
}

func TestCheck_BinaryMissing(t *testing.T) {
	home := t.TempDir()
	// Don't install. Just check.
	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; expected false")
	}
	if !strings.Contains(strings.ToLower(s.LastError), "binary") {
		t.Errorf("LastError should mention binary; got %q", s.LastError)
	}
}

func TestCheck_SymlinkPointsAtNonExistentFile(t *testing.T) {
	home := t.TempDir()
	bin := attermBinDir(home)
	os.MkdirAll(bin, 0o755)
	os.Symlink(filepath.Join(bin, "nope"), attermHookSymlink(home))

	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; want false")
	}
}

func TestCheck_BinaryNotExecutable(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(attermBinDir(home), "atterm-hook-"+embeddedHash)
	os.Chmod(target, 0o644)

	s := checkAt(home, true)
	if s.BinaryOK {
		t.Errorf("BinaryOK = true; want false on non-executable target")
	}
}

func TestCheck_SettingsMissingMarkerEntries(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	// Write a settings.json that has zero atterm entries.
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home),
		[]byte(`{"hooks":{"Notification":[{"matcher":"","hooks":[{"type":"command","command":"/u/y"}]}]}}`),
		0o644)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false")
	}
	if !strings.Contains(s.LastError, "Notification") {
		t.Errorf("LastError should mention Notification; got %q", s.LastError)
	}
}

func TestCheck_SettingsCommandPathStale(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	// Manually mutate settings.json so one atterm entry points at a
	// stale (different) path.
	cfg, _ := readClaudeSettings(home)
	cfg.Hooks.Notification[0].Hooks[0].Command = "/tmp/wrong/.atterm/bin/atterm-hook"
	writeClaudeSettings(home, cfg)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false on stale command path")
	}
}

func TestCheck_InvalidJSON(t *testing.T) {
	home := t.TempDir()
	if _, _, err := ensureBinary(home); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(claudeDir(home), 0o700)
	os.WriteFile(claudeSettingsPath(home), []byte("garbage"), 0o644)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false")
	}
	if !strings.Contains(strings.ToLower(s.LastError), "json") {
		t.Errorf("LastError should mention JSON; got %q", s.LastError)
	}
}
