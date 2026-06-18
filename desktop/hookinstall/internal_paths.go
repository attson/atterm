package hookinstall

import (
	"os"
	"path/filepath"
)

// homeOrDie panics if os.UserHomeDir returns an error. The desktop main
// process cannot run without a home directory, so we don't try to
// recover. Tests should not call this — they pass home explicitly.
func homeOrDie() string {
	h, err := os.UserHomeDir()
	if err != nil {
		panic("hookinstall: cannot determine home: " + err.Error())
	}
	return h
}

// attermBinDir returns ~/.atterm/bin given a home root.
func attermBinDir(home string) string {
	return filepath.Join(home, ".atterm", "bin")
}

// attermHookSymlink returns ~/.atterm/bin/atterm-hook given a home root.
func attermHookSymlink(home string) string {
	return filepath.Join(attermBinDir(home), "atterm-hook")
}

// claudeSettingsPath returns ~/.claude/settings.json given a home root.
func claudeSettingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// claudeDir returns ~/.claude given a home root.
func claudeDir(home string) string {
	return filepath.Join(home, ".claude")
}
