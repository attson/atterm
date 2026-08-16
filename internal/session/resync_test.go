package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// A client that falls behind used to be dropped, which closed its websocket
// with "session ended" and sent it round the reconnect-replay loop — the thing
// that made a local terminal announce "reconnecting" while a flood was running.
// Shed the backlog and resync it in place instead: it stays attached, keeps the
// driver, and sees a jump rather than a disconnect.
func TestFanout_ResyncsInsteadOfDroppingOnOverflow(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	defer s.Close()

	sub, _ := s.Subscribe(0, "slow-client", "test")
	if !s.IsDriver(sub) {
		t.Fatal("setup: the first subscriber should hold the driver")
	}

	chunk := make([]byte, 8192)
	for i := range chunk {
		chunk[i] = 'y'
	}
	// Never drain sub.out: the shape of a renderer stuck parsing.
	for seq := uint64(1); seq <= subscriberQueueDepth*2; seq++ {
		s.PushOut(seq, chunk)
	}

	if s.SubscriberCount() != 1 {
		t.Fatal("a client that fell behind must stay attached")
	}
	if !s.IsDriver(sub) {
		t.Fatal("resync must not cost the client its driver role")
	}
	select {
	case <-sub.closed:
		t.Fatal("subscriber websocket must not be closed for being slow")
	default:
	}
}

// After a resync the queue holds a coherent restart — a reset marker followed
// by recent output — rather than a torn middle of the stream.
func TestFanout_ResyncQueuesResetThenTail(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	defer s.Close()

	sub, _ := s.Subscribe(0, "slow-client", "test")

	chunk := make([]byte, 8192)
	for i := range chunk {
		chunk[i] = 'y'
	}
	for seq := uint64(1); seq <= subscriberQueueDepth*2; seq++ {
		s.PushOut(seq, chunk)
	}

	// Drain what the client would read next.
	var frames []proto.Frame
	deadline := time.After(time.Second)
	for len(frames) < 2 {
		select {
		case f := <-sub.out:
			frames = append(frames, f)
		case <-deadline:
			t.Fatalf("only got %d frames after a resync", len(frames))
		}
	}
	if frames[0].Type != proto.TypeOut {
		t.Fatalf("first frame after resync = %v, want OUT", frames[0].Type)
	}
	if len(frames[0].Payload) == 0 {
		t.Fatal("the reset marker must carry the escape sequence that clears the view")
	}
}
