package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestRelayHost_NewSession_AIKindKicksSniff(t *testing.T) {
	h := newTestRelayHost(t)

	var mu sync.Mutex
	var sniffStarted bool
	var capturedKind string
	h.startSniffFn = func(_ context.Context, _ *session.Session, _, kind string, _ func(string)) {
		mu.Lock()
		defer mu.Unlock()
		sniffStarted = true
		capturedKind = kind
	}

	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh",
		Cwd:     t.TempDir(),
		Cols:    80, Rows: 24,
		AIKind: "claude",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !sniffStarted {
		t.Fatal("expected sniff to start when AIKind is claude")
	}
	if capturedKind != "claude" {
		t.Fatalf("kind = %q want %q", capturedKind, "claude")
	}
}

func TestRelayHost_NewSession_RestoredInitialAISessionIDCapturedImmediately(t *testing.T) {
	h := newTestRelayHost(t)

	var mu sync.Mutex
	var gotLocal string
	var gotKind string
	var gotAISid string
	h.aiSidCallback = func(localSessionID uuid.UUID, kind, aiSid string) {
		mu.Lock()
		defer mu.Unlock()
		gotLocal = localSessionID.String()
		gotKind = kind
		gotAISid = aiSid
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command:            "/bin/sh",
		Cwd:                t.TempDir(),
		Cols:               80,
		Rows:               24,
		AIKind:             "codex",
		InitialAISessionID: "019fae95-43eb-7491-9341-05c156228664",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := gotLocal == id.String() && gotKind == "codex" && gotAISid == "019fae95-43eb-7491-9341-05c156228664"
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("captured (%q,%q,%q), want (%s,codex,019fae95-43eb-7491-9341-05c156228664)", gotLocal, gotKind, gotAISid, id)
}

func TestRelayHost_NewSession_NoAIKind_NoSniff(t *testing.T) {
	h := newTestRelayHost(t)
	var mu sync.Mutex
	var called bool
	h.startSniffFn = func(_ context.Context, _ *session.Session, _, _ string, _ func(string)) {
		mu.Lock()
		defer mu.Unlock()
		called = true
	}
	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: t.TempDir(), Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("sniff should not start when AIKind is empty")
	}
}

func TestRelayHost_NewSession_OnAIClassified_StartsSniff(t *testing.T) {
	h := newTestRelayHost(t)

	var mu sync.Mutex
	var captured []string
	h.startSniffFn = func(_ context.Context, _ *session.Session, cwd, kind string, _ func(string)) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, kind+"@"+cwd)
	}

	cwd := t.TempDir()
	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh",
		Cwd:     cwd,
		Cols:    80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("\x1b]133;C;claude --foo\x07"))

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("expected 1 sniff start, got %d (captured=%v)", len(captured), captured)
	}
	if captured[0] != "claude@"+cwd {
		t.Fatalf("expected claude@%s, got %s", cwd, captured[0])
	}
}

func TestRelayHost_NewSession_LatestAICommandOwnsRecoveryGeneration(t *testing.T) {
	h := newTestRelayHost(t)

	type resolver struct {
		kind    string
		ctx     context.Context
		capture func(string)
	}
	started := make(chan resolver, 4)
	h.startSniffFn = func(ctx context.Context, _ *session.Session, _ string, kind string, capture func(string)) {
		started <- resolver{kind: kind, ctx: ctx, capture: capture}
		<-ctx.Done()
	}
	type event struct{ kind, sid string }
	events := make(chan event, 8)
	h.aiSidCallback = func(_ uuid.UUID, kind, sid string) {
		events <- event{kind: kind, sid: sid}
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: t.TempDir(), Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("\x1b]133;C;claude\x07"))
	first := receiveAIEvent(t, started)
	if first.kind != "claude" {
		t.Fatalf("first resolver kind = %q, want claude", first.kind)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: ""}) {
		t.Fatalf("first event = %+v, want claude clear", got)
	}
	first.capture("claude-old")
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: "claude-old"}) {
		t.Fatalf("claude capture = %+v", got)
	}

	sess.PushOut(2, []byte("\x1b]133;D;0\x07"))
	sess.PushOut(3, []byte("\x1b]133;C;codex\x07"))
	second := receiveAIEvent(t, started)
	if second.kind != "codex" {
		t.Fatalf("second resolver kind = %q, want codex", second.kind)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "codex", sid: ""}) {
		t.Fatalf("switch event = %+v, want codex clear", got)
	}
	select {
	case <-first.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first resolver was not cancelled on AI switch")
	}

	// A cancelled resolver may still race a final callback. It must not restore
	// the old Claude credential after Codex became the latest generation.
	first.capture("claude-too-late")
	select {
	case got := <-events:
		t.Fatalf("stale resolver published event %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	second.capture("codex-new")
	if got := receiveAIEvent(t, events); got != (event{kind: "codex", sid: "codex-new"}) {
		t.Fatalf("codex capture = %+v", got)
	}

	// Same-kind relaunch is still a new conversation generation.
	sess.PushOut(4, []byte("\x1b]133;D;0\x07"))
	sess.PushOut(5, []byte("\x1b]133;C;codex\x07"))
	third := receiveAIEvent(t, started)
	if third.kind != "codex" {
		t.Fatalf("third resolver kind = %q, want codex", third.kind)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "codex", sid: ""}) {
		t.Fatalf("same-kind switch event = %+v, want codex clear", got)
	}
	select {
	case <-second.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("previous same-kind resolver was not cancelled")
	}
	second.capture("codex-a-too-late")
	select {
	case got := <-events:
		t.Fatalf("stale same-kind resolver published event %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	third.capture("codex-b")
	if got := receiveAIEvent(t, events); got != (event{kind: "codex", sid: "codex-b"}) {
		t.Fatalf("latest same-kind capture = %+v", got)
	}

	// The inverse cross-kind direction follows the same last-generation-wins
	// rule (the screenshot reproduction can happen in either order).
	sess.PushOut(6, []byte("\x1b]133;D;0\x07"))
	sess.PushOut(7, []byte("\x1b]133;C;claude\x07"))
	fourth := receiveAIEvent(t, started)
	if fourth.kind != "claude" {
		t.Fatalf("fourth resolver kind = %q, want claude", fourth.kind)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: ""}) {
		t.Fatalf("reverse switch event = %+v, want claude clear", got)
	}
	select {
	case <-third.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("codex resolver was not cancelled on reverse switch")
	}
	third.capture("codex-too-late-after-claude")
	select {
	case got := <-events:
		t.Fatalf("reverse-switch stale resolver published event %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	fourth.capture("claude-new")
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: "claude-new"}) {
		t.Fatalf("reverse-switch claude capture = %+v", got)
	}
}

func TestRelayHost_NewSession_RestoredResumeOSCConfirmsExistingGeneration(t *testing.T) {
	h := newTestRelayHost(t)

	started := make(chan string, 4)
	h.startSniffFn = func(ctx context.Context, _ *session.Session, _ string, kind string, _ func(string)) {
		started <- kind
		<-ctx.Done()
	}
	type event struct{ kind, sid string }
	events := make(chan event, 4)
	h.aiSidCallback = func(_ uuid.UUID, kind, sid string) { events <- event{kind: kind, sid: sid} }

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command:            "/bin/sh",
		Cwd:                t.TempDir(),
		Cols:               80,
		Rows:               24,
		AIKind:             "claude",
		InitialAISessionID: "claude-restored",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: "claude-restored"}) {
		t.Fatalf("initial recovery event = %+v", got)
	}
	if got := receiveAIEvent(t, started); got != "claude" {
		t.Fatalf("initial resolver = %q", got)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	sess.PushOut(1, []byte("\x1b]133;C;claude --resume claude-restored\x07"))
	select {
	case got := <-started:
		t.Fatalf("restored resume OSC started duplicate resolver %q", got)
	case got := <-events:
		t.Fatalf("restored resume OSC republished/cleared credential %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	// The confirmation exemption is one-shot. A later Claude launch is a new
	// generation and must clear the restored credential.
	sess.PushOut(2, []byte("\x1b]133;D;0\x07"))
	sess.PushOut(3, []byte("\x1b]133;C;claude\x07"))
	if got := receiveAIEvent(t, started); got != "claude" {
		t.Fatalf("second resolver = %q", got)
	}
	if got := receiveAIEvent(t, events); got != (event{kind: "claude", sid: ""}) {
		t.Fatalf("second launch event = %+v, want clear", got)
	}
}

func TestRelayHost_NewSession_UnresolvedRestoredKindDoesNotConsumeManualLaunch(t *testing.T) {
	h := newTestRelayHost(t)

	started := make(chan string, 4)
	h.startSniffFn = func(ctx context.Context, _ *session.Session, _ string, kind string, _ func(string)) {
		started <- kind
		<-ctx.Done()
	}

	id, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh", Cwd: t.TempDir(), Cols: 80, Rows: 24, AIKind: "claude",
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := receiveAIEvent(t, started); got != "claude" {
		t.Fatalf("initial resolver = %q", got)
	}
	sess, ok := h.server.Registry().Get(id)
	if !ok {
		t.Fatalf("session %s not found", id)
	}

	// No SID means no resume command was injected, so the next same-kind OSC is
	// a real user launch and must replace the speculative restored resolver.
	sess.PushOut(1, []byte("\x1b]133;C;claude\x07"))
	if got := receiveAIEvent(t, started); got != "claude" {
		t.Fatalf("manual launch resolver = %q", got)
	}
}

func receiveAIEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for AI recovery event")
		var zero T
		return zero
	}
}
