package quicktunnel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerStartsFromStderrAndStopsIdempotently(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "args")
	m := newHelperManager(t, "url-stderr", argsFile, 2*time.Second, time.Second)
	status, err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !status.Running || status.Starting {
		t.Fatalf("status after Start = %+v", status)
	}
	if status.PublicURL != "https://quiet-field.trycloudflare.com" {
		t.Fatalf("public URL = %q", status.PublicURL)
	}
	if !strings.HasPrefix(status.LocalOrigin, "http://127.0.0.1:") {
		t.Fatalf("local origin = %q", status.LocalOrigin)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read helper args: %v", err)
	}
	want := "tunnel\n--no-autoupdate\n--url\n" + status.LocalOrigin + "\n"
	if string(args) != want {
		t.Fatalf("cloudflared args = %q, want %q", args, want)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if got := m.Status(); got.Running || got.Starting || got.PublicURL != "" || got.LocalOrigin != "" {
		t.Fatalf("status after Stop = %+v", got)
	}
}

func TestManagerDoesNotDownloadMissingExecutable(t *testing.T) {
	t.Parallel()

	m := New(Config{Executable: filepath.Join(t.TempDir(), "missing-cloudflared")})
	_, err := m.Start(context.Background())
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("Start error = %v, want ErrExecutableNotFound", err)
	}
	if got := m.Status(); got.Running || got.Starting {
		t.Fatalf("status after missing executable = %+v", got)
	}
}

func TestManagerRejectsInvalidURLBeforeProcessExit(t *testing.T) {
	t.Parallel()

	m := newHelperManager(t, "invalid-exit", "", time.Second, time.Second)
	_, err := m.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "before publishing a valid Quick Tunnel URL") {
		t.Fatalf("Start error = %v", err)
	}
	if got := m.Status(); got.Running || got.Starting {
		t.Fatalf("status after invalid URL = %+v", got)
	}
}

func TestManagerStartTimeoutCleansProcessAndGateway(t *testing.T) {
	t.Parallel()

	m := newHelperManager(t, "hang", "", 80*time.Millisecond, 200*time.Millisecond)
	started := time.Now()
	_, err := m.Start(context.Background())
	if !errors.Is(err, ErrStartTimeout) {
		t.Fatalf("Start error = %v, want ErrStartTimeout", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("Start timeout cleanup took %s", time.Since(started))
	}
	if got := m.Status(); got.Running || got.Starting || got.LocalOrigin != "" {
		t.Fatalf("status after timeout = %+v", got)
	}
}

func TestManagerForceKillsProcessThatIgnoresTerminate(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a helper process")
	}

	m := newHelperManager(t, "ignore-term", "", time.Second, 80*time.Millisecond)
	if _, err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the helper time to install its SIGTERM handler after publishing the
	// URL; otherwise a scheduler race can turn this into the graceful case.
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 70*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("forced Stop took %s", elapsed)
	}
}

func TestManagerConcurrentStartIsRejected(t *testing.T) {
	t.Parallel()

	m := newHelperManager(t, "hang", "", 2*time.Second, 100*time.Millisecond)
	var done atomic.Bool
	go func() {
		_, _ = m.Start(context.Background())
		done.Store(true)
	}()

	deadline := time.Now().Add(time.Second)
	for !m.Status().Starting && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := m.Start(context.Background()); !errors.Is(err, ErrStartInProgress) {
		t.Fatalf("second Start error = %v, want ErrStartInProgress", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for !done.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("first Start did not return after Stop")
	}
}

func TestManagerStopWaitsForInFlightStartCleanup(t *testing.T) {
	t.Parallel()

	m := newHelperManager(t, "hang", "", time.Second, 500*time.Millisecond)
	enteredCommandFactory := make(chan struct{})
	releaseCommandFactory := make(chan struct{})
	baseCommand := m.command
	m.command = func(executable string, args ...string) *exec.Cmd {
		close(enteredCommandFactory)
		<-releaseCommandFactory
		return baseCommand(executable, args...)
	}

	startDone := make(chan error, 1)
	go func() {
		_, err := m.Start(context.Background())
		startDone <- err
	}()
	<-enteredCommandFactory

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.Stop() }()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before in-flight Start could clean up: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCommandFactory)

	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-startDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want context.Canceled", err)
	}
	if got := m.Status(); got.Running || got.Starting || got.LocalOrigin != "" {
		t.Fatalf("status after stopped in-flight Start = %+v", got)
	}
}

func newHelperManager(t *testing.T, mode, argsFile string, startTimeout, stopTimeout time.Duration) *Manager {
	t.Helper()
	m := New(Config{
		Executable:   os.Args[0],
		StartTimeout: startTimeout,
		StopTimeout:  stopTimeout,
	})
	m.command = func(executable string, args ...string) *exec.Cmd {
		helperArgs := []string{"-test.run=TestQuickTunnelHelperProcess", "--"}
		helperArgs = append(helperArgs, args...)
		cmd := exec.Command(executable, helperArgs...)
		cmd.Env = append(os.Environ(),
			"ATTERM_QUICKTUNNEL_HELPER=1",
			"ATTERM_QUICKTUNNEL_HELPER_MODE="+mode,
			"ATTERM_QUICKTUNNEL_ARGS_FILE="+argsFile,
		)
		return cmd
	}
	return m
}

func TestQuickTunnelHelperProcess(t *testing.T) {
	if os.Getenv("ATTERM_QUICKTUNNEL_HELPER") != "1" {
		return
	}

	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	args := os.Args[separator:]
	if path := os.Getenv("ATTERM_QUICKTUNNEL_ARGS_FILE"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(args, "\n")+"\n"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
	}

	switch os.Getenv("ATTERM_QUICKTUNNEL_HELPER_MODE") {
	case "url-stderr":
		fmt.Fprintln(os.Stderr, "INF Your quick Tunnel is available at https://quiet-field.trycloudflare.com")
		waitForTermination(false)
	case "invalid-exit":
		fmt.Fprintln(os.Stdout, "https://nested.bad.trycloudflare.com")
		os.Exit(2)
	case "hang":
		waitForTermination(false)
	case "ignore-term":
		fmt.Fprintln(os.Stdout, "https://quiet-field.trycloudflare.com")
		waitForTermination(true)
	case "tree-parent":
		child := exec.Command(os.Args[0], "-test.run=TestQuickTunnelHelperProcess", "--")
		child.Env = append(os.Environ(),
			"ATTERM_QUICKTUNNEL_HELPER=1",
			"ATTERM_QUICKTUNNEL_HELPER_MODE=tree-child",
			"ATTERM_QUICKTUNNEL_MARKER="+os.Getenv("ATTERM_QUICKTUNNEL_MARKER"),
		)
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(5)
		}
		ready := os.Getenv("ATTERM_QUICKTUNNEL_MARKER") + ".ready"
		deadline := time.Now().Add(time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "descendant did not become ready")
				os.Exit(6)
			}
			time.Sleep(time.Millisecond)
		}
		fmt.Fprintln(os.Stdout, "https://quiet-field.trycloudflare.com")
		waitForTermination(false)
	case "tree-child":
		waitForTerminationAndMark(os.Getenv("ATTERM_QUICKTUNNEL_MARKER"))
	default:
		os.Exit(4)
	}
	os.Exit(0)
}
