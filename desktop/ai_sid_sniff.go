package main

import (
	"context"
	"log"
	"os"
	"sort"
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
	before := snapshotJsonlNames(dir)
	deadline := time.Now().Add(totalBudget)
	interval := initialInterval
	const maxInterval = 3200 * time.Millisecond

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
		now := snapshotJsonlNames(dir)
		diff := setDiff(now, before)
		if len(diff) >= 2 {
			log.Printf("recovery: ai sniff ambiguous (%d new files in %s) — abort", len(diff), dir)
			return
		}
		if len(diff) == 1 {
			if sid, ok := spec.NewFile(diff[0]); ok {
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

// snapshotJsonlNames returns the set of *.jsonl basenames in dir (or
// the empty set if dir is unreadable).
func snapshotJsonlNames(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out[e.Name()] = struct{}{}
	}
	return out
}

func setDiff(now, before map[string]struct{}) []string {
	out := []string{}
	for k := range now {
		if _, in := before[k]; !in {
			out = append(out, k)
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
