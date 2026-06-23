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
func computeResumeArgs(kind, sid, _ string) []string {
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
	return append([]string{kind}, args...)
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
