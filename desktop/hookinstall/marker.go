package hookinstall

import (
	"encoding/json"
	"strings"
)

// HookEntry mirrors a single Claude Code Notification hook entry, using
// the schema Claude Code's validator enforces: a string `matcher` plus a
// `hooks` array of command actions. (An earlier version modeled `matcher`
// as an object and put the command at the top level; Claude Code's
// /doctor rejected that as "Expected string"/"Expected array".)
// Lives here rather than in settings.go because both marker and
// settings.go reference it; this is its natural home.
type HookEntry struct {
	Matcher string        `json:"matcher"`
	Hooks   []HookCommand `json:"hooks"`
}

// HookCommand is a single action inside a HookEntry's `hooks` array.
// Type is always "command"; Command is the executable path/line.
type HookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// UnmarshalJSON tolerates the legacy entry shape this package used to
// write — `{"matcher": {"type": ...}, "command": "..."}` — so that a
// settings.json left behind by an older atterm can still be read (and
// then rewritten in the current schema) instead of failing the whole
// parse. Without this, readClaudeSettings errors on the old object-typed
// matcher and Install aborts, stranding the broken entry on disk.
func (e *HookEntry) UnmarshalJSON(data []byte) error {
	var raw struct {
		Matcher json.RawMessage `json:"matcher"`
		Hooks   []HookCommand   `json:"hooks"`
		Command string          `json:"command"` // legacy top-level command
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// matcher: accept a string; silently drop a legacy object/other form
	// (it gets normalized to "" on the next write).
	if len(raw.Matcher) > 0 {
		var s string
		if json.Unmarshal(raw.Matcher, &s) == nil {
			e.Matcher = s
		}
	}
	e.Hooks = raw.Hooks
	// Legacy entries carry the command at the top level; fold it into a
	// hooks action so isAttermHookCommand still recognizes (and replaces)
	// an old atterm-owned entry.
	if len(e.Hooks) == 0 && raw.Command != "" {
		e.Hooks = []HookCommand{{Type: "command", Command: raw.Command}}
	}
	return nil
}

// isAttermHookCommand returns true when any of an entry's hook commands
// reference the atterm-managed binary path. Substring match — not strict
// equality — so that paths with differing $HOME expansions still match
// across machines. The substring deliberately omits the leading slash so
// paths quoted/relativized in user shells still match; the documented
// price is a corner-case false positive on unrelated commands that
// mention ".atterm/bin/atterm-hook" verbatim (e.g. grep for the path),
// which we accept.
func isAttermHookCommand(e HookEntry) bool {
	for _, h := range e.Hooks {
		if strings.Contains(h.Command, ".atterm/bin/atterm-hook") {
			return true
		}
	}
	return false
}
