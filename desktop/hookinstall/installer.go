package hookinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"runtime"
)

// Install ensures the binary is materialized and ~/.claude/settings.json
// contains atterm's Notification entries. Idempotent. On Windows this
// is a no-op (symlinks need admin); Check will surface the reason.
func Install(_ context.Context) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return installAt(homeOrDie())
}

// Uninstall removes the atterm-managed entries from settings.json and
// the atterm-hook symlink. Versioned binaries under ~/.atterm/bin/
// are left in place (a long-running claude session may still hold a
// reference); GC happens on the next Install.
func Uninstall(_ context.Context) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return uninstallAt(homeOrDie())
}

func installAt(home string) error {
	link, _, err := ensureBinary(home)
	if err != nil {
		return err
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		return err
	}

	desired := desiredEntries(link)
	merged := mergeAttermEntries(cfg.Hooks.Notification, desired, isAttermHookCommand)
	if entriesEqual(cfg.Hooks.Notification, merged) {
		return nil
	}
	cfg.Hooks.Notification = merged

	return writeClaudeSettings(home, cfg)
}

func uninstallAt(home string) error {
	link := attermHookSymlink(home)
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	cfg, err := readClaudeSettings(home)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	filtered := make([]HookEntry, 0, len(cfg.Hooks.Notification))
	for _, e := range cfg.Hooks.Notification {
		if !isAttermHookCommand(e) {
			filtered = append(filtered, e)
		}
	}
	if entriesEqual(cfg.Hooks.Notification, filtered) {
		return nil
	}
	cfg.Hooks.Notification = filtered
	return writeClaudeSettings(home, cfg)
}

// desiredEntries returns the single Notification entry we own. The
// matcher is empty (match all notifications) and the command relays the
// full hook payload to the atterm desktop process, which discriminates
// the event kind itself — so one schema-valid entry replaces what used
// to be two object-matcher entries.
func desiredEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// entriesEqual compares two []HookEntry by JSON marshaling (cheap; the
// arrays are small). Used to short-circuit a no-op write.
func entriesEqual(a, b []HookEntry) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}
