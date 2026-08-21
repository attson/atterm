package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
// that acquire() blocked — see TestSessionCreateHandler_OneInFlightPerConnection.
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
	handler := newSessionCreateHandler(context.Background(), out, h, 0)
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
	handler := newSessionCreateHandler(context.Background(), out, h, 0)
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

// TestSessionCreateHandler_UnknownProfileRefused pins the "refused, not
// silently substituted" contract: a profile_id that isn't in local config
// must never fall through to the configured default (the leniency
// resolveSessionProfile grants a locally-typed session, for a different
// reason — see its doc comment) and must never fork a PTY at all.
func TestSessionCreateHandler_UnknownProfileRefused(t *testing.T) {
	h := newTestRelayHost(t)
	before := h.SessionCount()

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, 0)
	handler.run(proto.SessionCreatePayload{RequestID: "req-unknown", HostID: "host-1", ProfileID: "does-not-exist"})

	resp := readSessionCreated(t, out)
	if resp.OK || resp.Error != sessionCreateErrUnknownProfile {
		t.Fatalf("run() = %+v, want ok=false error=%q", resp, sessionCreateErrUnknownProfile)
	}
	if got := h.SessionCount(); got != before {
		t.Fatalf("session count = %d, want unchanged %d — an unknown profile id must not fork a PTY", got, before)
	}
}

// TestSessionCreateHandler_SessionCapReached pins design §4's per-host cap:
// once the host is already at capacity, a create request is refused with a
// distinct error rather than forking another PTY (and rather than being
// dropped, which would just leave the phone to time out with no idea why).
func TestSessionCreateHandler_SessionCapReached(t *testing.T) {
	h := newTestRelayHost(t)

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh"}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	// Occupy the cap through NewSession directly (not through the handler),
	// so the cap check is exercised against the live session count itself,
	// not anything handler-specific.
	if _, err := h.NewSession(context.Background(), NewSessionReq{Command: "/bin/sh", Cwd: t.TempDir(), Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	before := h.SessionCount()

	out := make(chan proto.Frame, 4)
	handler := newSessionCreateHandler(context.Background(), out, h, before) // cap == already-live count
	handler.run(proto.SessionCreatePayload{RequestID: "req-cap", HostID: "host-1", ProfileID: "p1"})

	resp := readSessionCreated(t, out)
	if resp.OK || resp.Error != sessionCreateErrCapReached {
		t.Fatalf("run() = %+v, want ok=false error=%q", resp, sessionCreateErrCapReached)
	}
	if got := h.SessionCount(); got != before {
		t.Fatalf("session count = %d, want unchanged %d — a capped request must not fork a PTY", got, before)
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
	handler := newSessionCreateHandler(context.Background(), out, h, 0)
	handler.run(proto.SessionCreatePayload{RequestID: "req-bad-cwd", HostID: "host-1", ProfileID: "broken-cwd"})

	resp := readSessionCreated(t, out)
	if resp.OK {
		t.Fatalf("run() = %+v, want a failure — NewSession must refuse a missing profile cwd", resp)
	}
	if !strings.Contains(resp.Error, "Broken Cwd") || !strings.Contains(resp.Error, "broken-cwd") {
		t.Fatalf("error = %q, want it to name the profile (relay_host.go's own error, passed through unchanged)", resp.Error)
	}
}

// TestSessionCreateHandler_OneInFlightPerConnection pins design §4's "a
// phone that taps twice does not fork two shells." testHook parks the first
// request mid-run so a second request genuinely arrives while the first is
// in flight, proving submit() refuses it synchronously (not queues it), and
// that the slot is freed afterward so a third request can proceed.
func TestSessionCreateHandler_OneInFlightPerConnection(t *testing.T) {
	h := newTestRelayHost(t)

	cfg := h.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1", Shell: "/bin/sh"}}
	if err := h.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}

	out := make(chan proto.Frame, 8)
	handler := newSessionCreateHandler(context.Background(), out, h, 0)

	started := make(chan struct{})
	release := make(chan struct{})
	handler.testHook = func() {
		started <- struct{}{}
		<-release
	}

	handler.submit(proto.SessionCreatePayload{RequestID: "req-1", HostID: "host-1", ProfileID: "p1"})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request never reached run()")
	}

	// Second request arrives while the first is genuinely still in flight
	// (parked inside testHook). submit() must refuse it immediately.
	handler.submit(proto.SessionCreatePayload{RequestID: "req-2", HostID: "host-1", ProfileID: "p1"})
	busy := readSessionCreated(t, out)
	if busy.RequestID != "req-2" || busy.OK || busy.Error != sessionCreateErrBusy {
		t.Fatalf("second (concurrent) request = %+v, want ok=false error=%q for req-2", busy, sessionCreateErrBusy)
	}

	close(release)
	first := readSessionCreated(t, out)
	if first.RequestID != "req-1" || !first.OK {
		t.Fatalf("first request = %+v, want ok for req-1", first)
	}

	// The slot must actually be freed, not just "the first happened to
	// finish": wait on the real release(), then prove a third request goes
	// through.
	waitForSessionCreateInFlight(t, handler, 0)
	handler.testHook = nil
	handler.submit(proto.SessionCreatePayload{RequestID: "req-3", HostID: "host-1", ProfileID: "p1"})
	third := readSessionCreated(t, out)
	if third.RequestID != "req-3" || !third.OK {
		t.Fatalf("third request after release = %+v, want ok for req-3", third)
	}
}
