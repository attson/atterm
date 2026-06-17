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
// every '/' with '-'. home is injected for tests; production uses
// os.UserHomeDir().
func claudeWatchDir(cwd string, _ time.Time, home string) string {
	return filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
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
