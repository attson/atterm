package relay

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestReplayPacerPausesOnlyDuringReplay(t *testing.T) {
	id := uuid.New()
	pacer := newReplayPacer(10)

	if pacer.observe(proto.EncodeOut(id, 1, []byte("outside replay"))) {
		t.Fatal("pacer paused before replay start")
	}
	if pacer.observe(replayProgressFrameForTest(t, id, proto.ReplayProgressStart, 0, 20, 0)) {
		t.Fatal("pacer paused on replay start frame")
	}
	if pacer.observe(proto.EncodeOut(id, 2, []byte("12345"))) {
		t.Fatal("pacer paused before threshold")
	}
	if !pacer.observe(proto.EncodeOut(id, 3, []byte("67890"))) {
		t.Fatal("pacer did not pause at threshold")
	}
	if pacer.observe(replayProgressFrameForTest(t, id, proto.ReplayProgressEnd, 20, 20, 3)) {
		t.Fatal("pacer paused on replay end frame")
	}
	if pacer.observe(proto.EncodeOut(id, 4, []byte("after replay is live"))) {
		t.Fatal("pacer paused after replay end")
	}
}

func replayProgressFrameForTest(t *testing.T, id uuid.UUID, phase string, bytes, total, seq uint64) proto.Frame {
	t.Helper()
	payload, err := json.Marshal(proto.ReplayProgressPayload{
		Phase:      phase,
		Bytes:      bytes,
		TotalBytes: total,
		Seq:        seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	return proto.Frame{Type: proto.TypeReplayProgress, SessionID: id, Payload: payload}
}
