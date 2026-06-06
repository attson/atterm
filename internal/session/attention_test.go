package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func osc(s string) []byte { return []byte("\x1b]133;" + s + "\x07") }

func TestAttentionAt_WaitingInputAlwaysBumps(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.updateTerminalState([]byte("Continue? [y/N] "))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("expected waiting_input, got %q", s.Info().TaskState)
	}
	if s.Info().AttentionAt == 0 {
		t.Fatalf("waiting_input must bump attention_at")
	}
}

func TestAttentionAt_NonShellCompletionBumps(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.updateTerminalState(osc("C;claude"))
	if s.Info().Type != SessionTypeAI {
		t.Fatalf("expected ai, got %q", s.Info().Type)
	}
	s.updateTerminalState(osc("D;0"))
	if s.Info().AttentionAt == 0 {
		t.Fatalf("non-shell completion must bump attention_at")
	}
}

func TestAttentionAt_ShellCompletionDoesNotBump(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.updateTerminalState(osc("C;ls"))
	s.updateTerminalState(osc("D;0"))
	if s.Info().AttentionAt != 0 {
		t.Fatalf("shell completion must NOT bump attention_at, got %d", s.Info().AttentionAt)
	}
}
