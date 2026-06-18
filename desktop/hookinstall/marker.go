package hookinstall

import "strings"

// HookEntry mirrors a single Claude Code Notification hook entry.
// Lives here rather than in settings.go because both marker and
// settings.go reference it; this is its natural home.
type HookEntry struct {
	Matcher HookMatcher `json:"matcher"`
	Command string      `json:"command"`
}

// HookMatcher selects when the hook fires. Fields match Claude Code's
// schema as observed in production traffic: "type" is the event kind,
// "tool" optionally restricts an idle_prompt to a specific tool.
type HookMatcher struct {
	Type string `json:"type"`
	Tool string `json:"tool,omitempty"`
}

// isAttermHookCommand returns true when an entry's Command field
// references the atterm-managed binary path. Substring match — not
// strict equality — so that paths with differing $HOME expansions
// still match across machines. The substring deliberately omits the
// leading slash so paths quoted/relativized in user shells still
// match; the documented price is a corner-case false positive on
// unrelated commands that mention ".atterm/bin/atterm-hook" verbatim
// (e.g. grep for the path), which we accept.
func isAttermHookCommand(e HookEntry) bool {
	return strings.Contains(e.Command, ".atterm/bin/atterm-hook")
}
