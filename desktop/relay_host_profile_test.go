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
	// The subtest above passes defaultProfileID: "" — it cannot distinguish
	// "an unresolvable explicit id falls back to no profile" from "an
	// unresolvable explicit id falls back to the default profile", because
	// both answers produce nil when there is no default to fall back to.
	// This is the pinning test the final whole-branch review asked for
	// (Important 2): a stale explicit id must resolve to nil EVEN WHEN a
	// live default profile is configured, i.e. it must not be silently
	// promoted to the default. The user asked for something specific by id;
	// if that thing is gone, giving them a different profile they never
	// picked is a worse surprise than giving them none.
	t.Run("a stale explicit id does not fall through to a configured default", func(t *testing.T) {
		if got := resolveSessionProfile(profiles, "p1", "gone"); got != nil {
			t.Fatalf("got %+v, want nil — an unresolvable explicit id must not silently promote the default (p1)", got)
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

// TestRelayHost_NewSession_ProfileShellMissingFallsBackToRequestCommand covers
// the shell half of the Important 3 final-review ruling: a synced profile
// whose Shell doesn't exist on this machine must not fail the whole session —
// NewSession degrades to req.Command (what the caller already resolved from
// default_shell), exactly as if the profile had no Shell set at all.
func TestRelayHost_NewSession_ProfileShellMissingFallsBackToRequestCommand(t *testing.T) {
	h := newTestRelayHost(t)

	missingShell := filepath.Join(t.TempDir(), "no-such-shell")
	cwd := t.TempDir()

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "broken-shell", Name: "Broken Shell", Shell: missingShell},
	}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: cwd, ProfileID: "broken-shell", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession must fall back to the request's command rather than fail: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}
	info := sess.Info()
	if strings.Contains(info.Command, missingShell) {
		t.Errorf("command = %q, must not use the missing profile shell %q", info.Command, missingShell)
	}
	if !strings.Contains(info.Command, "/bin/sh") {
		t.Errorf("command = %q, want fallback to the request's /bin/sh", info.Command)
	}
}

// TestRelayHost_NewSession_ProfileCwdMissingFailsNamingProfile covers the cwd
// half of the Important 3 final-review ruling: unlike Shell, a synced
// profile's Cwd gets no fallback — a wrong cwd would otherwise open a working
// session in a directory the user never chose, silently. NewSession must fail
// outright, and the error must name the profile so the failure isn't a bare
// "no such file or directory" with no link back to the profile that caused
// it.
func TestRelayHost_NewSession_ProfileCwdMissingFailsNamingProfile(t *testing.T) {
	h := newTestRelayHost(t)

	missingCwd := filepath.Join(t.TempDir(), "no-such-dir")

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{ID: "broken-cwd", Name: "Broken Cwd", Cwd: missingCwd},
	}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: t.TempDir(), ProfileID: "broken-cwd", Cols: 80, Rows: 24,
	})
	if err == nil {
		t.Fatal("NewSession must fail loudly when the profile's cwd does not exist, not silently land elsewhere")
	}
	if !strings.Contains(err.Error(), "Broken Cwd") || !strings.Contains(err.Error(), "broken-cwd") {
		t.Errorf("error = %q, want it to name the profile (name %q and id %q)", err.Error(), "Broken Cwd", "broken-cwd")
	}
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
// (session.Session), and a GENUINE AI-restore session — AIKind set AND
// InitialAISessionID non-empty, so computeResumeArgs actually returns a
// resume command and the restore block really does call SetOnFirstPrompt —
// already claims it for the resume command. A profile-selected StartupCmd
// must not clobber that.
//
// This must set InitialAISessionID; AIKind alone is not sufficient to claim
// the callback (see TestRelayHost_NewSession_ProfileStartupCommandRunsForFreshAIClassification,
// which pins the opposite: AIKind set with no InitialAISessionID must NOT
// suppress the startup command). An earlier version of this test set only
// AIKind and passed while pinning the bug this pair now guards against.
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
		AIKind:             "claude",
		InitialAISessionID: "sid-123", // makes computeResumeArgs non-nil: a genuine restore
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
		t.Fatal("profile startup command ran on a genuine AI-restore session — it must have been skipped, not clobbered the resume injection")
	}
}

// TestRelayHost_NewSession_ProfileStartupCommandRunsForFreshAIClassification
// is the counterpart Finding 1 asked for: a session where the frontend
// classified AIKind from what the user typed (e.g. they typed "claude"
// fresh) has AIKind set but NO InitialAISessionID, so computeResumeArgs
// returns nil and nothing claims SetOnFirstPrompt. A default profile applies
// to this session exactly as it would to any other new session, so its
// StartupCmd must still run.
//
// This is the test that catches gating on req.AIKind == "" instead of the
// real "did the resume block actually claim the callback" condition.
func TestRelayHost_NewSession_ProfileStartupCommandRunsForFreshAIClassification(t *testing.T) {
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
		AIKind: "claude", // classified from typed text, NOT a restore
		// InitialAISessionID intentionally empty: no resume id available.
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
	t.Fatal("profile startup command was not injected — a fresh AI classification with no resume id must not suppress it")
}

// TestRelayHost_NewSession_ProfileEnvReachesSpawnedProcess proves the
// resolved profile's Env actually lands in the spawned child's environment
// through the full h.NewSession path — not just that applyProfileEnv itself
// is correct in isolation (TestApplyProfileEnvProtectsTERM already covers
// that). Deleting the `env = applyProfileEnv(env, profile.Env)` call in
// NewSession would still pass every other profile test, since none of them
// inspect the child's actual environment.
//
// Uses the same "spawn a real shell, observe a side effect" pattern as the
// startup-command tests: the profile's StartupCmd echoes the profile-set
// variable to a marker file. If the env never reached the child, the shell
// would expand $FOO to empty and the file would contain a blank line, not
// the value.
func TestRelayHost_NewSession_ProfileEnvReachesSpawnedProcess(t *testing.T) {
	h := newTestRelayHost(t)

	cwd := t.TempDir()
	marker := filepath.Join(cwd, "env-seen")

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{
			ID:         "p1",
			Name:       "P1",
			Env:        map[string]string{"ATTERM_PROFILE_TEST_VAR": "profile-env-value"},
			StartupCmd: "echo $ATTERM_PROFILE_TEST_VAR > " + marker,
		},
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
		if b, err := os.ReadFile(marker); err == nil {
			got := strings.TrimSpace(string(b))
			if got != "profile-env-value" {
				t.Fatalf("child process did not see the profile's env var: marker contains %q, want %q", got, "profile-env-value")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("marker file was never written — startup command did not run")
}
