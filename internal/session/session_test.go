package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestUpdateSizeChangesSessionInfo(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	s.UpdateSize(132, 43)

	info := s.Info()
	if info.Cols != 132 || info.Rows != 43 {
		t.Fatalf("size = %dx%d; want 132x43", info.Cols, info.Rows)
	}
}

func TestSubscribeEmitsReplayProgress(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	first := []byte("first")
	second := make([]byte, replayProgressIntervalBytes)
	s.PushOut(1, first)
	s.PushOut(2, second)

	sub, _ := s.Subscribe(0)
	defer s.Unsubscribe(sub)

	start := readFrameForTest(t, sub)
	if start.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", start.Type)
	}
	startProgress := decodeProgressForTest(t, start)
	total := uint64(len(first) + len(second))
	if startProgress.Phase != proto.ReplayProgressStart || startProgress.Bytes != 0 || startProgress.TotalBytes != total {
		t.Fatalf("start progress = %+v; want phase=start bytes=0 total=%d", startProgress, total)
	}

	if got := readFrameForTest(t, sub); got.Type != proto.TypeOut {
		t.Fatalf("second frame type = 0x%02x; want OUT", got.Type)
	}
	if got := readFrameForTest(t, sub); got.Type != proto.TypeOut {
		t.Fatalf("third frame type = 0x%02x; want OUT", got.Type)
	}

	chunk := readFrameForTest(t, sub)
	if chunk.Type != proto.TypeReplayProgress {
		t.Fatalf("fourth frame type = 0x%02x; want REPLAY_PROGRESS", chunk.Type)
	}
	chunkProgress := decodeProgressForTest(t, chunk)
	if chunkProgress.Phase != proto.ReplayProgressChunk || chunkProgress.Bytes != total || chunkProgress.TotalBytes != total || chunkProgress.Seq != 2 {
		t.Fatalf("chunk progress = %+v; want phase=chunk bytes=%d total=%d seq=2", chunkProgress, total, total)
	}

	end := readFrameForTest(t, sub)
	if end.Type != proto.TypeReplayProgress {
		t.Fatalf("fifth frame type = 0x%02x; want REPLAY_PROGRESS", end.Type)
	}
	endProgress := decodeProgressForTest(t, end)
	if endProgress.Phase != proto.ReplayProgressEnd || endProgress.Bytes != total || endProgress.TotalBytes != total || endProgress.Seq != 2 {
		t.Fatalf("end progress = %+v; want phase=end bytes=%d total=%d seq=2", endProgress, total, total)
	}
}

func TestSubscribeFromStartMarksTruncatedScrollback(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	large := make([]byte, scrollbackBytes/2+1)
	s.PushOut(1, large)
	s.PushOut(2, large)
	s.PushOut(3, large)
	if oldest := s.scroll.OldestSeq(); oldest <= 1 {
		t.Fatalf("oldest seq = %d; want scrollback truncated past seq 1", oldest)
	}

	sub, _ := s.Subscribe(0)
	defer s.Unsubscribe(sub)

	if got := readFrameForTest(t, sub); got.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", got.Type)
	}
	reset := readFrameForTest(t, sub)
	if reset.Type != proto.TypeOut {
		t.Fatalf("second frame type = 0x%02x; want OUT reset marker", reset.Type)
	}
	seq, data, err := proto.DecodeOut(reset.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 0 || string(data) != "\x1b[2J\x1b[H" {
		t.Fatalf("reset marker = seq %d data %q; want seq 0 clear/home", seq, data)
	}
}

func TestSubscribeTruncatedAltScreenReplayRestoresAltScreenMode(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})
	s.PushOut(1, []byte("\x1b[?1049h"))
	large := make([]byte, scrollbackBytes/2+1)
	s.PushOut(2, large)
	s.PushOut(3, large)
	s.PushOut(4, large)
	if oldest := s.scroll.OldestSeq(); oldest <= 1 {
		t.Fatalf("oldest seq = %d; want scrollback truncated past alt-screen enter", oldest)
	}

	sub, _ := s.Subscribe(0)
	defer s.Unsubscribe(sub)

	if got := readFrameForTest(t, sub); got.Type != proto.TypeReplayProgress {
		t.Fatalf("first frame type = 0x%02x; want REPLAY_PROGRESS", got.Type)
	}
	reset := readFrameForTest(t, sub)
	seq, data, err := proto.DecodeOut(reset.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 0 || string(data) != "\x1b[?1049h\x1b[2J\x1b[H" {
		t.Fatalf("reset marker = seq %d data %q; want seq 0 alt-screen clear/home", seq, data)
	}
}

func TestAltScreenTrackingHandlesSplitEscapeSequence(t *testing.T) {
	id := uuid.New()
	s := New(id, proto.SessionInfo{Cols: 80, Rows: 24})

	s.PushOut(1, []byte("\x1b[?"))
	s.PushOut(2, []byte("10"))
	s.PushOut(3, []byte("49h"))
	if !s.altScreen {
		t.Fatal("altScreen = false; want true after split ?1049h")
	}
	s.PushOut(4, []byte("\x1b[?1049l"))
	if s.altScreen {
		t.Fatal("altScreen = true; want false after ?1049l")
	}
}

func readFrameForTest(t *testing.T, sub *Subscriber) proto.Frame {
	t.Helper()
	select {
	case f := <-sub.Out():
		return f
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber frame")
		return proto.Frame{}
	}
}

func decodeProgressForTest(t *testing.T, f proto.Frame) proto.ReplayProgressPayload {
	t.Helper()
	var p proto.ReplayProgressPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
