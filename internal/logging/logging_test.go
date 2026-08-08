package logging

import (
	"bytes"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSink installs a buffer as the sink and restores the previous state
// when the test ends.
func captureSink(t *testing.T) *bytes.Buffer {
	t.Helper()
	prevSink := sink.Load()
	prevLevel := CurrentLevel()
	t.Cleanup(func() {
		sink.Store(prevSink)
		SetLevel(prevLevel)
	})
	buf := &bytes.Buffer{}
	SetSink(buf)
	SetLevel(LevelDebug)
	return buf
}

func TestFormatLine(t *testing.T) {
	ts := time.Date(2026, 8, 8, 15, 4, 5, 123000000, time.UTC)
	cases := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "2026/08/08 15:04:05.123 DEBUG [pty-input] hello\n"},
		{LevelInfo, "2026/08/08 15:04:05.123 INFO  [pty-input] hello\n"},
		{LevelWarn, "2026/08/08 15:04:05.123 WARN  [pty-input] hello\n"},
		{LevelError, "2026/08/08 15:04:05.123 ERROR [pty-input] hello\n"},
	}
	for _, tc := range cases {
		if got := FormatLine(ts, tc.level, "pty-input", "hello"); got != tc.want {
			t.Errorf("FormatLine(%v) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

// The frontend regex in parseLogLine.ts must keep matching what we emit.
func TestFormatLineMatchesFrontendShape(t *testing.T) {
	line := FormatLine(time.Now(), LevelWarn, "uplink", "out chan full")
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("line must end with newline: %q", line)
	}
	trimmed := strings.TrimSuffix(line, "\n")
	// TS(23) + space + LEVEL(5) + space + [tag] + space + msg
	if len(trimmed) < 30 {
		t.Fatalf("line too short: %q", trimmed)
	}
	if trimmed[23] != ' ' || trimmed[29] != ' ' {
		t.Fatalf("column layout drifted: %q", trimmed)
	}
	if !strings.Contains(trimmed, "[uplink] out chan full") {
		t.Fatalf("tag/message not rendered: %q", trimmed)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in    string
		want  Level
		valid bool
	}{
		{"DEBUG", LevelDebug, true},
		{"debug", LevelDebug, true},
		{"  Info ", LevelInfo, true},
		{"WARN", LevelWarn, true},
		{"warning", LevelWarn, true},
		{"ERROR", LevelError, true},
		{"", LevelInfo, false},
		{"TRACE", LevelInfo, false},
		{"nonsense", LevelInfo, false},
	}
	for _, tc := range cases {
		got, ok := ParseLevel(tc.in)
		if got != tc.want || ok != tc.valid {
			t.Errorf("ParseLevel(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.valid)
		}
	}
}

func TestParseLevelOrFallback(t *testing.T) {
	if got := ParseLevelOr("garbage", LevelError); got != LevelError {
		t.Errorf("ParseLevelOr(garbage) = %v, want LevelError", got)
	}
	if got := ParseLevelOr("debug", LevelError); got != LevelDebug {
		t.Errorf("ParseLevelOr(debug) = %v, want LevelDebug", got)
	}
}

func TestLevelStringRoundTrip(t *testing.T) {
	for _, l := range []Level{LevelDebug, LevelInfo, LevelWarn, LevelError} {
		got, ok := ParseLevel(l.String())
		if !ok || got != l {
			t.Errorf("round trip %v -> %q -> (%v, %v)", l, l.String(), got, ok)
		}
	}
}

func TestThresholdDropsBelowLevel(t *testing.T) {
	buf := captureSink(t)
	SetLevel(LevelWarn)

	Debug("uplink", "debug line")
	Info("uplink", "info line")
	if buf.Len() != 0 {
		t.Fatalf("below-threshold records reached the sink: %q", buf.String())
	}

	Warn("uplink", "warn line")
	Error("uplink", "error line")
	out := buf.String()
	if !strings.Contains(out, "warn line") || !strings.Contains(out, "error line") {
		t.Fatalf("at/above-threshold records missing: %q", out)
	}
}

// A below-threshold call must not even evaluate its format arguments through
// Sprintf — that is what makes per-frame Debug calls free.
func TestThresholdSkipsFormatting(t *testing.T) {
	captureSink(t)
	SetLevel(LevelError)

	formatted := false
	Debug("uplink", "%s", stringerFunc(func() string {
		formatted = true
		return "expensive"
	}))
	if formatted {
		t.Fatal("Debug formatted its arguments below threshold")
	}
}

type stringerFunc func() string

func (f stringerFunc) String() string { return f() }

func TestEnabled(t *testing.T) {
	captureSink(t)
	SetLevel(LevelWarn)
	if Enabled(LevelDebug) || Enabled(LevelInfo) {
		t.Error("Enabled reported true below threshold")
	}
	if !Enabled(LevelWarn) || !Enabled(LevelError) {
		t.Error("Enabled reported false at/above threshold")
	}
}

func TestEmitForcedBypassesThreshold(t *testing.T) {
	buf := captureSink(t)
	SetLevel(LevelError)

	Debug("pty-input", "dropped")
	if buf.Len() != 0 {
		t.Fatalf("Debug leaked past threshold: %q", buf.String())
	}

	EmitForced(LevelDebug, "pty-input", "write n=1 hex=1b")
	out := buf.String()
	if !strings.Contains(out, "DEBUG [pty-input] write n=1 hex=1b") {
		t.Fatalf("EmitForced did not write: %q", out)
	}
}

func TestEmitAtUsesSuppliedTimestamp(t *testing.T) {
	buf := captureSink(t)
	ts := time.Date(2020, 1, 2, 3, 4, 5, 678000000, time.Local)

	EmitAt(ts, LevelInfo, "ui-boot", "started")
	out := buf.String()
	if !strings.HasPrefix(out, "2020/01/02 03:04:05.678 INFO  [ui-boot] started") {
		t.Fatalf("EmitAt used the wrong timestamp: %q", out)
	}
}

func TestEmitAtRespectsThreshold(t *testing.T) {
	buf := captureSink(t)
	SetLevel(LevelWarn)
	EmitAt(time.Now(), LevelDebug, "ui-term", "noisy")
	if buf.Len() != 0 {
		t.Fatalf("EmitAt ignored the threshold: %q", buf.String())
	}
}

func TestNilSinkDiscards(t *testing.T) {
	prevSink := sink.Load()
	prevLevel := CurrentLevel()
	t.Cleanup(func() {
		sink.Store(prevSink)
		SetLevel(prevLevel)
	})
	SetSink(nil)
	SetLevel(LevelDebug)
	// Must not panic.
	Info("app", "into the void")
}

// A sink that fails must not propagate the failure — logging is never allowed
// to become a source of errors for its caller.
type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	return 0, io.ErrClosedPipe
}

func TestSinkErrorIsSwallowed(t *testing.T) {
	prevSink := sink.Load()
	prevLevel := CurrentLevel()
	t.Cleanup(func() {
		sink.Store(prevSink)
		SetLevel(prevLevel)
	})
	fw := &failingWriter{}
	SetSink(fw)
	SetLevel(LevelDebug)
	Error("app", "boom")
	if fw.calls != 1 {
		t.Fatalf("sink not called: %d", fw.calls)
	}
}

func TestFormatArgsOptional(t *testing.T) {
	buf := captureSink(t)
	// With no args the format string is written verbatim — no Sprintf pass, so
	// a message carrying a stray verb cannot be mangled.
	Info("app", "restored 3 of 3 panes")
	if !strings.Contains(buf.String(), "restored 3 of 3 panes") {
		t.Fatalf("message mangled: %q", buf.String())
	}

	buf.Reset()
	Info("app", "%d%% done", 100)
	if !strings.Contains(buf.String(), "100% done") {
		t.Fatalf("format verbs not applied: %q", buf.String())
	}
}

func TestStdlibWriterWrapsRawOutput(t *testing.T) {
	buf := captureSink(t)
	logger := log.New(StdlibWriter(LevelInfo, "app"), "", 0)
	logger.Printf("legacy line %d", 7)

	out := buf.String()
	if !strings.Contains(out, "INFO  [app] legacy line 7") {
		t.Fatalf("stdlib output not normalized: %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected exactly one line, got %q", out)
	}
}

// Unattributed third-party output must survive a raised threshold, otherwise
// raising the level to WARN would hide library errors entirely.
func TestStdlibWriterBypassesThreshold(t *testing.T) {
	buf := captureSink(t)
	SetLevel(LevelError)
	logger := log.New(StdlibWriter(LevelInfo, "app"), "", 0)
	logger.Print("third-party noise")
	if !strings.Contains(buf.String(), "[app] third-party noise") {
		t.Fatalf("stdlib output dropped by threshold: %q", buf.String())
	}
}

// syncWriter is a concurrency-safe sink for the race tests. bytes.Buffer is
// not safe for concurrent writes, and the races we care about are in the
// package's own atomics, not in the test's collector.
type syncWriter struct {
	mu sync.Mutex
	n  int
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	return len(p), nil
}

func TestConcurrentLevelChangeAndWrite(t *testing.T) {
	prevSink := sink.Load()
	prevLevel := CurrentLevel()
	t.Cleanup(func() {
		sink.Store(prevSink)
		SetLevel(prevLevel)
	})
	SetSink(&syncWriter{})
	SetLevel(LevelDebug)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Debug("uplink", "frame %d", j)
				Error("uplink", "err %d", j)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
			for j := 0; j < 200; j++ {
				SetLevel(levels[(i+j)%len(levels)])
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentSinkSwap(t *testing.T) {
	prevSink := sink.Load()
	prevLevel := CurrentLevel()
	t.Cleanup(func() {
		sink.Store(prevSink)
		SetLevel(prevLevel)
	})
	SetLevel(LevelDebug)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			Info("app", "line %d", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			SetSink(io.Discard)
		}
	}()
	wg.Wait()
}
