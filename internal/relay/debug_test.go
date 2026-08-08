package relay

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// lockedBuffer collects debug output. relay writes from connection
// goroutines, so the collector has to be safe under -race.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureDebugLog redirects the shared logger to a buffer for one test.
// The relay's debug output has no per-server sink any more — it goes through
// internal/logging like everything else.
func captureDebugLog(t *testing.T) *lockedBuffer {
	t.Helper()
	prev := logging.CurrentLevel()
	t.Cleanup(func() {
		logging.SetSink(nil)
		logging.SetLevel(prev)
	})
	buf := &lockedBuffer{}
	logging.SetSink(buf)
	return buf
}

func TestDebugFrameDisabledWritesNothing(t *testing.T) {
	buf := captureDebugLog(t)
	s := NewServer(Config{})

	s.debugFrame("client", "recv", proto.Frame{Type: proto.TypeList})

	if got := buf.String(); got != "" {
		t.Fatalf("debug disabled wrote %q; want empty", got)
	}
}

func TestDebugFrameSummarizesInOutWithoutPayload(t *testing.T) {
	buf := captureDebugLog(t)
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true})

	s.debugFrame("client", "recv", proto.Frame{
		Type:      proto.TypeIn,
		SessionID: id,
		Payload:   []byte("secret command\n"),
	})
	s.debugFrame("agent", "recv", proto.EncodeOut(id, 42, []byte("secret output\n")))

	got := buf.String()
	for _, want := range []string{
		"[relay-debug] client recv IN session=11111111-1111-1111-1111-111111111111 bytes=15",
		"[relay-debug] agent recv OUT session=11111111-1111-1111-1111-111111111111 seq=42 bytes=14",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "secret command") || strings.Contains(got, "secret output") {
		t.Fatalf("payload leaked with DebugPayload=false: %q", got)
	}
}

func TestDebugFrameCanIncludeInOutPayload(t *testing.T) {
	buf := captureDebugLog(t)
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true, DebugPayload: true})

	s.debugFrame("client", "recv", proto.Frame{
		Type:      proto.TypeIn,
		SessionID: id,
		Payload:   []byte("echo hi\n"),
	})
	s.debugFrame("agent", "recv", proto.EncodeOut(id, 7, []byte("ok\r\n")))

	got := buf.String()
	for _, want := range []string{
		`payload="echo hi\n"`,
		`payload="ok\r\n"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q missing %q", got, want)
		}
	}
}

func TestDebugFrameSummarizesStructuredFrames(t *testing.T) {
	buf := captureDebugLog(t)
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true})

	s.debugFrame("client", "recv", proto.Frame{
		Type:      proto.TypeResize,
		SessionID: id,
		Payload:   proto.EncodeResize(120, 36),
	})
	s.debugFrame("client", "recv", proto.Frame{
		Type:      proto.TypeAttach,
		SessionID: id,
		Payload:   []byte(`{"session_id":"11111111-1111-1111-1111-111111111111","since_seq":9}`),
	})

	got := buf.String()
	for _, want := range []string{
		"RESIZE session=11111111-1111-1111-1111-111111111111 cols=120 rows=36",
		"ATTACH session=11111111-1111-1111-1111-111111111111 attach_session=11111111-1111-1111-1111-111111111111 since_seq=9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q missing %q", got, want)
		}
	}
}
