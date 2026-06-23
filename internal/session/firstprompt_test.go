package session

import (
	"sync/atomic"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// The first OSC 133 prompt marker (A or B) must fire onFirstPrompt exactly
// once — restored AI panes rely on this to inject their resume command.
func TestSetOnFirstPrompt_FiresOnceOnPromptMarker(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	var n int32
	s.SetOnFirstPrompt(func() { atomic.AddInt32(&n, 1) })

	// First prompt cycle: A ... B.
	s.PushOut(1, []byte("\x1b]133;A\x07# prompt $ \x1b]133;B\x07"))
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("after first prompt: fired %d times, want 1", got)
	}

	// Subsequent prompts must NOT fire again.
	s.PushOut(2, []byte("\x1b]133;A\x07# prompt $ \x1b]133;B\x07"))
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("after second prompt: fired %d times, want 1", got)
	}
}

// No prompt marker → never fires (e.g. a shell without integration).
func TestSetOnFirstPrompt_NoMarkerNoFire(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	var fired bool
	s.SetOnFirstPrompt(func() { fired = true })
	s.PushOut(1, []byte("plain output, no osc133\n"))
	if fired {
		t.Fatal("onFirstPrompt fired without any prompt marker")
	}
}
