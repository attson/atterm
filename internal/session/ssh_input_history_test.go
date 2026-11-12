package session

import (
	"strings"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestEchoConfirmedInputRecordsSubmittedRemoteCommand(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	if !s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("docker compose pull atterm-relay")}) {
		t.Fatal("input was not accepted")
	}
	s.PushOut(1, []byte("docker compose pull atterm-relay"))
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("\r")})

	if len(got) != 1 || got[0] != "docker compose pull atterm-relay" {
		t.Fatalf("confirmed inputs = %#v", got)
	}
}

func TestEchoConfirmedInputRejectsHiddenPassword(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("hunter2")})
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("\r")})
	s.PushOut(1, []byte("\r\nPermission denied: hunter2 is not a command\r\n"))

	if len(got) != 0 {
		t.Fatalf("hidden input was recorded: %#v", got)
	}
}

func TestEchoConfirmedInputRejectsHiddenValueMatchingOutputPrefix(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("hunter2\r")})
	// No leading newline: the secret happens to prefix visible error output.
	// Recording at the instant the prefix matched would leak it.
	s.PushOut(1, []byte("hunter2 is not a valid token\r\n"))

	if len(got) != 0 {
		t.Fatalf("hidden input prefix was recorded: %#v", got)
	}
}

func TestEchoConfirmedInputAppliesLineEditingAndBracketedPaste(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	// Correct "dockre" to "docker" with two backspaces, then paste the rest.
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("dockre")})
	s.PushOut(1, []byte("dockre"))
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("\x7f\x7fer")})
	s.PushOut(2, []byte("\b \b\b \ber"))
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("\x1b[200~ compose ps\x1b[201~")})
	s.PushOut(3, []byte(" compose ps"))
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("\r")})

	if len(got) != 1 || got[0] != "docker compose ps" {
		t.Fatalf("edited input = %#v", got)
	}
}

func TestEchoConfirmedInputRejectsDraftAndAlternateScreen(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("draft only")})
	s.PushOut(1, []byte("draft only"))
	s.PushOut(2, []byte("\x1b[?1049h"))
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("full-screen-key\r")})
	s.PushOut(3, []byte("full-screen-key\r\n"))
	s.PushOut(4, []byte("\x1b[?1049l"))

	if len(got) != 0 {
		t.Fatalf("unsafe input was recorded: %#v", got)
	}
}

func TestEchoConfirmedInputRejectsAlternateScreenCycleInOneOutputChunk(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	// Input can race ahead of the output that announces a TUI's alternate
	// screen. The enter, echo, and exit may then arrive in one network read;
	// checking only the final screen mode would incorrectly treat it as shell
	// echo and persist the TUI value.
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte("tui-secret\r")})
	s.PushOut(1, []byte("\x1b[?1049htui-secret\r\n\x1b[?1049l"))

	if len(got) != 0 {
		t.Fatalf("alternate-screen input was recorded: %#v", got)
	}
}

func TestEchoConfirmedInputBoundsCandidateMemory(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	var got []string
	s.SetOnEchoConfirmedInput(func(line string) { got = append(got, line) })

	oversized := strings.Repeat("x", maxEchoInputGlyphs+1)
	s.SendInbound(proto.Frame{Type: proto.TypeIn, Payload: []byte(oversized + "\r")})
	s.PushOut(1, []byte(oversized+"\r\n"))

	if len(got) != 0 {
		t.Fatalf("oversized input was recorded: %d items", len(got))
	}
}
