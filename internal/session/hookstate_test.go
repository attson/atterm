package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func aiSession(t *testing.T) *Session {
	t.Helper()
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.Type = SessionTypeAI
	s.meta.TaskState = proto.TaskStateRunning
	s.mu.Unlock()
	return s
}

func TestApplyHookState_SetsStateAndLatches(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateWaitingInput)
	if got := s.Info().TaskState; got != proto.TaskStateWaitingInput {
		t.Fatalf("state = %q, want waiting_input", got)
	}
	if !s.HookDriven() {
		t.Fatal("first hook event must latch the session")
	}
}

func TestApplyHookState_WaitingBumpsAttention(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateWaitingInput)
	if s.Info().AttentionAt == 0 {
		t.Fatal("waiting_input must raise attention, like the silence path")
	}
}

func TestApplyHookState_RunningDoesNotBumpAttention(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateRunning)
	if s.Info().AttentionAt != 0 {
		t.Fatal("running is not a call for the user")
	}
}

// A hook event that arrives after the CLI has exited must not drag an ordinary
// shell into an AI state.
func TestApplyHookState_IgnoredForNonAISessions(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.Type = SessionTypeShell
	s.meta.TaskState = proto.TaskStateIdle
	s.mu.Unlock()

	s.ApplyHookState(proto.TaskStateRunning)
	if s.Info().TaskState != proto.TaskStateIdle {
		t.Fatal("hook events must not touch a shell session")
	}
	if s.HookDriven() {
		t.Fatal("a rejected event must not latch")
	}
}

func TestApplyHookState_IgnoresUnknownState(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState("banana")
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatal("unknown states must be dropped, not stored")
	}
}

// The whole point of the latch: once the client reports state, the timer that
// used to guess it must never arm again for this session.
func TestHookDriven_SilenceTimerNeverArms(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "40")
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateRunning)

	s.mu.Lock()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	armed := s.silenceTimer != nil
	s.mu.Unlock()
	if armed {
		t.Fatal("silence timer must not arm for a hook-driven session")
	}

	time.Sleep(150 * time.Millisecond)
	if got := s.Info().TaskState; got != proto.TaskStateRunning {
		t.Fatalf("state drifted to %q — the heuristic is still running", got)
	}
}

// When the AI CLI exits, the shell underneath takes over and there are no more
// hooks. The session has to go back to the heuristic or it would freeze at
// whatever the last hook said.
func TestHookDriven_ClearedWhenCommandExits(t *testing.T) {
	s := aiSession(t)
	s.mu.Lock()
	s.meta.CommandStartedAt = time.Now().Unix()
	s.mu.Unlock()
	s.ApplyHookState(proto.TaskStateRunning)

	s.updateTerminalState([]byte("\x1b]133;D;0\x07"))

	if s.HookDriven() {
		t.Fatal("command exit must return the session to the heuristic")
	}
	if got := s.Info().TaskState; got != proto.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got)
	}
}
