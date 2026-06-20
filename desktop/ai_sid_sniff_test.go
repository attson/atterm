package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAISniffer_Claude_CapturesNewFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	// Pre-existing file (must NOT be reported).
	_ = os.WriteFile(filepath.Join(dir, "deadbeef-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl"), nil, 0o600)

	var got atomic.Value // string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 100*time.Millisecond, 3*time.Second, func(sid string) {
		got.Store(sid)
	})
	time.Sleep(150 * time.Millisecond) // let loop snapshot `before`
	_ = os.WriteFile(filepath.Join(dir, "1234abcd-1234-1234-1234-12345678abcd.jsonl"), nil, 0o600)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := got.Load(); v != nil && v.(string) != "" {
			if v.(string) != "1234abcd-1234-1234-1234-12345678abcd" {
				t.Fatalf("got sid %q", v)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sniff did not fire")
}

// A resumed/continued session (claude -c / --resume, or atterm's own restore)
// appends to an existing jsonl rather than creating a new one. The sniff must
// still capture its sid by noticing the modtime advance — this is the case
// that previously logged "ai sniff timeout" and broke AI session recovery.
func TestAISniffer_Claude_CapturesResumedFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	// Pre-existing session file present BEFORE the sniff snapshots `before`.
	target := filepath.Join(dir, "deadbeef-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl")
	_ = os.WriteFile(target, []byte("{}\n"), 0o600)

	var got atomic.Value // string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 100*time.Millisecond, 3*time.Second, func(sid string) {
		got.Store(sid)
	})
	time.Sleep(170 * time.Millisecond) // let loop snapshot `before`, then advance mtime
	// Resume appends — same filename, no new file, just a later modtime.
	_ = os.WriteFile(target, []byte("{}\n{}\n"), 0o600)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := got.Load(); v != nil && v.(string) != "" {
			if v.(string) != "deadbeef-aaaa-aaaa-aaaa-aaaaaaaaaaaa" {
				t.Fatalf("got sid %q", v)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sniff did not capture the resumed (appended) session file")
}

func TestAISniffer_TimeoutNoEmit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 50*time.Millisecond, 300*time.Millisecond, func(sid string) {
			fired = true
		})
		close(done)
	}()
	<-done
	if fired {
		t.Fatal("expected no emit on timeout")
	}
}

func TestAISniffer_AmbiguousNoEmit(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 80*time.Millisecond, 1500*time.Millisecond, func(sid string) {
			fired = true
		})
		close(done)
	}()
	time.Sleep(150 * time.Millisecond)
	// Two new files within one tick — must abort.
	_ = os.WriteFile(filepath.Join(dir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jsonl"), nil, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb.jsonl"), nil, 0o600)
	<-done
	if fired {
		t.Fatal("expected no emit on ambiguous diff")
	}
}

func TestAISniffer_Cancel(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	fired := false
	done := make(chan struct{})
	go func() {
		sniffAISessionIDForTest(ctx, dir, aiSniffers["claude"], 50*time.Millisecond, 5*time.Second, func(sid string) {
			fired = true
		})
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sniff did not exit on cancel")
	}
	if fired {
		t.Fatal("unexpected emit")
	}
}
