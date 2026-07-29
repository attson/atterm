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
