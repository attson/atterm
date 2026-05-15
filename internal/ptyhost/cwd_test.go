//go:build linux || darwin

package ptyhost

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"
)

// TestHostCwdReflectsChildChdir spawns a shell, has it chdir into /tmp, and
// verifies that Host.Cwd reports the new directory. Symlinks are resolved on
// both sides because macOS exposes /tmp as /private/tmp.
func TestHostCwdReflectsChildChdir(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host, err := Open(ctx, Config{
		Argv: []string{"/bin/sh", "-c", "cd /tmp && exec sleep 30"},
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = host.Close()
		go io.Copy(io.Discard, host) // drain so Wait can return
		_ = host.Wait()
	})

	wantResolved, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatalf("EvalSymlinks(/tmp): %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		got = host.Cwd()
		if got != "" {
			if resolved, err := filepath.EvalSymlinks(got); err == nil && resolved == wantResolved {
				return // success
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Host.Cwd never reported /tmp; last value = %q (want %q)", got, wantResolved)
}
