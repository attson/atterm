package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// claudeWatchDir returns ~/.claude/projects/<cwd-encoded>/ where Claude
// Code writes one <UUID>.jsonl per conversation. cwd-encoded replaces
// every non-alphanumeric character with '-' (so '/', '.', '_', '+' etc.
// all collapse to '-') — this mirrors Claude Code's own project-dir
// encoding. A git-worktree cwd like ".../.claude/worktrees/feat+a_b" thus
// maps to "...--claude-worktrees-feat-a-b"; the earlier "only replace '/'"
// form missed such dirs entirely, so the AI session id never resolved
// (ai=- in logs) and the worktree pane could not be --resumed on restart.
// home is injected for tests; production uses os.UserHomeDir().
func claudeWatchDir(cwd string, _ time.Time, home string) string {
	return filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
}

// encodeClaudeProjectDir replaces every non-alphanumeric rune in p with '-',
// matching Claude Code's project-directory naming. Runs of specials each map
// to their own '-' (no collapsing), e.g. "/.claude" → "--claude".
func encodeClaudeProjectDir(p string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, p)
}

// codexWatchDir returns ~/.codex/sessions/YYYY/MM/DD/ keyed by the wall
// clock at fork time. Codex writes rollout-<ts>-<UUID>.jsonl files into
// this directory. now is injected for tests.
func codexWatchDir(_ string, now time.Time, home string) string {
	y, m, d := now.Date()
	return filepath.Join(home, ".codex", "sessions",
		fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", int(m)), fmt.Sprintf("%02d", d))
}

// claudeParseSid extracts the UUID stem from a Claude jsonl filename.
// "<UUID>.jsonl" → UUID; rejects anything that doesn't pass RFC4122 parse.
func claudeParseSid(name string) (string, bool) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	stem := strings.TrimSuffix(name, ".jsonl")
	if _, err := uuid.Parse(stem); err != nil {
		return "", false
	}
	return stem, true
}

// codexParseSid extracts the UUID tail from a Codex rollout filename of
// the form "rollout-<ISO-with-dashes>-<UUID>.jsonl". The last 36
// characters of the body (stripped of prefix/suffix) are validated as
// a UUID.
func codexParseSid(name string) (string, bool) {
	if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(name, "rollout-"), ".jsonl")
	if len(body) < 36 {
		return "", false
	}
	sid := body[len(body)-36:]
	if _, err := uuid.Parse(sid); err != nil {
		return "", false
	}
	return sid, true
}
