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

func TestSilence_TinyRunningRedrawDoesNotPostponeWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.meta.Type = SessionTypeAI
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.mu.Unlock()

	// Codex/Claude idle TUIs can emit tiny cursor/status redraw chunks while
	// the prompt is already waiting. Those bytes should update LastOutputAt,
	// but must not reset the silence clock that decides running -> waiting.
	s.updateTerminalState([]byte("\x1b[?25l"))
	s.onSilenceFired()

	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("tiny redraw while running should not postpone silence flip; got %q", s.Info().TaskState)
	}
}

func TestSilence_BurstSmallOutputPostponesWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "8")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.meta.Type = SessionTypeAI
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.mu.Unlock()

	s.updateTerminalState([]byte("abcd"))
	s.updateTerminalState([]byte("efgh"))
	s.onSilenceFired()

	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("burst small output should postpone silence flip; got %q", s.Info().TaskState)
	}
}

func TestSilence_OutputRestoresRunningFromSilenceWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	// Use a small restore threshold so we can exercise it with a short buffer
	// without simulating hundreds of bytes of claude output.
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "5")
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

// Cursor-blink / spinner redraws (a handful of bytes) MUST NOT restore
// running once the silence heuristic has flipped to waiting_input —
// otherwise TUI sessions (claude code v2.x etc.) ping-pong continuously
// and the sidebar visibly oscillates.
func TestSilence_TinyOutputDoesNotRestoreFromSilence(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	// Default ATTERM_TASK_SILENCE_RESTORE_BYTES (256).
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
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	// Cursor-blink-sized output arrives twice — well under 256B total.
	s.updateTerminalState([]byte("\x1b[?25l"))
	s.updateTerminalState([]byte("\x1b[?25h"))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("small redraws must NOT restore running from silence; got %q", s.Info().TaskState)
	}
	s.mu.RLock()
	wfs := s.waitingFromSilence
	bytes := s.silenceRestoreBytes
	s.mu.RUnlock()
	if !wfs {
		t.Fatalf("waitingFromSilence should still be set")
	}
	if bytes == 0 {
		t.Fatalf("silenceRestoreBytes should have accumulated; got 0")
	}
}

// SIGWINCH-driven repaint after a PTY resize must NOT restore running
// even though the repaint chunk can easily exceed the byte threshold.
// Regression: collapsing the desktop task sidebar (which RESIZEs the
// terminal) used to knock idle claude sessions back to running because
// the alt-screen repaint blew past the accumulator.
func TestSilence_ResizeRepaintDoesNotRestoreFromSilence(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	// Small byte threshold so a single chunk would otherwise restore.
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "32")
	// Default grace window (1500ms) is plenty for this test.
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
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	// User collapses the sidebar → frontend RESIZE → SIGWINCH → claude
	// repaints the whole alt-screen as one large chunk.
	s.UpdateSize(120, 30)
	repaint := make([]byte, 4096)
	for i := range repaint {
		repaint[i] = 'A'
	}
	s.updateTerminalState(repaint)
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("resize repaint must NOT restore running; got %q", s.Info().TaskState)
	}
	s.mu.RLock()
	bytes := s.silenceRestoreBytes
	s.mu.RUnlock()
	if bytes != 0 {
		t.Fatalf("resize grace should keep accumulator at 0; got %d", bytes)
	}
}

// After the resize grace window closes, accumulator behavior returns to
// normal — sustained output then restores running as usual.
func TestSilence_RestoreResumesAfterResizeGraceCloses(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "16")
	t.Setenv("ATTERM_TASK_SILENCE_RESIZE_GRACE_MS", "30") // short, for the test
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
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	// Resize fires; repaint within the grace window is ignored.
	s.UpdateSize(120, 30)
	s.updateTerminalState([]byte("\x1b[2J\x1b[H")) // small repaint chunk
	// Wait past the grace window.
	time.Sleep(80 * time.Millisecond)
	// Now sustained "real" output past the byte threshold.
	for i := 0; i < 4; i++ {
		s.updateTerminalState([]byte("0123456789"))
	}
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("output past resize-grace should restore running; got %q", s.Info().TaskState)
	}
}

// Larger sustained output — once the accumulator passes the threshold —
// DOES restore to running. Sanity check that the gate isn't permanent.
func TestSilence_LargeOutputRestoresFromSilence(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "32")
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
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	// Several small chunks summing past 32 bytes.
	for i := 0; i < 4; i++ {
		s.updateTerminalState([]byte("0123456789"))
	}
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("sustained output should restore running; got %q", s.Info().TaskState)
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
	t.Setenv("ATTERM_TASK_SILENCE_RESTORE_BYTES", "1")
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
