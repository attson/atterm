package main

import (
	"context"
	"testing"
)

func TestBeforeCloseEmitsAndPreventsByDefault(t *testing.T) {
	a := &App{}
	emitted := 0
	prevent := a.beforeClose(context.Background(), func() { emitted++ })
	if !prevent {
		t.Fatalf("prevent = false; want true (first close should be intercepted)")
	}
	if emitted != 1 {
		t.Fatalf("emitted = %d; want 1", emitted)
	}
}

func TestBeforeCloseAllowsQuitWhenApproved(t *testing.T) {
	a := &App{}
	a.quitApproved.Store(true)
	emitted := 0
	prevent := a.beforeClose(context.Background(), func() { emitted++ })
	if prevent {
		t.Fatalf("prevent = true; want false (approved quits must not be intercepted)")
	}
	if emitted != 0 {
		t.Fatalf("emitted = %d; want 0 (no event should fire when approved)", emitted)
	}
}
