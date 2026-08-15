package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// attermHookCommand is the command line written into an agent's hook config.
// The agent kind is explicit so the receiving end never has to infer which CLI
// invoked it — the two share a payload shape but not their event vocabulary.
func attermHookCommand(link, agentKind string) string {
	return link + " --agent " + agentKind
}

// codexHookEvents are the events whose payloads carry a task-state meaning.
// Everything else codex offers (SessionStart, SessionEnd, subagent and
// compaction events) is deliberately left alone: the turn-scoped events already
// describe the state machine, and reading more would only add ways for the two
// sources to disagree.
var codexHookEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"Stop",
	"PermissionRequest",
}

// codexHooksPath is the user-level hooks file for a given home.
//
// Repo-local .codex/config.toml is not an option: hooks configured there do not
// fire in interactive sessions (openai/codex#17532).
func codexHooksPath(home string) string {
	return filepath.Join(home, ".codex", "hooks.json")
}

type codexHookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type codexHookEntry struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []codexHookHandler `json:"hooks"`
}

// installCodexAt makes ~/.codex/hooks.json name our binary for every event we
// read, merging rather than replacing: the file belongs to the user and codex
// loads every entry in it.
//
// Called on every launch, so it is a no-op when the file already says what we
// want — rewriting it each time would churn the user's file and its mtime for
// nothing.
func installCodexAt(home, link string) error {
	path := codexHooksPath(home)

	doc := map[string][]codexHookEntry{}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		// A hand-broken file is not a reason to refuse to install; start from
		// empty rather than propagating a parse error to the caller.
		_ = json.Unmarshal(raw, &doc)
	}

	want := codexHookEntry{
		Hooks: []codexHookHandler{{Type: "command", Command: attermHookCommand(link, "codex")}},
	}
	changed := false
	for _, event := range codexHookEvents {
		cur := doc[event]
		merged := make([]codexHookEntry, 0, len(cur)+1)
		for _, entry := range cur {
			// Drop our own entry from a previous run — including one written by
			// an older build whose binary path or flags differed — so a version
			// bump replaces the line instead of stacking a second one.
			if isAttermCodexEntry(entry) {
				continue
			}
			merged = append(merged, entry)
		}
		merged = append(merged, want)
		if !codexEntriesEqual(cur, merged) {
			changed = true
		}
		doc[event] = merged
	}
	if !changed {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// uninstallCodexAt strips the entries we own, leaving the user's untouched. An
// event left with no entries is removed entirely rather than left as an empty
// array, so an uninstall returns the file to how it looked before.
func uninstallCodexAt(home string) error {
	path := codexHooksPath(home)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	doc := map[string][]codexHookEntry{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil // not ours to repair
	}
	changed := false
	for event, entries := range doc {
		kept := make([]codexHookEntry, 0, len(entries))
		for _, entry := range entries {
			if isAttermCodexEntry(entry) {
				changed = true
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(doc, event)
			continue
		}
		doc[event] = kept
	}
	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// isAttermCodexEntry recognises our own entry across binary hash changes, the
// same way isAttermHookCommand does for claude.
func isAttermCodexEntry(e codexHookEntry) bool {
	for _, h := range e.Hooks {
		if strings.Contains(h.Command, ".atterm/bin/atterm-hook") {
			return true
		}
	}
	return false
}

func codexEntriesEqual(a, b []codexHookEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Matcher != b[i].Matcher || len(a[i].Hooks) != len(b[i].Hooks) {
			return false
		}
		for j := range a[i].Hooks {
			if a[i].Hooks[j] != b[i].Hooks[j] {
				return false
			}
		}
	}
	return true
}
