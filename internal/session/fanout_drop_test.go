package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// A client that cannot keep up is dropped by the live fan-out, which expects it
// to reconnect. That contract is cheap only while the scrollback still covers
// the gap: a flood wraps the 4 MiB ring in seconds, so the reconnect replays
// the whole ring, falls behind again, and is dropped again — which is what puts
// a replay progress bar on screen with no tab switch and no re-attach by the
// user.
func TestFanout_DropsSubscriberThatCannotKeepUp(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	defer s.Close()

	sub, _ := s.Subscribe(0, "slow-client", "test")
	if s.SubscriberCount() != 1 {
		t.Fatalf("setup: subscriber count = %d, want 1", s.SubscriberCount())
	}

	// Never read sub.out — the shape of a client whose main thread is busy
	// parsing megabytes of output.
	chunk := make([]byte, 4096)
	for i := range chunk {
		chunk[i] = 'y'
	}
	deadline := time.Now().Add(2 * time.Second)
	for seq := uint64(1); s.SubscriberCount() > 0 && time.Now().Before(deadline); seq++ {
		s.PushOut(seq, chunk)
	}

	if s.SubscriberCount() != 0 {
		t.Fatal("a subscriber that never drains was not dropped; the reconnect loop theory needs revisiting")
	}
	select {
	case <-sub.closed:
	default:
		t.Fatal("dropped subscriber should be closed so the client learns to reconnect")
	}
}
