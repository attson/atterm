package hookinstall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"time"
)

// State is the read-only health snapshot returned by Check. Renders
// directly to the Settings · Feishu status row.
type State struct {
	Enabled       bool      `json:"enabled"`
	BinaryPath    string    `json:"binary_path"`
	BinaryOK      bool      `json:"binary_ok"`
	BinaryVersion string    `json:"binary_version"`
	SettingsPath  string    `json:"settings_path"`
	SettingsOK    bool      `json:"settings_ok"`
	LastError     string    `json:"last_error"`
	LastCheck     time.Time `json:"last_check"`
}

// Healthy returns true when both surfaces are OK and auto-install is on.
func (s State) Healthy() bool { return s.Enabled && s.BinaryOK && s.SettingsOK }

// Check is a pure read of the current on-disk state. Does NOT mutate
// anything. Cheap to call: a handful of file stats + one JSON parse.
func Check(_ context.Context, enabled bool) State {
	if runtime.GOOS == "windows" {
		return State{
			Enabled:      enabled,
			BinaryPath:   "",
			SettingsPath: "",
			LastError:    "auto-install unsupported on Windows",
			LastCheck:    time.Now(),
		}
	}
	return checkAt(homeOrDie(), enabled)
}

func checkAt(home string, enabled bool) State {
	s := State{
		Enabled:      enabled,
		BinaryPath:   attermHookSymlink(home),
		SettingsPath: claudeSettingsPath(home),
		LastCheck:    time.Now(),
	}
	if !enabled {
		return s
	}

	binOK, binVer, binErr := checkBinary(home)
	s.BinaryOK = binOK
	s.BinaryVersion = binVer

	setOK, setErr := checkSettings(home, s.BinaryPath)
	s.SettingsOK = setOK

	switch {
	case binErr != "":
		s.LastError = binErr
	case setErr != "":
		s.LastError = setErr
	}
	return s
}

func checkBinary(home string) (ok bool, version string, errStr string) {
	link := attermHookSymlink(home)
	target, err := os.Readlink(link)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, "", "hook binary symlink missing"
		}
		return false, "", fmt.Sprintf("read symlink: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return false, "", fmt.Sprintf("symlink target missing: %v", err)
	}
	if info.IsDir() {
		return false, "", "symlink target is a directory"
	}
	if info.Mode()&0o111 == 0 {
		return false, "", "hook binary not executable"
	}
	// Extract version from filename suffix: atterm-hook-<sha8>
	name := info.Name()
	const prefix = "atterm-hook-"
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		version = name[len(prefix):]
	}
	return true, version, ""
}

func checkSettings(home string, wantCommand string) (ok bool, errStr string) {
	cfg, err := readClaudeSettings(home)
	if err != nil {
		return false, fmt.Sprintf("Claude settings.json invalid JSON: %v", err)
	}
	var attermEntries []HookEntry
	for _, e := range cfg.Hooks.Notification {
		if isAttermHookCommand(e) {
			attermEntries = append(attermEntries, e)
		}
	}
	if len(attermEntries) < 2 {
		return false, "Notification hook entries missing or incomplete"
	}
	for _, e := range attermEntries {
		if e.Command != wantCommand {
			return false, "Notification entry points at stale binary path"
		}
		if _, err := os.Stat(e.Command); err != nil {
			return false, "Notification command path missing on disk"
		}
	}
	return true, ""
}
