// Package logging is the single source of truth for atterm's plain-text log
// format and its write threshold.
//
// It is deliberately tiny — no logrus/zap/slog (see docs/spec/conventions.md).
// Every component (desktop app, internal packages, the relay server) formats
// lines the same way, so one parser (desktop/frontend/src/lib/parseLogLine.ts)
// renders logs from all of them:
//
//	2026/08/08 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC
//
// The sink receives fully formatted lines. Callers wire it once at startup:
// the desktop app points it at its rotating file writer, the relay at stderr.
//
// internal/ must not import desktop/ (AGENTS.md red line #5), which is why the
// format lives here rather than in desktop/logging.go.
package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Level orders the four severities. Records below the configured threshold are
// dropped before their message is formatted.
type Level int8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// TimeLayout is the timestamp format every log line starts with. Millisecond
// precision matters: it is what makes a split escape sequence visible in the
// pty-input trace.
const TimeLayout = "2006/01/02 15:04:05.000"

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// ParseLevel maps a case-insensitive level name to a Level. It reports false
// for anything it does not recognise so callers can fall back to a default
// rather than silently logging at the wrong threshold.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return LevelDebug, true
	case "INFO":
		return LevelInfo, true
	case "WARN", "WARNING":
		return LevelWarn, true
	case "ERROR":
		return LevelError, true
	default:
		return LevelInfo, false
	}
}

// ParseLevelOr is ParseLevel with an explicit fallback, for config paths where
// a bad value must not stop the process from starting.
func ParseLevelOr(s string, fallback Level) Level {
	if l, ok := ParseLevel(s); ok {
		return l
	}
	return fallback
}

var (
	// sink holds an *io.Writer so it can be swapped atomically.
	sink atomic.Pointer[io.Writer]
	// threshold is the minimum level that reaches the sink.
	threshold atomic.Int32
)

func init() {
	threshold.Store(int32(LevelInfo))
	// Default to stderr, matching the standard library. A binary that forgets
	// to call SetSink then still prints its logs somewhere visible instead of
	// silently dropping them.
	w := io.Writer(os.Stderr)
	sink.Store(&w)
}

// SetSink installs the writer that receives formatted lines. A nil writer
// disables output. Safe to call at any time; in-flight writes use whichever
// sink they loaded.
func SetSink(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	sink.Store(&w)
}

// SetLevel sets the minimum level written to the sink. Hot-appliable: the
// relay flips it from its admin config, the desktop app from Settings.
func SetLevel(l Level) { threshold.Store(int32(l)) }

// CurrentLevel returns the active threshold.
func CurrentLevel() Level { return Level(threshold.Load()) }

// Enabled reports whether a record at level l would be written. Use it to skip
// building expensive arguments before calling Debug on a hot path.
func Enabled(l Level) bool { return l >= CurrentLevel() }

// FormatLine renders one log record. Exported so tests and adapters (e.g. the
// desktop manager's legacy stdlib-log path) produce byte-identical output.
//
// LEVEL is right-padded to width 5 so the tag column lines up.
func FormatLine(t time.Time, l Level, tag, msg string) string {
	var b strings.Builder
	b.Grow(len(TimeLayout) + len(tag) + len(msg) + 12)
	b.WriteString(t.Format(TimeLayout))
	b.WriteByte(' ')
	b.WriteString(padLevel(l.String()))
	b.WriteString(" [")
	b.WriteString(tag)
	b.WriteString("] ")
	b.WriteString(msg)
	b.WriteByte('\n')
	return b.String()
}

func padLevel(level string) string {
	for len(level) < 5 {
		level += " "
	}
	return level
}

// write pushes an already-formatted line at the sink. Logging must never be a
// source of failure, so a sink error is dropped rather than propagated.
func write(line string) {
	w := sink.Load()
	if w == nil {
		return
	}
	_, _ = io.WriteString(*w, line)
}

// Emit writes a record if it clears the threshold.
func Emit(l Level, tag, msg string) {
	if !Enabled(l) {
		return
	}
	write(FormatLine(time.Now(), l, tag, msg))
}

// EmitAt is Emit with a caller-supplied timestamp. Frontend records are
// batched before they cross the Wails boundary, so they carry the time they
// happened rather than the time they were flushed.
func EmitAt(t time.Time, l Level, tag, msg string) {
	if !Enabled(l) {
		return
	}
	write(FormatLine(t, l, tag, msg))
}

// EmitForced writes regardless of the threshold. Reserved for records behind
// their own explicit user-facing toggle — currently only the pty-input byte
// trace, where the toggle *is* the gate and silently honouring a higher
// threshold would look like the feature is broken.
func EmitForced(l Level, tag, msg string) {
	write(FormatLine(time.Now(), l, tag, msg))
}

// The leveled helpers check the threshold before formatting, so a below-
// threshold call on a per-frame or per-keystroke path allocates nothing.

func Debug(tag, format string, args ...any) { logf(LevelDebug, tag, format, args...) }
func Info(tag, format string, args ...any)  { logf(LevelInfo, tag, format, args...) }
func Warn(tag, format string, args ...any)  { logf(LevelWarn, tag, format, args...) }
func Error(tag, format string, args ...any) { logf(LevelError, tag, format, args...) }

func logf(l Level, tag, format string, args ...any) {
	if !Enabled(l) {
		return
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	write(FormatLine(time.Now(), l, tag, msg))
}
