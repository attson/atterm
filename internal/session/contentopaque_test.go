package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// osc133PromptSequence is a real OSC 133 prompt-start escape. The
// internal parser at minimum accumulates this into osc133Buf when the
// session is not content-opaque; when opaque, no parsing happens at
// all and osc133Buf must remain empty.
var osc133PromptSequence = []byte("\x1b]133;A\x07")

func newSessionForTest(t *testing.T) *Session {
	t.Helper()
	return New(uuid.New(), proto.SessionInfo{
		Cols:      80,
		Rows:      24,
		TaskState: proto.TaskStateIdle,
	})
}

// TestPushOut_NotOpaque_FeedsOSCBuffer confirms the OSC parser is
// actually receiving the bytes on a non-opaque session — when osc133Buf
// contains a partial OSC after PushOut, the parser is doing its job.
// Baseline for the opaque-branch suppression test below.
func TestPushOut_NotOpaque_FeedsOSCBuffer(t *testing.T) {
	s := newSessionForTest(t)
	// Push the OSC prefix without the terminator — the parser must hold
	// the partial sequence in osc133Buf so a terminator that lands on the
	// next chunk still parses.
	s.PushOut(1, []byte("\x1b]133;"))
	s.mu.RLock()
	bufLen := len(s.osc133Buf)
	s.mu.RUnlock()
	if bufLen == 0 {
		t.Fatalf("osc133Buf empty after partial OSC; parser did not run")
	}
}

// TestPushOut_Opaque_SkipsOSCParser confirms MarkContentOpaque suppresses
// the OSC parser entirely. After PushOut the osc133Buf must stay empty
// even when the bytes look like a real partial OSC 133 sequence — when
// the relay sees ciphertext, it must not try to read any structure from
// the bytes.
func TestPushOut_Opaque_SkipsOSCParser(t *testing.T) {
	s := newSessionForTest(t)
	s.MarkContentOpaque()
	s.PushOut(1, []byte("\x1b]133;"))
	s.mu.RLock()
	bufLen := len(s.osc133Buf)
	s.mu.RUnlock()
	if bufLen != 0 {
		t.Fatalf("osc133Buf has %d bytes on opaque session; parser ran when it shouldn't have", bufLen)
	}
}

// TestPushOut_Opaque_StillFansOutAndStoresRingbuf asserts the bypass is
// surgical: ringbuf storage and subscriber fan-out remain unchanged, so
// downstream clients still receive the (ciphertext) bytes and can
// replay scrollback. The subscriber receives replay-progress frames
// before live OUT — drain past them and check the OUT chunk.
func TestPushOut_Opaque_StillFansOutAndStoresRingbuf(t *testing.T) {
	s := newSessionForTest(t)
	s.MarkContentOpaque()

	sub, _ := s.Subscribe(0, "client-1", "client-1")

	payload := []byte("opaque-bytes")
	s.PushOut(7, payload)

	deadline := time.After(time.Second)
	for {
		select {
		case f := <-sub.Out():
			if f.Type != proto.TypeOut {
				// Drain META / REPLAY_PROGRESS / etc. emitted before the live OUT.
				continue
			}
			seq, data, err := proto.DecodeOut(f.Payload)
			if err != nil {
				t.Fatalf("DecodeOut: %v", err)
			}
			if seq != 7 {
				t.Fatalf("seq = %d, want 7", seq)
			}
			if string(data) != string(payload) {
				t.Fatalf("fan-out data differs from input")
			}
			return
		case <-deadline:
			t.Fatalf("subscriber did not receive OUT frame within 1s")
		}
	}
}

// TestMarkContentOpaque_Idempotent: calling twice is fine and remains
// opaque. Mostly a regression guard against a future "unset on second
// call" refactor.
func TestMarkContentOpaque_Idempotent(t *testing.T) {
	s := newSessionForTest(t)
	s.MarkContentOpaque()
	s.MarkContentOpaque()
	s.PushOut(1, osc133PromptSequence)
	s.mu.RLock()
	bufLen := len(s.osc133Buf)
	s.mu.RUnlock()
	if bufLen != 0 {
		t.Fatalf("parser ran after double-mark; osc133Buf has %d bytes", bufLen)
	}
}
