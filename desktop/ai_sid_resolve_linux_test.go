//go:build linux

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestCodexRolloutOwnedByProcess_FollowsDescendantOpenFile(t *testing.T) {
	dir := t.TempDir()
	wantedSID := "019fae77-52c1-7201-bbdc-078634559f19"
	wantedPath := filepath.Join(dir, "rollout-wanted-"+wantedSID+".jsonl")
	otherPath := filepath.Join(dir, "rollout-other-019fae95-43eb-7491-9341-05c156228664.jsonl")
	writeJsonl(t, wantedPath, `{}`)
	writeJsonl(t, otherPath, `{}`)

	// The PTY starts a shell; Codex is a descendant and owns the rollout fd.
	// Keep the shell alive as the root so the lookup must walk the process tree.
	cmd := exec.Command("sh", "-c", `sh -c 'exec 3<"$1"; printf x; sleep 10' child "$1" & wait`, "root", wantedPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	ready := make([]byte, 1)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatalf("wait for child fd: %v", err)
	}

	files := map[string]codexRolloutFileInfo{
		wantedSID: {Path: wantedPath, ModTime: time.Now()},
		"other":   {Path: otherPath, ModTime: time.Now()},
	}
	got, ok := codexRolloutOwnedByProcess(cmd.Process.Pid, files)
	if !ok || got != wantedSID {
		t.Fatalf("owned rollout = (%q, %v), want (%q, true)", got, ok, wantedSID)
	}
}

func TestStartCodexFileResolve_SameCwdUsesProcessOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "repo")
	dir := codexWatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	firstSID := "019fae77-52c1-7201-bbdc-078634559f19"
	secondSID := "019fae95-43eb-7491-9341-05c156228664"
	firstPath := filepath.Join(dir, "rollout-first-"+firstSID+".jsonl")
	secondPath := filepath.Join(dir, "rollout-second-"+secondSID+".jsonl")
	writeJsonl(t, firstPath,
		`{"type":"session_meta","payload":{"id":"`+firstSID+`","cwd":"`+cwd+`","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"first pane title"}}`,
	)
	writeJsonl(t, secondPath,
		`{"type":"session_meta","payload":{"id":"`+secondSID+`","cwd":"`+cwd+`","thread_source":"user"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"second pane title"}}`,
	)

	firstProc := startProcessHoldingFile(t, firstPath)
	secondProc := startProcessHoldingFile(t, secondPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := session.New(uuid.New(), proto.SessionInfo{Cwd: cwd})
	defer first.Close()
	second := session.New(uuid.New(), proto.SessionInfo{Cwd: cwd})
	defer second.Close()
	firstCaptured := make(chan string, 1)
	secondCaptured := make(chan string, 1)
	go startCodexFileResolve(ctx, first, cwd, firstProc.Process.Pid, func(sid string) { firstCaptured <- sid })
	go startCodexFileResolve(ctx, second, cwd, secondProc.Process.Pid, func(sid string) { secondCaptured <- sid })

	if got := receiveString(t, firstCaptured); got != firstSID {
		t.Fatalf("first pane captured %q, want %q", got, firstSID)
	}
	if got := receiveString(t, secondCaptured); got != secondSID {
		t.Fatalf("second pane captured %q, want %q", got, secondSID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if first.Info().Title == "first pane title" && second.Info().Title == "second pane title" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("titles = (%q, %q), want distinct process-owned titles", first.Info().Title, second.Info().Title)
}

func startProcessHoldingFile(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sh", "-c", `exec 3<"$1"; printf x; exec sleep 10`, "holder", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	ready := make([]byte, 1)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatalf("wait for file holder: %v", err)
	}
	return cmd
}

func receiveString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for capture")
		return ""
	}
}
