package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// readSessionCreated drains one TypeSessionCreated frame from out and
// decodes its payload. Shared by every test in this file.
func readSessionCreated(t *testing.T, out <-chan proto.Frame) proto.SessionCreatedPayload {
	t.Helper()
	select {
	case f := <-out:
		if f.Type != proto.TypeSessionCreated {
			t.Fatalf("frame type = %v, want TypeSessionCreated", f.Type)
		}
		var p proto.SessionCreatedPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			t.Fatalf("decode SessionCreatedPayload: %v", err)
		}
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a TypeSessionCreated frame")
		return proto.SessionCreatedPayload{}
	}
}

// waitForSessionCreateInFlight polls the handler's private in-flight counter
// (same package, so the test can read it directly rather than adding a
// production-only accessor). Used to prove release() actually ran, not just
// that acquire() blocked — see TestSessionCreateHandler_ConcurrencyBounded.
func waitForSessionCreateInFlight(t *testing.T, h *sessionCreateHandler, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.Lock()
		got := h.inFlight
		h.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("inFlight = %d after 2s, want %d", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSessionCreateHandler_Success is the happy path: a valid profile_id
// forks a real session through relayHost.NewSession, and that session is
// both attachable (in the registry) and announced (in Snapshot()) exactly
// like a locally-created one — design §3's "the phone attaches through the
// existing path, as if the session had appeared on its own."
func TestSessionCreateHandler_Success(t *testing.T) {
	h := newTestRelayHost(t)

	profileCwd := t.TempDir()
	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh", Cwd: profileCwd}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionFull)
	handler.run(proto.SessionCreatePayload{RequestID: "req-ok", HostID: "host-1", ProfileID: "p1"})

	resp := readSessionCreated(t, out)
	if !resp.OK || resp.RequestID != "req-ok" || resp.SessionID == "" {
		t.Fatalf("run() = %+v, want ok with a session id", resp)
	}
	sid := uuid.MustParse(resp.SessionID)

	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		t.Fatalf("session %s not found in registry — the phone's existing attach path looks it up here", sid)
	}
	info := sess.Info()
	if info.Cwd != profileCwd {
		t.Errorf("cwd = %q, want the profile's %q", info.Cwd, profileCwd)
	}
	if !strings.Contains(info.Command, "/bin/sh") {
		t.Errorf("command = %q, want it to contain the profile's shell /bin/sh", info.Command)
	}

	found := false
	for _, s := range h.Snapshot() {
		if s.ID == sid.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("created session %s missing from Snapshot() — that is what the uplink's ANNOUNCE sends, so a phone would never see it", sid)
	}
}

// TestSessionCreateHandler_PayloadCannotInfluenceSession is the security
// property the whole mobile "use a profile" design rests on (design §3): the
// phone sends a profile ID and never a profile body. SessionCreatePayload
// has exactly three string fields and no map/free-form field, so a raw wire
// payload carrying extra command/cwd/env/startup_cmd keys must decode with
// those keys silently dropped, and the session that gets created must come
// entirely from the LOCAL profile the id resolves to.
func TestSessionCreateHandler_PayloadCannotInfluenceSession(t *testing.T) {
	h := newTestRelayHost(t)

	safeCwd := t.TempDir()
	marker := filepath.Join(safeCwd, "env-seen")

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{
		{
			ID:         "profile-1",
			Name:       "Safe Profile",
			Shell:      "/bin/sh",
			Cwd:        safeCwd,
			Env:        map[string]string{"ATTERM_SESSION_CREATE_TEST_VAR": "safe-value"},
			StartupCmd: "echo $ATTERM_SESSION_CREATE_TEST_VAR > " + marker,
		},
	}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	evilCwd := filepath.Join(t.TempDir(), "does-not-exist")

	// The raw bytes a malicious or buggy sender could put on the wire. If
	// SessionCreatePayload ever grew a field for any of these, or if the
	// handler ever read one, this test would start failing. Today neither
	// is true: the struct has nowhere for them to land.
	raw := []byte(`{
		"request_id": "req-1",
		"host_id": "host-1",
		"profile_id": "profile-1",
		"command": "/bin/evil-shell",
		"cwd": "` + evilCwd + `",
		"env": {"ATTERM_SESSION_CREATE_TEST_VAR": "evil-value"},
		"startup_cmd": "echo pwned > ` + filepath.Join(safeCwd, "pwned") + `"
	}`)
	var req proto.SessionCreatePayload
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := proto.SessionCreatePayload{RequestID: "req-1", HostID: "host-1", ProfileID: "profile-1"}
	if req != want {
		t.Fatalf("decoded payload = %+v, want %+v — extra wire fields must not survive decode into SessionCreatePayload", req, want)
	}

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionFull)
	handler.run(req)

	resp := readSessionCreated(t, out)
	if !resp.OK {
		t.Fatalf("run() = %+v, want ok", resp)
	}
	sid := uuid.MustParse(resp.SessionID)
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		t.Fatalf("session %s not found", sid)
	}
	info := sess.Info()
	if info.Cwd != safeCwd {
		t.Fatalf("cwd = %q, want the profile's %q — the payload's cwd must be ignored", info.Cwd, safeCwd)
	}
	if strings.Contains(info.Command, "evil-shell") {
		t.Fatalf("command = %q, must not contain the payload's injected command", info.Command)
	}

	sess.PushOut(1, []byte("\x1b]133;A\x07$ "))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil {
			got := strings.TrimSpace(string(b))
			if got != "safe-value" {
				t.Fatalf("child saw env var %q, want the profile's %q — the payload's env must be ignored", got, "safe-value")
			}
			if _, err := os.Stat(filepath.Join(safeCwd, "pwned")); err == nil {
				t.Fatal("the payload's startup_cmd executed — it must be structurally impossible to smuggle one")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("profile startup command never ran (marker file never appeared)")
}

// TestSessionCreatePayloadWireShapeIsExactlyThreeFields pins the SHAPE of
// SessionCreatePayload structurally, not by name. A test that only checks
// req == SessionCreatePayload{RequestID: ..., HostID: ..., ProfileID: ...}
// (as TestSessionCreateHandler_PayloadCannotInfluenceSession above does)
// only catches a NEW field if the exploit happens to guess one of the three
// names it already knows about. A review proved this: adding a brand new
// Shell field (json tag "shell") to SessionCreatePayload, wiring the
// handler to fork with it, left every test in ./desktop, ./internal/proto
// and ./internal/relay green, because nothing pinned the field COUNT.
//
// This test marshals a fully-populated SessionCreatePayload and asserts the
// resulting JSON key set is exactly {request_id, host_id, profile_id} — and
// separately asserts the Go struct has exactly 3 fields via reflection — so
// ANY growth of the type trips it, regardless of what the new field is
// named or does.
func TestSessionCreatePayloadWireShapeIsExactlyThreeFields(t *testing.T) {
	typ := reflect.TypeOf(proto.SessionCreatePayload{})
	if typ.NumField() != 3 {
		var names []string
		for i := 0; i < typ.NumField(); i++ {
			names = append(names, typ.Field(i).Name)
		}
		t.Fatalf("proto.SessionCreatePayload has %d fields %v, want exactly 3 (request_id, host_id, profile_id) — "+
			"a phone must never be able to influence a created session through a field this test doesn't know about; "+
			"update this test deliberately if the type is meant to grow, and re-audit session_create_handler.go's run() "+
			"to confirm it still reads nothing but RequestID and ProfileID", typ.NumField(), names)
	}

	full := proto.SessionCreatePayload{RequestID: "r", HostID: "h", ProfileID: "p"}
	body, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(body, &keys); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"host_id", "profile_id", "request_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SessionCreatePayload wire keys = %v, want exactly %v — a new field changes what a phone can say, "+
			"and that must be a deliberate, reviewed change to this test, not a silent addition", got, want)
	}
}

// TestSessionCreateHandler_UnknownProfileRefused pins the "refused, not
// silently substituted" contract: a profile_id that isn't in local config
// must never fall through to the configured default (the leniency
// resolveSessionProfile grants a locally-typed session, for a different
// reason — see its doc comment) and must never fork a PTY at all.
func TestSessionCreateHandler_UnknownProfileRefused(t *testing.T) {
	h := newTestRelayHost(t)
	before := len(h.Snapshot())

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionFull)
	handler.run(proto.SessionCreatePayload{RequestID: "req-unknown", HostID: "host-1", ProfileID: "does-not-exist"})

	resp := readSessionCreated(t, out)
	if resp.OK || resp.Error != sessionCreateErrUnknownProfile {
		t.Fatalf("run() = %+v, want ok=false error=%q", resp, sessionCreateErrUnknownProfile)
	}
	if got := len(h.Snapshot()); got != before {
		t.Fatalf("session count = %d, want unchanged %d — an unknown profile id must not fork a PTY", got, before)
	}
}

// TestSessionCreateHandler_PermissionDeniedWithoutControl pins the review
// finding that remote_permission was never consulted at all for
// TypeSessionCreate: a desktop the owner published as view-only must not
// fork a shell (and potentially run a profile's startup_cmd) just because a
// request reached it. Forking a shell is at least as privileged as typing
// into one, so this uses the same control-or-full bar
// localFrameAllowedByPermission enforces for TypeIn/TypeResize.
func TestSessionCreateHandler_PermissionDeniedWithoutControl(t *testing.T) {
	h := newTestRelayHost(t)
	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh"}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}
	before := len(h.Snapshot())

	for _, perm := range []string{proto.RemotePermissionView, "", "garbage"} {
		out := make(chan proto.Frame, 4)
		handler := newSessionCreateHandler(context.Background(), out, h, perm)
		handler.run(proto.SessionCreatePayload{RequestID: "req-view-" + perm, HostID: "host-1", ProfileID: "p1"})

		resp := readSessionCreated(t, out)
		if resp.OK || resp.Error != sessionCreateErrPermissionDenied {
			t.Fatalf("permission=%q: run() = %+v, want ok=false error=%q — only control or full may fork a shell", perm, resp, sessionCreateErrPermissionDenied)
		}
	}
	if got := len(h.Snapshot()); got != before {
		t.Fatalf("session count = %d, want unchanged %d — a permission-denied request must never fork a PTY", got, before)
	}

	// Control (not just Full) must be allowed — it is at least as
	// privileged as TypeIn/TypeResize require, which this mirrors.
	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionControl)
	handler.run(proto.SessionCreatePayload{RequestID: "req-control", HostID: "host-1", ProfileID: "p1"})
	resp := readSessionCreated(t, out)
	if !resp.OK {
		t.Fatalf("run() with control permission = %+v, want ok", resp)
	}
}

// TestSessionCreateHandler_NewSessionErrorPropagated proves the handler
// does not duplicate NewSession's own profile validation (item 5): a
// profile that exists but can't actually start (here, a missing cwd — see
// TestRelayHost_NewSession_ProfileCwdMissingFailsNamingProfile for why cwd,
// unlike shell, gets no fallback) surfaces NewSession's own error, naming
// the profile, unchanged.
func TestSessionCreateHandler_NewSessionErrorPropagated(t *testing.T) {
	h := newTestRelayHost(t)

	missingCwd := filepath.Join(t.TempDir(), "no-such-dir")
	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "broken-cwd", Name: "Broken Cwd", Cwd: missingCwd}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionFull)
	handler.run(proto.SessionCreatePayload{RequestID: "req-bad-cwd", HostID: "host-1", ProfileID: "broken-cwd"})

	resp := readSessionCreated(t, out)
	if resp.OK {
		t.Fatalf("run() = %+v, want a failure — NewSession must refuse a missing profile cwd", resp)
	}
	if !strings.Contains(resp.Error, "Broken Cwd") || !strings.Contains(resp.Error, "broken-cwd") {
		t.Fatalf("error = %q, want it to name the profile (relay_host.go's own error, passed through unchanged)", resp.Error)
	}
}

// TestSessionCreateHandler_ConcurrencyBounded pins the desktop's own
// concurrency safety valve (Ruling A: this is a coarse per-desktop bound,
// not a per-client one — the true per-client dedup lives at the relay, see
// internal/relay/session_create_router_test.go). testHook parks each
// request mid-run so sessionCreateConcurrency requests genuinely arrive
// in flight together, proving submit() refuses the next one synchronously
// (not queues it), and that a slot is freed afterward so a fresh request
// can proceed.
func TestSessionCreateHandler_ConcurrencyBounded(t *testing.T) {
	h := newTestRelayHost(t)

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh"}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	out := make(chan proto.Frame, sessionCreateConcurrency+4)
	handler := newSessionCreateHandler(context.Background(), out, h, proto.RemotePermissionFull)

	started := make(chan struct{}, sessionCreateConcurrency+1)
	release := make(chan struct{})
	handler.testHook = func() {
		started <- struct{}{}
		<-release
	}

	for i := 0; i < sessionCreateConcurrency; i++ {
		handler.submit(proto.SessionCreatePayload{RequestID: fmt.Sprintf("req-%d", i), HostID: "host-1", ProfileID: "p1"})
	}
	for i := 0; i < sessionCreateConcurrency; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d requests reached run()", i, sessionCreateConcurrency)
		}
	}

	// One more, arriving while all sessionCreateConcurrency slots are
	// genuinely still in flight, must be refused immediately, not queued.
	handler.submit(proto.SessionCreatePayload{RequestID: "req-over", HostID: "host-1", ProfileID: "p1"})
	busy := readSessionCreated(t, out)
	if busy.RequestID != "req-over" || busy.OK || busy.Error != sessionCreateErrBusy {
		t.Fatalf("request over budget = %+v, want ok=false error=%q for req-over", busy, sessionCreateErrBusy)
	}
	select {
	case <-started:
		t.Fatal("a request over budget reached run(): the concurrency bound is not enforced")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	for i := 0; i < sessionCreateConcurrency; i++ {
		resp := readSessionCreated(t, out)
		if !resp.OK {
			t.Fatalf("in-flight request %+v, want ok", resp)
		}
	}

	// The slots must actually be freed, not just "the batch happened to
	// finish": wait on the real release(), then prove a fresh request goes
	// through.
	waitForSessionCreateInFlight(t, handler, 0)
	handler.testHook = nil
	handler.submit(proto.SessionCreatePayload{RequestID: "req-after", HostID: "host-1", ProfileID: "p1"})
	after := readSessionCreated(t, out)
	if after.RequestID != "req-after" || !after.OK {
		t.Fatalf("request after release = %+v, want ok for req-after", after)
	}
}

// TestSessionCreateHandler_ForkSurvivesConnectionCancellation pins CRITICAL
// 1: a review found a mobile-created session was killed by every uplink
// reconnect, because the handler forked using the uplink connection's own
// ctx (cancelled by runOnce's defer on every return — a routine event on
// that link) instead of the app's own lifetime ctx (relayHost.forkCtx),
// which local session creation (App.NewSession) already uses. This test
// cancels the CONNECTION-scoped ctx passed into the handler (simulating a
// reconnect) both before and during a fork, and asserts the resulting
// session is unaffected — still present in the registry and still
// forwardable to (no closed-channel panic on SendInbound), because the fork
// itself ran on forkCtx, not connCtx.
func TestSessionCreateHandler_ForkSurvivesConnectionCancellation(t *testing.T) {
	h := newTestRelayHost(t)
	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh"}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	connCtx, cancelConn := context.WithCancel(context.Background())
	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(connCtx, out, h, proto.RemotePermissionFull)

	// Simulate the connection dying (an uplink reconnect) the instant the
	// request is handed off, exactly like runOnce's `defer cancelConn()`
	// firing on every return. If the fork used connCtx, exec.CommandContext
	// and AdoptSession downstream in relayHost.NewSession would tear the
	// session down within moments of this.
	cancelConn()

	handler.run(proto.SessionCreatePayload{RequestID: "req-reconnect", HostID: "host-1", ProfileID: "p1"})

	// sendResult's own escape hatch also watches connCtx.Done(), so with
	// connCtx already cancelled the reply may or may not land on out — that
	// race is sendResult's documented, acceptable behavior (the connection
	// is gone; nothing is left to deliver to) and is not what this test is
	// about. What matters is what happened to the SESSION itself: forkCtx,
	// not connCtx, must have governed the fork.
	var sid uuid.UUID
	select {
	case f := <-out:
		var p proto.SessionCreatedPayload
		if err := json.Unmarshal(f.Payload, &p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !p.OK {
			t.Fatalf("run() = %+v, want ok — a cancelled CONNECTION ctx must not fail the fork itself", p)
		}
		sid = uuid.MustParse(p.SessionID)
	case <-time.After(500 * time.Millisecond):
		// sendResult raced connCtx.Done() and lost — acceptable per above.
		// Find the session directly via Snapshot() instead.
		snap := h.Snapshot()
		if len(snap) != 1 {
			t.Fatalf("Snapshot() = %d sessions, want exactly 1 — the fork must have succeeded despite the cancelled connCtx", len(snap))
		}
		sid = uuid.MustParse(snap[0].ID)
	}

	// Give a wrongly-scoped ctx a moment to have torn things down, the same
	// way the review's own probe did ("gone from the registry within
	// 1.5s").
	time.Sleep(200 * time.Millisecond)

	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		t.Fatalf("session %s missing from the registry after the connection ctx was cancelled — the fork must survive an uplink reconnect the same way a local tab does", sid)
	}
	// A closed-channel panic on SendInbound was the review's own proof the
	// session was actually dead, not just unlisted. Recover so a regression
	// here reports as a normal test failure instead of crashing the whole
	// suite.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SendInbound panicked (%v) — the session's underlying resources were torn down by the cancelled connection ctx", r)
			}
		}()
		sess.SendInbound(proto.Frame{Type: proto.TypeResize, SessionID: sid})
	}()
}

// TestDefaultShellForNewSession_HonorsConfiguredDefaultShell pins MAJOR 4: a
// review found defaultShellForNewSession was a hand-written twin of
// App.ListShells, independently able to drift (deleting its
// configured-default-shell branch left the whole ./desktop suite green).
// Both now call the single shared shellPriorityOrder (shell_resolve.go);
// this test exercises defaultShellForNewSession directly to pin that the
// configured default_shell still wins over $SHELL and the well-known-shell
// fallback list.
func TestDefaultShellForNewSession_HonorsConfiguredDefaultShell(t *testing.T) {
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this test machine")
	}
	h := newTestRelayHost(t)
	cfg := h.cfg.Get()
	cfg.DefaultShell = "sh"
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	got, err := defaultShellForNewSession(h.cfg)
	if err != nil {
		t.Fatalf("defaultShellForNewSession: %v", err)
	}
	if got != shPath {
		t.Fatalf("shell = %q, want the configured default %q — the configured default_shell must win over $SHELL and the well-known-shell fallback list", got, shPath)
	}
}

// TestDefaultShellForNewSession_MatchesListShellsFirstEntry is a drift
// guard: defaultShellForNewSession and App.ListShells must agree on which
// shell is "the default," since a mobile-created session should start the
// same shell a local tab would. Both now share shellPriorityOrder, so this
// mostly proves the extraction didn't drop a step rather than testing any
// one behavior in isolation.
func TestDefaultShellForNewSession_MatchesListShellsFirstEntry(t *testing.T) {
	h := newTestRelayHost(t)
	a := &App{cfgStore: h.cfg}
	shells := a.ListShells()
	if len(shells) == 0 {
		t.Skip("no shells found on this test machine")
	}

	got, err := defaultShellForNewSession(h.cfg)
	if err != nil {
		t.Fatalf("defaultShellForNewSession: %v", err)
	}
	if got != shells[0] {
		t.Fatalf("defaultShellForNewSession = %q, App.ListShells()[0] = %q — a mobile-created session must start the same shell a local tab would", got, shells[0])
	}
}
