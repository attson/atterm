package hookinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeCodexTrustConfig(t *testing.T, home string) {
	t.Helper()
	raw, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := readCodexHooksDocument(raw)
	var config strings.Builder
	for _, event := range codexHookEvents {
		trustEvent := codexTrustEventNames[event]
		for entryIdx, entry := range doc.Hooks[event] {
			if !isAttermCodexEntry(entry) {
				continue
			}
			for hookIdx := range entry.Hooks {
				key := fmt.Sprintf("%s:%s:%d:%d", codexHooksPath(home), trustEvent, entryIdx, hookIdx)
				fmt.Fprintf(&config, "[hooks.state.%s]\ntrusted_hash = \"test-hash\"\n\n", strconv.Quote(key))
			}
		}
	}
	if err := os.WriteFile(codexConfigPath(home), []byte(config.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_Healthy(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	writeCodexTrustConfig(t, home)
	s := checkAt(home, true /* enabled */)
	if !s.BinaryOK {
		t.Errorf("BinaryOK = false; LastError=%q", s.LastError)
	}
	if !s.SettingsOK {
		t.Errorf("SettingsOK = false; LastError=%q", s.LastError)
	}
	if !s.CodexSettingsOK || !s.CodexTrustOK {
		t.Errorf("Codex health = settings:%t trust:%t; LastError=%q", s.CodexSettingsOK, s.CodexTrustOK, s.LastError)
	}
	if !s.Healthy() {
		t.Errorf("Healthy() = false; state=%+v", s)
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
	if s.BinaryPath == "" || s.SettingsPath == "" || s.CodexSettingsPath == "" || s.CodexTrustPath == "" {
		t.Errorf("paths empty: %+v", s)
	}
}

func TestCheck_CodexEventMissing(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := readCodexHooksDocument(raw)
	delete(doc.Hooks, "SessionStart")
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexHooksPath(home), out, 0o600); err != nil {
		t.Fatal(err)
	}

	s := checkAt(home, true)
	if s.CodexSettingsOK {
		t.Fatal("CodexSettingsOK = true; want false")
	}
	if !s.RepairNeeded() {
		t.Fatal("RepairNeeded() = false; missing hook event is repairable")
	}
	if !strings.Contains(s.LastError, "SessionStart") {
		t.Errorf("LastError = %q; want SessionStart", s.LastError)
	}
}

func TestCheck_CodexTrustMissingDoesNotRequestRepair(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}

	s := checkAt(home, true)
	if !s.CodexSettingsOK {
		t.Fatalf("CodexSettingsOK = false; LastError=%q", s.LastError)
	}
	if s.CodexTrustOK {
		t.Fatal("CodexTrustOK = true; want false")
	}
	if s.RepairNeeded() {
		t.Fatal("RepairNeeded() = true; install cannot approve Codex trust")
	}
	if !strings.Contains(s.LastError, "trust") {
		t.Errorf("LastError = %q; want trust guidance", s.LastError)
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

func TestCheck_PreToolUseMissing(t *testing.T) {
	home := t.TempDir()
	if err := installAt(home); err != nil {
		t.Fatal(err)
	}
	// Manually strip the atterm-owned PreToolUse entry. Settings should
	// then fail health-check with a PreToolUse-specific reason — Install
	// is what restores it on next launch.
	cfg, _ := readClaudeSettings(home)
	cfg.Hooks.PreToolUse = nil
	writeClaudeSettings(home, cfg)

	s := checkAt(home, true)
	if s.SettingsOK {
		t.Errorf("SettingsOK = true; want false when PreToolUse entry is missing")
	}
	if !strings.Contains(s.LastError, "PreToolUse") {
		t.Errorf("LastError should mention PreToolUse; got %q", s.LastError)
	}
}

// Each owned streaming slot independently breaks the SettingsOK gate when
// stripped — without this guard, a half-installed settings.json would render
// "healthy" even though the anchor card body never fills in.
func TestCheck_StreamingHookSlotMissing(t *testing.T) {
	cases := []struct {
		name  string
		strip func(*ClaudeHooks)
	}{
		{"UserPromptSubmit", func(h *ClaudeHooks) { h.UserPromptSubmit = nil }},
		{"Stop", func(h *ClaudeHooks) { h.Stop = nil }},
		{"PostToolUse", func(h *ClaudeHooks) { h.PostToolUse = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			if err := installAt(home); err != nil {
				t.Fatal(err)
			}
			cfg, _ := readClaudeSettings(home)
			c.strip(&cfg.Hooks)
			writeClaudeSettings(home, cfg)

			s := checkAt(home, true)
			if s.SettingsOK {
				t.Errorf("SettingsOK = true; want false when %s is missing", c.name)
			}
			if !strings.Contains(s.LastError, c.name) {
				t.Errorf("LastError should mention %s; got %q", c.name, s.LastError)
			}
		})
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
