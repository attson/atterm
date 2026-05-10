package relay

import (
	"bytes"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestDebugFrameDisabledWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer(Config{DebugLog: &buf})

	s.debugFrame("client", "recv", proto.Frame{Type: proto.TypeList})

	if got := buf.String(); got != "" {
		t.Fatalf("debug disabled wrote %q; want empty", got)
	}
}

func TestDebugFrameSummarizesInOutWithoutPayload(t *testing.T) {
	var buf bytes.Buffer
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true, DebugLog: &buf})

	s.debugFrame("client", "recv", proto.Frame{
		Type:      proto.TypeIn,
		SessionID: id,
		Payload:   []byte("secret command\n"),
	})
	s.debugFrame("agent", "recv", proto.EncodeOut(id, 42, []byte("secret output\n")))

	got := buf.String()
	for _, want := range []string{
		"relay-debug client recv IN session=11111111-1111-1111-1111-111111111111 bytes=15",
		"relay-debug agent recv OUT session=11111111-1111-1111-1111-111111111111 seq=42 bytes=14",
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
	var buf bytes.Buffer
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true, DebugPayload: true, DebugLog: &buf})

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
	var buf bytes.Buffer
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s := NewServer(Config{Debug: true, DebugLog: &buf})

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
