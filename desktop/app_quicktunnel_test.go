package main

import (
	"context"
	"sync/atomic"
	"testing"
)

type fakeQuickTunnelLifecycle struct {
	stops atomic.Int32
}

func (f *fakeQuickTunnelLifecycle) Stop() error {
	f.stops.Add(1)
	return nil
}

func TestShutdownStopsQuickTunnel(t *testing.T) {
	lifecycle := &fakeQuickTunnelLifecycle{}
	a := &App{quickTunnel: lifecycle}

	a.shutdown(context.Background())

	if got := lifecycle.stops.Load(); got != 1 {
		t.Fatalf("quick tunnel Stop calls = %d, want 1", got)
	}
}
