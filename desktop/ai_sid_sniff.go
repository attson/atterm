package main

import (
	"path/filepath"
	"strings"
)

// AI CLI metadata: how to resume each kind, and command-line classification.
// Session-id resolution lives in ai_sid_resolve.go (claude = title match,
// codex = filename heuristic). The old mtime-diff sniff was retired because it
// timed out on resumed/idle sessions and aborted on same-cwd concurrency.

// aiSniffSpec carries the resume invocation for one CLI.
type aiSniffSpec struct {
	ResumeArgs func(sid string) []string
}

var aiSniffers = map[string]aiSniffSpec{
	"claude": {ResumeArgs: func(sid string) []string { return []string{"--resume", sid} }},
	"codex":  {ResumeArgs: func(sid string) []string { return []string{"resume", sid} }},
	// aider resumes by cwd, not by id — and per the recovery design we do NOT
	// fall back to re-running a recorded command, so there's nothing to resume
	// without a precise id.
	"aider": {ResumeArgs: func(_ string) []string { return nil }},
}

// computeResumeArgs picks the resume invocation for an AI pane. Returns nil
// when no resume should happen: unknown kind, aider (no id-based resume), or a
// kind that needs a captured sid but doesn't have one. No command-replay
// fallback (recovery design: never resume the wrong conversation).
//
// commandLine is the original full command line the AI CLI was launched with
// (the pane's last recorded command). For claude we preserve its launch flags
// (e.g. --permission-mode bypassPermissions, --model) onto the resume command
// so a recovered session keeps the user's original settings — see
// claudePreservedFlags. codex/aider ignore it.
func computeResumeArgs(kind, sid, commandLine string) []string {
	spec, ok := aiSniffers[kind]
	if !ok {
		return nil
	}
	if kind == "aider" {
		return nil
	}
	if sid == "" {
		return nil
	}
	args := spec.ResumeArgs(sid)
	if args == nil {
		return nil
	}
	out := append([]string{kind}, args...)
	if kind == "claude" {
		out = append(out, claudePreservedFlags(commandLine)...)
	}
	return out
}

// claudeValueFlags are claude launch flags that REQUIRE a following value
// (separate-token form, e.g. `--permission-mode plan`). When such a flag
// appears without its value — last token, or immediately followed by another
// flag — the command line is corrupted; re-injecting the valueless flag makes
// claude exit with "option '--X <...>' argument missing", which then fails
// recovery on every restart (the broken line is captured and re-propagated).
// We drop the valueless flag to break that cycle. The inline `--flag=value`
// form is unaffected (it carries its own value as one token). Boolean flags
// (--dangerously-skip-permissions, --continue, --debug, ...) are NOT listed
// here, so they are always preserved.
var claudeValueFlags = map[string]bool{
	"--permission-mode":      true,
	"--model":                true,
	"--add-dir":              true,
	"--mcp-config":           true,
	"--settings":             true,
	"--append-system-prompt": true,
}

// claudePreservedFlags extracts the launch flags worth carrying onto a claude
// `--resume` from the original command line. It skips the same leading
// env-assign / wrapper / binary tokens classifyAIKindFromCommand skips, then
// drops the flags that would collide with the resume we inject ourselves:
// `--resume`/`-r` (and a following non-flag id value), the `--resume=...` form,
// and `--continue`/`-c`. A value-taking flag (claudeValueFlags) left without
// its value is dropped to avoid re-injecting a broken `--X` (see that var).
// Everything else (--permission-mode <v>, --model <v>, --dangerously-skip-
// permissions, --add-dir <v>, ...) is preserved verbatim.
//
// Tokenization mirrors classifyAIKindFromCommand (whitespace split): the source
// is the OSC 133;C command line, already whitespace-joined, so flag values with
// embedded spaces aren't representable here either — acceptable and consistent.
func claudePreservedFlags(commandLine string) []string {
	tokens := strings.Fields(commandLine)
	// Skip env-assign prefixes and wrappers (env/sudo/...), then the binary.
	for len(tokens) > 0 {
		t := tokens[0]
		if envAssignFromCommand(t) || isAIWrapper(t) {
			tokens = tokens[1:]
			continue
		}
		break
	}
	if len(tokens) == 0 {
		return nil
	}
	tokens = tokens[1:] // drop the claude binary token
	var out []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--resume" || t == "-r":
			// Drop the flag and its id value, but don't swallow a following flag.
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				i++
			}
		case strings.HasPrefix(t, "--resume="):
			// inline value form — drop the whole token
		case t == "--continue" || t == "-c":
			// no value to drop
		case claudeValueFlags[t]:
			// Value-taking flag: keep it WITH its value, but drop it when the
			// value is missing (last token, or next token is another flag) so we
			// never re-inject a broken `--X` that crashes claude on resume.
			if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				out = append(out, t, tokens[i+1])
				i++
			}
		default:
			out = append(out, t)
		}
	}
	return out
}

// classifyAIKindFromCommand extracts the AI CLI kind from an OSC 133;C
// command line. Returns "" when the command's first token (basename, after
// stripping env-assign prefixes and a small wrapper list) isn't in
// aiSniffers. Mirrors the frontend's classifyAIKind in lib/aiKind.ts —
// keep the two in sync.
func classifyAIKindFromCommand(commandLine string) string {
	tokens := strings.Fields(commandLine)
	for len(tokens) > 0 {
		t := tokens[0]
		if envAssignFromCommand(t) || isAIWrapper(t) {
			tokens = tokens[1:]
			continue
		}
		break
	}
	if len(tokens) == 0 {
		return ""
	}
	first := filepath.Base(tokens[0])
	if _, ok := aiSniffers[first]; ok {
		return first
	}
	return ""
}

var aiWrappers = map[string]struct{}{
	"sudo": {}, "time": {}, "nice": {}, "env": {},
}

func isAIWrapper(t string) bool {
	_, ok := aiWrappers[t]
	return ok
}

func envAssignFromCommand(t string) bool {
	eq := strings.IndexByte(t, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := t[i]
		if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9' && i > 0)) {
			return false
		}
	}
	return true
}
