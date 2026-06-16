package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRelayHost_NewSession_AIKindKicksSniff(t *testing.T) {
	h := newTestRelayHost(t)

	var mu sync.Mutex
	var sniffStarted bool
	var capturedKind string
	h.startSniffFn = func(_ context.Context, _, kind string, _ func(string)) {
		mu.Lock()
		defer mu.Unlock()
		sniffStarted = true
		capturedKind = kind
	}

	_, err := h.NewSession(context.Background(), NewSessionReq{
		Command: "/bin/sh",
		Cwd:     t.TempDir(),
		Cols:    80, Rows: 24,
		AIKind:  "claude",
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

func TestRelayHost_NewSession_NoAIKind_NoSniff(t *testing.T) {
	h := newTestRelayHost(t)
	var mu sync.Mutex
	var called bool
	h.startSniffFn = func(_ context.Context, _, _ string, _ func(string)) {
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
