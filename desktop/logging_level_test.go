package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/logging"
)

// lockedBuffer collects sink output. The logging package writes from whatever
// goroutine logged, so the collector has to be safe even when a test only
// happens to log from one.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

// captureLogSink points internal/logging at a buffer for the duration of a
// test, at DEBUG so nothing is filtered out by accident, and restores the
// previous sink and level afterwards.
func captureLogSink(t *testing.T) *lockedBuffer {
	t.Helper()
	prevLevel := logging.CurrentLevel()
	t.Cleanup(func() {
		logging.SetSink(nil)
		logging.SetLevel(prevLevel)
	})
	buf := &lockedBuffer{}
	logging.SetSink(buf)
	logging.SetLevel(logging.LevelDebug)
	return buf
}

// setTestLogLevelWarn raises the threshold after captureLogSink has lowered
// it, for tests that need to prove filtering actually happens.
func setTestLogLevelWarn(t *testing.T) {
	t.Helper()
	logging.SetLevel(logging.LevelWarn)
}

func TestLogLevelOrDefault(t *testing.T) {
	cases := []struct {
		in   string
		want logging.Level
	}{
		{"", logging.LevelInfo},
		{"DEBUG", logging.LevelDebug},
		{"warn", logging.LevelWarn},
		{"ERROR", logging.LevelError},
		{"nonsense", logging.LevelInfo},
	}
	for _, tc := range cases {
		if got := (appConfig{LogLevel: tc.in}).LogLevelOrDefault(); got != tc.want {
			t.Errorf("LogLevelOrDefault(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The two legs into the sink must not step on each other: internal/logging
// hands over a finished line, the stdlib logger hands over a bare message.
func TestRawWriterDoesNotDoubleWrap(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}

	line := logging.FormatLine(time.Date(2026, 8, 8, 15, 4, 5, 123000000, time.UTC), logging.LevelWarn, "uplink", "out chan full")
	if _, err := m.rawWriter().Write([]byte(line)); err != nil {
		t.Fatalf("rawWriter.Write() error = %v", err)
	}

	got := buf.String()
	if got != line {
		t.Fatalf("rawWriter rewrote the line:\n got %q\nwant %q", got, line)
	}
	if strings.Count(got, "[") != 1 {
		t.Fatalf("line was wrapped twice: %q", got)
	}
}

func TestLegacyWriteStillWraps(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}

	if _, err := m.Write([]byte("wails: something happened\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !strings.Contains(buf.String(), "INFO  [app] wails: something happened\n") {
		t.Fatalf("legacy write not normalized: %q", buf.String())
	}
}

// Applying a config state must move the process threshold, which is what makes
// the Settings dropdown take effect without a restart.
func TestApplySetsProcessLevel(t *testing.T) {
	prevLevel := logging.CurrentLevel()
	t.Cleanup(func() { logging.SetLevel(prevLevel) })

	m := &loggingManager{currentWriter: nil, maxBytes: 1024, maxBackups: 1}
	if err := m.Apply(loggingConfigState{enabled: false, level: logging.LevelWarn}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := logging.CurrentLevel(); got != logging.LevelWarn {
		t.Fatalf("level after Apply = %v, want LevelWarn", got)
	}
}

func TestLeveledHelpersReachSink(t *testing.T) {
	buf := captureLogSink(t)

	logDebug("pty", "d")
	logInfo("app", "i")
	logWarn("uplink", "w")
	logError("updater", "e")

	out := buf.String()
	for _, want := range []string{
		"DEBUG [pty] d",
		"INFO  [app] i",
		"WARN  [uplink] w",
		"ERROR [updater] e",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// End-to-end through the real manager: a leveled helper must land in the
// rotating file in the shape the viewer parses, and a below-threshold record
// must not.
func TestLeveledHelperReachesTheLogFile(t *testing.T) {
	dir := t.TempDir()
	prevLevel := logging.CurrentLevel()
	t.Cleanup(func() {
		logging.SetSink(nil)
		logging.SetLevel(prevLevel)
	})

	m, err := newLoggingManager(loggingOptions{
		devMode:       false,
		defaultPathFn: func() string { return filepath.Join(dir, "desktop.log") },
	})
	if err != nil {
		t.Fatalf("newLoggingManager() error = %v", err)
	}
	defer m.Close()

	if err := m.Apply(loggingConfigState{enabled: true, level: logging.LevelInfo}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	logDebug("uplink", "per-frame noise")
	logWarn("uplink", "out chan full session=%s", "abc")

	data, err := os.ReadFile(filepath.Join(dir, "desktop.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := string(data)
	if strings.Contains(got, "per-frame noise") {
		t.Errorf("DEBUG record written at INFO threshold:\n%s", got)
	}
	if !strings.Contains(got, "WARN  [uplink] out chan full session=abc\n") {
		t.Errorf("WARN record missing or misformatted:\n%s", got)
	}
}

func TestLeveledHelpersRespectThreshold(t *testing.T) {
	buf := captureLogSink(t)
	logging.SetLevel(logging.LevelWarn)

	logDebug("pty", "dropped")
	logInfo("app", "dropped")
	if buf.Len() != 0 {
		t.Fatalf("records below threshold reached the sink: %q", buf.String())
	}

	logWarn("uplink", "kept")
	if !strings.Contains(buf.String(), "kept") {
		t.Fatalf("at-threshold record missing: %q", buf.String())
	}
}
