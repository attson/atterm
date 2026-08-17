package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestResolveSessionProfile exercises design §4's precedence in isolation,
// without spinning up a relayHost: explicit id > default id > no profile.
func TestResolveSessionProfile(t *testing.T) {
	profiles := []SessionProfile{
		{ID: "p1", Name: "P1", Shell: "/bin/echo"},
		{ID: "p2", Name: "P2", Shell: "/bin/cat"},
	}

	t.Run("explicit wins over default", func(t *testing.T) {
		got := resolveSessionProfile(profiles, "p1", "p2")
		if got == nil || got.ID != "p2" {
			t.Fatalf("got %+v, want p2", got)
		}
	})
	t.Run("falls back to default when no explicit choice", func(t *testing.T) {
		got := resolveSessionProfile(profiles, "p1", "")
		if got == nil || got.ID != "p1" {
			t.Fatalf("got %+v, want p1", got)
		}
	})
	t.Run("nil when neither explicit nor default is set", func(t *testing.T) {
		if got := resolveSessionProfile(profiles, "", ""); got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})
	t.Run("a stale/deleted id resolves to nil, not an error", func(t *testing.T) {
		if got := resolveSessionProfile(profiles, "", "gone"); got != nil {
			t.Fatalf("got %+v, want nil for an unknown id", got)
		}
	})
}

// TestApplyProfileEnvProtectsTERM is the design §6.3 regression test: a
// profile env containing TERM must never win over what terminalEnvForXterm
// set, or the renderer's terminal capability negotiation breaks. Every other
// key is the profile's to override.
func TestApplyProfileEnvProtectsTERM(t *testing.T) {
	base := terminalEnvForXterm(nil)
	merged := applyProfileEnv(base, map[string]string{"TERM": "vt100", "FOO": "bar"})

	got := map[string]string{}
	for _, e := range merged {
		if k, v, ok := strings.Cut(e, "="); ok {
			got[k] = v
		}
	}
	if got[envKeyTerm] != xtermTerm {
		t.Errorf("TERM must stay %q (set by terminalEnvForXterm), got %q — a profile must not be able to override it", xtermTerm, got[envKeyTerm])
	}
	if got["FOO"] != "bar" {
		t.Errorf("non-protected profile env keys must apply: got %+v", got)
	}
}

// TestRelayHost_NewSession_ProfilePrecedence drives the full precedence
// chain through h.NewSession and checks the adopted session's Info(): an
// explicitly chosen profile's shell/cwd win, then the configured default's,
// then — with no profile at all — today's behavior (whatever the caller
// already resolved into req.Command/req.Cwd) is unchanged.
func TestRelayHost_NewSession_ProfilePrecedence(t *testing.T) {
	h := newTestRelayHost(t)

	explicitCwd := t.TempDir()
	defaultCwd := t.TempDir()
	fallbackCwd := t.TempDir()

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "explicit", Name: "Explicit", Shell: "/bin/cat", Cwd: explicitCwd},
		{ID: "default", Name: "Default", Shell: "/bin/echo", Cwd: defaultCwd},
	}
	cfg.DefaultProfileID = "default"
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit profile overrides the default profile", func(t *testing.T) {
		id, err := h.NewSession(context.Background(), NewSessionReq{
			Command: "/bin/sh", Cwd: fallbackCwd, ProfileID: "explicit", Cols: 80, Rows: 24,
		})
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		sess, ok := h.server.Registry().Get(id)
		if !ok {
			t.Fatalf("session %s not found", id)
		}
		info := sess.Info()
		if info.Cwd != explicitCwd {
			t.Errorf("cwd = %q, want explicit profile's %q", info.Cwd, explicitCwd)
		}
		if !strings.Contains(info.Command, "/bin/cat") {
			t.Errorf("command = %q, want explicit profile's shell /bin/cat", info.Command)
		}
	})

	t.Run("default profile applies when nothing explicit is chosen", func(t *testing.T) {
		id, err := h.NewSession(context.Background(), NewSessionReq{
			Command: "/bin/sh", Cwd: fallbackCwd, Cols: 80, Rows: 24,
		})
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		sess, ok := h.server.Registry().Get(id)
		if !ok {
			t.Fatalf("session %s not found", id)
		}
		info := sess.Info()
		if info.Cwd != defaultCwd {
			t.Errorf("cwd = %q, want default profile's %q", info.Cwd, defaultCwd)
		}
		if !strings.Contains(info.Command, "/bin/echo") {
			t.Errorf("command = %q, want default profile's shell /bin/echo", info.Command)
		}
	})

	t.Run("no profile at all falls back to today's behavior", func(t *testing.T) {
		cfg := h.cfg.Get()
		cfg.DefaultProfileID = ""
		if err := h.cfg.Set(cfg); err != nil {
			t.Fatal(err)
		}
		id, err := h.NewSession(context.Background(), NewSessionReq{
			Command: "/bin/sh", Cwd: fallbackCwd, Cols: 80, Rows: 24,
		})
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		sess, ok := h.server.Registry().Get(id)
		if !ok {
			t.Fatalf("session %s not found", id)
		}
		info := sess.Info()
		if info.Cwd != fallbackCwd {
			t.Errorf("cwd = %q, want the request's own %q (no profile applied)", info.Cwd, fallbackCwd)
		}
		if !strings.Contains(info.Command, "/bin/sh") {
			t.Errorf("command = %q, want /bin/sh (no profile applied)", info.Command)
		}
	})
}

// TestRelayHost_NewSession_ProfileStartupCommandInjectedOnFirstPrompt proves
// the profile's StartupCmd reaches the PTY through session.SetOnFirstPrompt
// — the mechanism session-restore's resume-command injection already uses —
// rather than through a frontend sendInput of "<cmd>\r" (AGENTS.md redline
// #28: Codex reads a trailing CR as a paste, not a submitted command).
//
// Simulates the shell drawing its first prompt by pushing an OSC 133;A
// marker directly (the same trigger firstprompt_test.go uses), then checks
// for the side effect of the injected command actually running in the real
// child shell, proving the bytes reached the PTY and were executed — not
// just that a callback was registered.
func TestRelayHost_NewSession_ProfileStartupCommandInjectedOnFirstPrompt(t *testing.T) {
	h := newTestRelayHost(t)

	cwd := t.TempDir()
	marker := filepath.Join(cwd, "startup-ran")

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "P1", StartupCmd: "touch " + marker},
	}
	cfg.DefaultProfileID = "p1"
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: cwd, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("\x1b]133;A\x07$ "))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("profile startup command was not injected into the PTY (marker file never appeared)")
}

// TestRelayHost_NewSession_ProfileStartupCommandSkippedForAIRestore checks
// the collision guard: SetOnFirstPrompt is a single callback slot
// (session.Session), and an AI-restore session already claims it for the
// resume command. A profile-selected StartupCmd must not clobber that —
// it is simply skipped when req.AIKind != "".
func TestRelayHost_NewSession_ProfileStartupCommandSkippedForAIRestore(t *testing.T) {
	h := newTestRelayHost(t)

	cwd := t.TempDir()
	marker := filepath.Join(cwd, "startup-ran")

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "p1", Name: "P1", StartupCmd: "touch " + marker},
	}
	cfg.DefaultProfileID = "p1"
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: cwd, Cols: 80, Rows: 24,
		AIKind: "claude", // restore path: claims SetOnFirstPrompt itself
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("\x1b]133;A\x07$ "))
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("profile startup command ran on an AI-restore session — it must have been skipped, not clobbered the resume injection")
	}
}
