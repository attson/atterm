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

// ownedSlot wires one hook slot's lifecycle (read → merge/strip → write back)
// through a single ClaudeHooks pointer field, so install/uninstall stay one
// switch-case instead of five copies of the same three lines.
type ownedSlot struct {
	name    string
	field   func(*ClaudeHooks) *[]HookEntry
	desired func(link string) []HookEntry
}

var ownedSlots = []ownedSlot{
	{"Notification", func(h *ClaudeHooks) *[]HookEntry { return &h.Notification }, desiredNotificationEntries},
	{"PreToolUse", func(h *ClaudeHooks) *[]HookEntry { return &h.PreToolUse }, desiredPreToolUseEntries},
	{"UserPromptSubmit", func(h *ClaudeHooks) *[]HookEntry { return &h.UserPromptSubmit }, desiredUserPromptSubmitEntries},
	{"Stop", func(h *ClaudeHooks) *[]HookEntry { return &h.Stop }, desiredStopEntries},
	{"PostToolUse", func(h *ClaudeHooks) *[]HookEntry { return &h.PostToolUse }, desiredPostToolUseEntries},
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

	merged := make([][]HookEntry, len(ownedSlots))
	allEqual := true
	for i, s := range ownedSlots {
		cur := *s.field(&cfg.Hooks)
		merged[i] = mergeAttermEntries(cur, s.desired(link), isAttermHookCommand)
		if !entriesEqual(cur, merged[i]) {
			allEqual = false
		}
	}
	if allEqual {
		return nil
	}
	for i, s := range ownedSlots {
		*s.field(&cfg.Hooks) = merged[i]
	}
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
	filtered := make([][]HookEntry, len(ownedSlots))
	allEqual := true
	for i, s := range ownedSlots {
		cur := *s.field(&cfg.Hooks)
		filtered[i] = stripAttermEntries(cur)
		if !entriesEqual(cur, filtered[i]) {
			allEqual = false
		}
	}
	if allEqual {
		return nil
	}
	for i, s := range ownedSlots {
		*s.field(&cfg.Hooks) = filtered[i]
	}
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

// desiredPreToolUseEntries returns the PreToolUse entry we own. The matcher
// is empty so a single entry covers both the WaitingInput path (AskUserQuestion
// — claude-code's Notification hook does NOT fire for that tool) and the
// streaming anchor-card path (other tool uses → 🛠 calls in the card body).
// The dispatcher's claudeCodeAdapter.Parse / ParseTurn route by tool_name.
func desiredPreToolUseEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// desiredUserPromptSubmitEntries returns the UserPromptSubmit entry we own.
// claudeCodeAdapter.ParseTurn emits TurnUserPrompt → 👤 in the anchor card.
func desiredUserPromptSubmitEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// desiredStopEntries returns the Stop entry we own. claudeCodeAdapter.ParseTurn
// emits TurnAssistantFinal → 🤖 in the anchor card.
func desiredStopEntries(link string) []HookEntry {
	return []HookEntry{
		{Matcher: "", Hooks: []HookCommand{{Type: "command", Command: link}}},
	}
}

// desiredPostToolUseEntries returns the PostToolUse entry we own.
// claudeCodeAdapter.ParseTurn emits TurnToolEnd → 🛠 result in the card.
func desiredPostToolUseEntries(link string) []HookEntry {
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
