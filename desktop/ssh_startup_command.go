package main

import (
	"context"
	"io"
	"strings"
	"time"
)

// Startup commands (SSHHost.StartupCommand) are the answer to a bastion that
// puts a menu in front of the shell. JumpServer is the case this was built
// for: logging in lands on `Opt>`, where you type the asset, and the reply is
// a *second* prompt (`ID>`) asking which system user to log in as. Neither
// step is a shell command — they are answers typed at a program — so there is
// nothing to pass on the ssh command line. The only thing that works is to
// type them, which is what this file does.
//
// It is deliberately delay-driven rather than expect-driven. Matching on the
// prompt would be sturdier, but reading the shell's output means tapping
// sshPtyHost on its way into relay's adopt pipeline, and a menu that answers
// in well under a second does not justify that. The interval is per host
// (StartupDelayMs) so a slow bastion is a field the user edits, not a bug
// report.
const (
	defaultStartupDelay = 1500 * time.Millisecond
	minStartupDelay     = 100 * time.Millisecond
	maxStartupDelay     = 60 * time.Second

	// startupReturnDelay separates a line from its carriage return. They are
	// two writes, never one: "text\r" arriving in a single read is a *paste*
	// to a program in raw mode, not a typed line, and this repo has had to
	// re-fix exactly that on the quick-template path three times (#63, #110,
	// #129). The gap only has to be long enough for the far side to see two
	// reads.
	startupReturnDelay = 80 * time.Millisecond
)

// startupCommandLines splits a startup-command block into the lines to type,
// one per prompt. Blank lines are dropped rather than sent as a bare Enter: a
// stray trailing newline is far likelier than a deliberate "just press Enter"
// step, and an extra Enter at a JumpServer prompt re-prints the menu.
func startupCommandLines(s string) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// startupDelay turns the host's configured interval into a duration, filling
// in the default for an unset value and clamping the rest. The floor keeps a
// typo (5) from firing every step into a prompt that has not been drawn yet;
// the ceiling keeps one from parking a goroutine for an hour.
func startupDelay(ms int) time.Duration {
	if ms <= 0 {
		return defaultStartupDelay
	}
	d := time.Duration(ms) * time.Millisecond
	if d < minStartupDelay {
		return minStartupDelay
	}
	if d > maxStartupDelay {
		return maxStartupDelay
	}
	return d
}

// sendStartupCommand types lines into an already-open remote shell, waiting
// `delay` before each one so the prompt it answers has been drawn.
//
// Write errors end the sequence: the session is gone (or going), and typing
// the remaining steps into it would at best be wasted and at worst land in
// whatever replaced it.
func sendStartupCommand(ctx context.Context, w io.Writer, lines []string, delay time.Duration) {
	for _, line := range lines {
		if !sleepCtx(ctx, delay) {
			return
		}
		if _, err := io.WriteString(w, line); err != nil {
			return
		}
		if !sleepCtx(ctx, startupReturnDelay) {
			return
		}
		if _, err := io.WriteString(w, "\r"); err != nil {
			return
		}
	}
}

// sleepCtx waits for d, reporting false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
