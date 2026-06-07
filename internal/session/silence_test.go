package session

import (
	"testing"

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
