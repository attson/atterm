package session

import (
	"time"

	"github.com/attson/atterm/internal/proto"
)

// ApplyHookState records a task state reported by the AI client's own hooks.
//
// This is the authoritative path for AI sessions. OSC 133 cannot help here —
// claude and codex render inline and run as a single long shell command, so no
// command boundary is ever reported between turns — and inferring the state
// from output silence cannot tell "answered you, now waiting" apart from
// "thinking, or running a quiet tool". See
// docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md.
//
// Only running and waiting_input are accepted: those are the two states a turn
// moves between. Anything else is a caller bug and is dropped rather than
// stored, so a malformed payload cannot invent a state.
func (s *Session) ApplyHookState(next string) {
	if next != proto.TaskStateRunning && next != proto.TaskStateWaitingInput {
		return
	}
	s.mu.Lock()
	// Only AI sessions. A hook event can arrive just after the CLI exited, and
	// moving the shell that took its place would be worse than losing the event.
	if s.meta.Type != SessionTypeAI {
		s.mu.Unlock()
		return
	}
	prev := s.meta.TaskState
	s.hookDriven = true
	// The heuristic's bookkeeping is dead weight from here on; clear it so a
	// later unlatch starts from a clean slate rather than a stale accumulator.
	s.waitingFromSilence = false
	s.resetSilenceRestoreLocked()
	s.resetSilenceActivityBurstLocked()
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
	changed := prev != next
	s.meta.TaskState = next
	if next == proto.TaskStateWaitingInput {
		// Same contract as the silence path: waiting means the session wants
		// the user, which is what drives unread, the widget and the cards.
		s.meta.AttentionAt = time.Now().Unix()
	}
	if changed {
		s.fireTaskStateLocked(prev, next, TaskMeta{})
	}
	metaHook := s.onMetaChanged
	s.mu.Unlock()

	if changed {
		s.broadcastCurrentMeta()
		if metaHook != nil {
			metaHook()
		}
	}
}

// HookDriven reports whether this session's state comes from its AI client's
// hooks rather than from the silence heuristic.
func (s *Session) HookDriven() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hookDriven
}

// clearHookDrivenLocked returns the session to heuristic control. Caller holds
// s.mu.
func (s *Session) clearHookDrivenLocked() {
	s.hookDriven = false
}
