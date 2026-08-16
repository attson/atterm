package hookinstall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"
)

// State is the read-only health snapshot returned by Check. Renders
// directly to the Settings · Feishu status row.
type State struct {
	Enabled       bool   `json:"enabled"`
	BinaryPath    string `json:"binary_path"`
	BinaryOK      bool   `json:"binary_ok"`
	BinaryVersion string `json:"binary_version"`
	SettingsPath  string `json:"settings_path"`
	SettingsOK    bool   `json:"settings_ok"`
	LastError     string `json:"last_error"`
	// LastCheck is an RFC3339 timestamp. It is a string rather than a
	// time.Time so the Wails binding generator can map it to a TS `string`
	// (time.Time has no TS counterpart and is emitted as `any`).
	LastCheck string `json:"last_check"`
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
			LastCheck:    time.Now().Format(time.RFC3339),
		}
	}
	return checkAt(homeOrDie(), enabled)
}

func checkAt(home string, enabled bool) State {
	s := State{
		Enabled:      enabled,
		BinaryPath:   attermHookSymlink(home),
		SettingsPath: claudeSettingsPath(home),
		LastCheck:    time.Now().Format(time.RFC3339),
	}
	if !enabled {
		return s
	}

	binOK, binVer, binErr := checkBinary(home)
	s.BinaryOK = binOK
	s.BinaryVersion = binVer

	// Compare against the same string the installer writes, agent flag and all —
	// the health check and the writer disagreeing would report a permanently
	// unhealthy install that repairing cannot fix.
	setOK, setErr := checkSettings(home, attermHookCommand(s.BinaryPath, "claude-code"))
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
	for _, s := range ownedSlots {
		entries := *s.field(&cfg.Hooks)
		if ok, msg := checkHookKind(s.name, entries, wantCommand); !ok {
			return false, msg
		}
	}
	return true, ""
}

func checkHookKind(kind string, entries []HookEntry, wantCommand string) (ok bool, msg string) {
	var attermEntries []HookEntry
	for _, e := range entries {
		if isAttermHookCommand(e) {
			attermEntries = append(attermEntries, e)
		}
	}
	if len(attermEntries) == 0 {
		return false, kind + " hook entry missing"
	}
	for _, e := range attermEntries {
		for _, h := range e.Hooks {
			if h.Command != wantCommand {
				return false, kind + " entry points at stale binary path"
			}
			if _, err := os.Stat(hookCommandBinary(h.Command)); err != nil {
				return false, kind + " command path missing on disk"
			}
		}
	}
	return true, ""
}

// hookCommandBinary strips the flags off a hook command line so the path can be
// stat'ed. The command is ours and always "<binary> --agent <kind>", but split
// on the flag rather than on whitespace: the binary lives under the user's home
// directory, which is allowed to contain spaces.
func hookCommandBinary(cmd string) string {
	if i := strings.Index(cmd, " --"); i >= 0 {
		return cmd[:i]
	}
	return cmd
}
