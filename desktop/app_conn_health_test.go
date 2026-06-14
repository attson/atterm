package main

import (
	"testing"
)

func TestGetUplinkHealth_NoUplinkReturnsClosed(t *testing.T) {
	a := &App{}
	s := a.GetUplinkHealth()
	if s.State != "closed" {
		t.Fatalf("state = %q, want closed", s.State)
	}
	if s.RTT.LastMS != nil {
		t.Fatalf("RTT non-nil on closed app")
	}
	if s.RTTSamples == nil {
		t.Fatalf("RTTSamples is nil; should be empty slice for JSON serialization")
	}
	if s.Reconnect.History == nil {
		t.Fatalf("Reconnect.History is nil; should be empty slice")
	}
}
