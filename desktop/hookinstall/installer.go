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

	mergedNotif := mergeAttermEntries(cfg.Hooks.Notification, desiredNotificationEntries(link), isAttermHookCommand)
	mergedPre := mergeAttermEntries(cfg.Hooks.PreToolUse, desiredPreToolUseEntries(link), isAttermHookCommand)
	if entriesEqual(cfg.Hooks.Notification, mergedNotif) && entriesEqual(cfg.Hooks.PreToolUse, mergedPre) {
		return nil
	}
	cfg.Hooks.Notification = mergedNotif
	cfg.Hooks.PreToolUse = mergedPre

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
	filteredNotif := stripAttermEntries(cfg.Hooks.Notification)
	filteredPre := stripAttermEntries(cfg.Hooks.PreToolUse)
	if entriesEqual(cfg.Hooks.Notification, filteredNotif) && entriesEqual(cfg.Hooks.PreToolUse, filteredPre) {
		return nil
	}
	cfg.Hooks.Notification = filteredNotif
	cfg.Hooks.PreToolUse = filteredPre
	return writeClaudeSettings(home, cfg)
}

func stripAttermEntries(in []HookEntry) []HookEntry {
	out := make([]HookEntry, 0, len(in))
	for _, e := range in {
		if !isAttermHookCommand(e) {
			out = append(out, e)
		}
	}
	return out
}

// desiredNotificationEntries returns the Notification entry we own. The
// matcher is empty (match all notification_type values) — the desktop
// adapter discriminates by notification_type itself.
func desiredNotificationEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// desiredPreToolUseEntries returns the PreToolUse entry we own. Matched
// strictly on AskUserQuestion — claude-code's Notification hook does NOT
// fire for that tool, so PreToolUse is the only place to learn the
// questions/options payload and render a button-bearing card.
func desiredPreToolUseEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "AskUserQuestion", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// entriesEqual compares two []HookEntry by JSON marshaling (cheap; the
// arrays are small). Used to short-circuit a no-op write.
func entriesEqual(a, b []HookEntry) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}
