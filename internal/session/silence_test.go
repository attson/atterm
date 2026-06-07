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
	})
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("silence timer should have flipped state to waiting_input; got %q", s.Info().TaskState)
	}
	if !s.waitingFromSilence {
		t.Fatalf("waitingFromSilence should be true after silence flip")
	}
	if s.Info().AttentionAt == 0 {
		t.Fatalf("AttentionAt should be bumped by silence flip")
	}
}

func TestSilence_TimerNoopWhenNotAltScreen(t *testing.T) {
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = false
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must not fire outside alt-screen")
	}
}

func TestSilence_TimerNoopWhenDetectDisabled(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0")
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = true
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
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
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	time.Sleep(120 * time.Millisecond)
	// Push LastOutputAt back in time so the NEXT check will see "silent"
	s.mu.Lock()
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
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
