package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/ringbuf"
	"github.com/google/uuid"
)

func chunksOf(n int, size int) []ringbuf.Chunk {
	out := make([]ringbuf.Chunk, 0, n)
	for i := 0; i < n; i++ {
		data := make([]byte, size)
		for j := range data {
			data[j] = byte('a' + i%26)
		}
		out = append(out, ringbuf.Chunk{Seq: uint64(i + 1), Data: data})
	}
	return out
}

// xterm keeps 20000 lines; the relay's ring holds 4 MiB. Replaying all of it
// makes the client parse megabytes it will immediately discard, which is both
// pure waste and what pushes a slow consumer over the edge into the
// drop-reconnect loop. Cap the replay at the tail that can actually be used.
func TestCapReplayTail_KeepsOnlyTheTail(t *testing.T) {
	chunks := chunksOf(100, 64*1024) // 6.4 MiB
	capped, dropped := capReplayTail(chunks)
	if !dropped {
		t.Fatal("6.4 MiB of scrollback must be capped")
	}
	if got := replayBytes(capped); got > replayTailCapBytes {
		t.Fatalf("capped replay = %d bytes, want <= %d", got, replayTailCapBytes)
	}
	// The tail is what matters: the newest chunk has to survive.
	if capped[len(capped)-1].Seq != chunks[len(chunks)-1].Seq {
		t.Fatal("capping must keep the newest chunks, not the oldest")
	}
}

func TestCapReplayTail_LeavesSmallReplaysAlone(t *testing.T) {
	chunks := chunksOf(4, 1024)
	capped, dropped := capReplayTail(chunks)
	if dropped {
		t.Fatal("a 4 KiB replay is nowhere near the cap")
	}
	if len(capped) != len(chunks) {
		t.Fatalf("capped %d chunks, want all %d", len(capped), len(chunks))
	}
}

// A single chunk larger than the cap still has to be sent: dropping it would
// leave the client with nothing at all.
func TestCapReplayTail_KeepsOneOversizedChunk(t *testing.T) {
	chunks := chunksOf(1, replayTailCapBytes*2)
	capped, dropped := capReplayTail(chunks)
	if len(capped) != 1 {
		t.Fatalf("capped to %d chunks, want the one chunk kept", len(capped))
	}
	if !dropped {
		t.Fatal("an oversized single chunk is still a torn view; say so")
	}
}

func TestCapReplayTail_EmptyIsFine(t *testing.T) {
	capped, dropped := capReplayTail(nil)
	if len(capped) != 0 || dropped {
		t.Fatal("no chunks means nothing to cap")
	}
}

// End to end: a session whose ring is full replays only the capped tail, so the
// client is not handed megabytes it would parse and immediately discard.
func TestSubscribe_ReplaysOnlyTheCappedTail(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	defer s.Close()

	chunk := make([]byte, 64*1024)
	for i := range chunk {
		chunk[i] = 'y'
	}
	for seq := uint64(1); seq <= 100; seq++ { // 6.4 MiB in, ring holds 4 MiB
		s.PushOut(seq, chunk)
	}

	sub, _ := s.Subscribe(0, "client", "test")
	defer s.removeSubscriber(sub)

	var out uint64
	for {
		select {
		case f := <-sub.out:
			if f.Type == proto.TypeOut {
				out += uint64(len(f.Payload))
			}
			continue
		default:
		}
		break
	}
	// Payloads carry a seq header, so allow a little slack over the cap.
	if out > replayTailCapBytes+64*1024 {
		t.Fatalf("replayed %d bytes, want at most the %d byte tail", out, replayTailCapBytes)
	}
	if out == 0 {
		t.Fatal("replayed nothing; the tail must still arrive")
	}
}
