package session

import (
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSilence_DefaultsApplied(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if !s.silenceDetectEnabled {
		t.Fatalf("default ATTERM_TASK_SILENCE_DETECT should be true; got false")
	}
	if s.silenceThresholdMS != 5000 {
		t.Fatalf("default ATTERM_TASK_SILENCE_THRESHOLD_MS should be 5000; got %d", s.silenceThresholdMS)
	}
}

func TestSilence_EnvOverrides(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0")
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "250")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if s.silenceDetectEnabled {
		t.Fatalf("DETECT=0 should disable")
	}
	if s.silenceThresholdMS != 250 {
		t.Fatalf("THRESHOLD_MS=250 should set field to 250; got %d", s.silenceThresholdMS)
	}
}

func TestSilence_InvalidThresholdFallsBack(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "not-a-number")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if s.silenceThresholdMS != 5000 {
		t.Fatalf("invalid threshold should fall back to 5000; got %d", s.silenceThresholdMS)
	}
}

// directFireSilenceTimer constructs a session, sets the heuristic to fire
// after 50ms, drops the session into running + alt-screen, calls
// rescheduleSilenceTimerLocked(), waits long enough for the timer to fire,
// and returns the session. Tests then inspect Info() / waitingFromSilence.
func directFireSilenceTimer(t *testing.T, prep func(*Session)) *Session {
	t.Helper()
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	prep(s)
	s.mu.Lock()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

func TestSilence_TimerFlipsRunningToWaiting(t *testing.T) {
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = true
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
		s.lastOutputMono = time.Now().Add(-10 * time.Second)
	})
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("silence timer should have flipped state to waiting_input; got %q", s.Info().TaskState)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if !wfs {
		t.Fatalf("waitingFromSilence should be true after silence flip")
	}
	if s.Info().AttentionAt == 0 {
		t.Fatalf("AttentionAt should be bumped by silence flip")
	}
}

func TestSilence_TimerNoopWhenNotAltScreenAndNotAI(t *testing.T) {
	// Default Type is shell (set by New). Not in alt-screen + not AI →
	// silence heuristic does NOT fire (e.g. tail -f, sleep 30 — silent
	// shell commands legitimately doing work).
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = false
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
		s.lastOutputMono = time.Now().Add(-10 * time.Second)
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must not fire outside alt-screen for shell-type sessions")
	}
}

func TestSilence_TimerFiresForAITypeWithoutAltScreen(t *testing.T) {
	// Claude Code v2.x (and similar inline-rendering AI clients) never
	// flip alt-screen on, so the heuristic must apply on Type=ai even
	// when altScreen is false. This is the canonical "claude waiting for
	// my next message" case the inbox model targets.
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = false
		s.meta.Type = SessionTypeAI
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
		s.lastOutputMono = time.Now().Add(-10 * time.Second)
	})
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must fire for AI type without alt-screen; got %q",
			s.Info().TaskState)
	}
	if !s.waitingFromSilence {
		t.Fatalf("waitingFromSilence flag should be set")
	}
}

func TestSilence_TimerNoopForTestTypeWithoutAltScreen(t *testing.T) {
	// Test/build/deploy types routinely run silently while genuinely
	// working. The relaxation is intentionally AI-only.
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = false
		s.meta.Type = SessionTypeTest
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
		s.lastOutputMono = time.Now().Add(-10 * time.Second)
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must NOT fire for test type without alt-screen "+
			"(test/build/deploy routinely work silently); got %q",
			s.Info().TaskState)
	}
}

func TestSilence_TimerNoopWhenDetectDisabled(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0")
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = true
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
		s.lastOutputMono = time.Now().Add(-10 * time.Second)
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must not fire when detection is disabled")
	}
}

func TestSilence_TimerReschedulesWhenOutputTooRecent(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "30")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Unix() // not silent
	s.lastOutputMono = time.Now()           // not silent
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	time.Sleep(120 * time.Millisecond)
	// Push both timestamps back in time so the NEXT check will see "silent"
	s.mu.Lock()
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("after delay + back-dated LastOutputAt, silence timer should flip; got %q", s.Info().TaskState)
	}
}

func TestSilence_OutputRestoresRunningFromSilenceWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup precondition: expected waiting_input, got %q", s.Info().TaskState)
	}
	att := s.Info().AttentionAt
	if att == 0 {
		t.Fatalf("setup precondition: expected AttentionAt to be bumped")
	}
	// Now output arrives. State should flip back to running; AttentionAt MUST
	// remain at the bumped value (spec §6 is explicit on this).
	s.updateTerminalState([]byte("hello"))
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("expected output to restore running; got %q", s.Info().TaskState)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if wfs {
		t.Fatalf("waitingFromSilence should be cleared after restore")
	}
	if s.Info().AttentionAt != att {
		t.Fatalf("AttentionAt should not roll back on restore; was %d, now %d", att, s.Info().AttentionAt)
	}
}

func TestSilence_KeywordWaitingNotRestoredByOutput(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0") // isolate the keyword path
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.updateTerminalState([]byte("Continue? [y/N] "))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected keyword to flip to waiting_input")
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if wfs {
		t.Fatalf("keyword path must not set waitingFromSilence")
	}
	s.updateTerminalState([]byte("more text"))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("keyword waiting_input must persist across output; got %q", s.Info().TaskState)
	}
}

func TestSilence_OSC133DOverridesSilenceWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.meta.CommandStartedAt = time.Now().Unix() // satisfy 'D' early-return guard
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	s.updateTerminalState(osc("D;0"))
	if s.Info().TaskState != proto.TaskStateCompleted {
		t.Fatalf("expected D to flip to completed; got %q", s.Info().TaskState)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if wfs {
		t.Fatalf("waitingFromSilence should be cleared after D")
	}
}

func TestSilence_CloseStopsTimer(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	s.Close()
	time.Sleep(200 * time.Millisecond)
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("Close() should have stopped the silence timer")
	}
}

func TestSilence_RepeatSilenceIsMonotone(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "40")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	firstAtt := s.Info().AttentionAt
	if firstAtt == 0 {
		t.Fatalf("setup: expected silence flip to bump AttentionAt")
	}
	// Wait at least 1 second so that AttentionAt (unix seconds) can be strictly greater.
	time.Sleep(1100 * time.Millisecond)
	// Output arrives → restore to running.
	s.updateTerminalState([]byte("x"))
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("expected restore to running; got %q", s.Info().TaskState)
	}
	// Re-prime silence: back-date both timestamps and let the timer fire again.
	s.mu.Lock()
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline = time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	secondAtt := s.Info().AttentionAt
	if secondAtt <= firstAtt {
		t.Fatalf("second silence AttentionAt must be > first; %d vs %d", secondAtt, firstAtt)
	}
}

func TestSilence_SubSecondThresholdActuallyFires(t *testing.T) {
	// Force a 300ms threshold and verify the flip happens within ~700ms
	// (would be ~1100ms with the old seconds-precision math).
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "300")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	// Pretend we just received output 1ms ago at monotonic resolution.
	s.lastOutputMono = time.Now().Add(-1 * time.Millisecond)
	s.meta.LastOutputAt = time.Now().Unix() // align unix-seconds source
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	// Wait < 1s; flip MUST have happened.
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("300ms threshold should fire within 700ms; state=%q after deadline", s.Info().TaskState)
	}
}

func TestSilence_OSC133CClearsWaitingFromSilenceFlag(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if !wfs {
		t.Fatalf("setup: expected waitingFromSilence == true")
	}
	// Now a new OSC C arrives (e.g. previous tool exited, shell starts new command).
	s.updateTerminalState(osc("C;make"))
	s.mu.RLock()
	wfs = s.waitingFromSilence
	s.mu.RUnlock()
	if wfs {
		t.Fatalf("OSC C must clear waitingFromSilence")
	}
}

func TestSilence_UpdateMetaRescheduleAndClearFlag(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	// Drive into heuristic waiting_input.
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	s.mu.RUnlock()
	if !wfs {
		t.Fatalf("setup: expected waitingFromSilence == true")
	}
	// External META pushes the session back to running (mirror scenario).
	s.UpdateMeta(proto.MetaPayload{TaskState: proto.TaskStateRunning})
	s.mu.RLock()
	wfs = s.waitingFromSilence
	s.mu.RUnlock()
	if wfs {
		t.Fatalf("UpdateMeta to running must clear waitingFromSilence")
	}
}

func TestSilence_NoRaceUnderConcurrentOutput(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "20")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.mu.Unlock()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				s.updateTerminalState([]byte("x"))
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	s.Close()
}
