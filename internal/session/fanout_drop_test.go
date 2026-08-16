package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// One client that stops reading must never hold up the PTY or the other
// clients. It gets shed and resynced (see resync_test.go); what matters here is
// that the fan-out itself stays non-blocking while that happens.
func TestFanout_NeverBlocksOnAClientThatStoppedReading(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	defer s.Close()

	stuck, _ := s.Subscribe(0, "stuck-client", "test")
	_ = stuck // deliberately never drained

	chunk := make([]byte, 8192)
	for i := range chunk {
		chunk[i] = 'y'
	}

	done := make(chan struct{})
	go func() {
		for seq := uint64(1); seq <= subscriberQueueDepth*3; seq++ {
			s.PushOut(seq, chunk)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PushOut blocked on a client that stopped reading")
	}
	if s.SubscriberCount() != 1 {
		t.Fatal("the client should still be attached after being resynced")
	}
}
