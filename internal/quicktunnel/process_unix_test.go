//go:build !windows

package quicktunnel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerStopSignalsDescendantProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "descendant-stopped")
	m := New(Config{
		Executable:   os.Args[0],
		StartTimeout: time.Second,
		StopTimeout:  time.Second,
	})
	m.command = func(executable string, args ...string) *exec.Cmd {
		cmd := exec.Command(executable, "-test.run=TestQuickTunnelHelperProcess", "--")
		cmd.Env = append(os.Environ(),
			"ATTERM_QUICKTUNNEL_HELPER=1",
			"ATTERM_QUICKTUNNEL_HELPER_MODE=tree-parent",
			"ATTERM_QUICKTUNNEL_MARKER="+marker,
		)
		return cmd
	}

	if _, err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant did not receive process-group termination")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
