package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// aiSniffSpec captures everything we need to watch one CLI's data dir.
// WatchDir == nil means "do not sniff" (currently aider — it resumes by
// cwd, no UUID involved).
type aiSniffSpec struct {
	Kind       string
	WatchDir   func(cwd string, now time.Time, home string) string
	NewFile    func(name string) (sid string, ok bool)
	ResumeArgs func(sid string) []string
}

var aiSniffers = map[string]aiSniffSpec{
	"claude": {
		Kind:       "claude",
		WatchDir:   claudeWatchDir,
		NewFile:    claudeParseSid,
		ResumeArgs: func(sid string) []string { return []string{"--resume", sid} },
	},
	"codex": {
		Kind:       "codex",
		WatchDir:   codexWatchDir,
		NewFile:    codexParseSid,
		ResumeArgs: func(sid string) []string { return []string{"resume", sid} },
	},
	"aider": {
		Kind: "aider",
		// WatchDir/NewFile nil → sniffer never starts; resume falls back
		// to re-injecting last_command_line.
		ResumeArgs: func(_ string) []string { return nil },
	},
}

// startAISniff is the production entry point: home from os.UserHomeDir,
// poll cadence 100ms→3.2s exponential, total 30s budget.
func startAISniff(ctx context.Context, cwd string, kind string, onCapture func(sid string)) {
	spec, ok := aiSniffers[kind]
	if !ok || spec.WatchDir == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("recovery: no home for AI sniff: %v", err)
		return
	}
	dir := spec.WatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("recovery: mkdir %s: %v — skip sniff", dir, err)
		return
	}
	go sniffAISessionIDForTest(ctx, dir, spec, 100*time.Millisecond, 30*time.Second, onCapture)
}

// sniffAISessionIDForTest is the testable core loop. Exported only via
// test access (function name is non-Test- so it's package-visible but the
// _ForTest suffix flags it as an internal seam — production callers use
// startAISniff).
func sniffAISessionIDForTest(
	ctx context.Context,
	dir string,
	spec aiSniffSpec,
	initialInterval time.Duration,
	totalBudget time.Duration,
	onCapture func(sid string),
) {
	before := snapshotJsonlMtimes(dir)
	deadline := time.Now().Add(totalBudget)
	interval := initialInterval
	const maxInterval = 3200 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		active := activeJsonl(snapshotJsonlMtimes(dir), before)
		if len(active) >= 2 {
			log.Printf("recovery: ai sniff ambiguous (%d active files in %s) — abort", len(active), dir)
			return
		}
		if len(active) == 1 {
			if sid, ok := spec.NewFile(active[0]); ok {
				onCapture(sid)
				return
			}
		}
		interval *= 2
		if interval > maxInterval {
			interval = maxInterval
		}
	}
	log.Printf("recovery: ai sniff timeout in %s", dir)
}

// snapshotJsonlMtimes maps each *.jsonl basename in dir to its modtime (empty
// if dir is unreadable). Subdirectories (Claude writes a per-session subdir of
// the same UUID) and non-jsonl files (locks, *.tmp) are skipped so they can't
// create false "new file" ambiguity.
func snapshotJsonlMtimes(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out[e.Name()] = info.ModTime()
	}
	return out
}

// activeJsonl returns the *.jsonl basenames that are new (absent from before)
// or whose modtime advanced — i.e. the session file(s) being actively written
// during the watch window. Detecting modified files (not just newly created
// ones) is what captures resumed/continued sessions: `claude -c` / `--resume`
// and atterm's own restore append to an existing file rather than creating a
// new one, so a creation-only watch would time out without ever capturing the
// sid.
func activeJsonl(now, before map[string]time.Time) []string {
	out := []string{}
	for name, mt := range now {
		if prev, existed := before[name]; !existed || mt.After(prev) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// computeResumeArgs picks the right resume invocation for an AI pane.
// Returns nil when no resume should be injected (no kind / unknown / no
// captured sid for kinds that require one).
func computeResumeArgs(kind, sid, lastCommandLine string) []string {
	spec, ok := aiSniffers[kind]
	if !ok {
		return nil
	}
	if kind == "aider" {
		if lastCommandLine == "" {
			return nil
		}
		return []string{lastCommandLine}
	}
	if sid == "" {
		return nil
	}
	bin := kind // claude → "claude" ; codex → "codex"
	args := spec.ResumeArgs(sid)
	if args == nil {
		return nil
	}
	return append([]string{bin}, args...)
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
