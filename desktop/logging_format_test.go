package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatLogLine(t *testing.T) {
	ts := time.Date(2026, 6, 22, 15, 4, 5, 123_000_000, time.UTC)
	cases := []struct {
		level, tag, msg, want string
	}{
		{"DEBUG", "pty-input", "write n=1 hex=1b LONE-ESC",
			"2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC\n"},
		{"INFO", "app", "hello",
			"2026/06/22 15:04:05.123 INFO  [app] hello\n"},
		{"WARN", "relay", "dropping",
			"2026/06/22 15:04:05.123 WARN  [relay] dropping\n"},
		{"ERROR", "app", "boom",
			"2026/06/22 15:04:05.123 ERROR [app] boom\n"},
	}
	for _, c := range cases {
		got := formatLogLine(ts, c.level, c.tag, c.msg)
		if got != c.want {
			t.Errorf("formatLogLine(%s) = %q, want %q", c.level, got, c.want)
		}
	}
}

func TestEmitWritesFormattedLine(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}
	m.Emit("DEBUG", "pty-input", "hi")
	got := buf.String()
	if !strings.Contains(got, " DEBUG [pty-input] hi\n") {
		t.Errorf("Emit wrote %q, missing formatted suffix", got)
	}
}

func TestLegacyWriteNormalized(t *testing.T) {
	var buf strings.Builder
	m := &loggingManager{currentWriter: &buf}
	_, _ = m.Write([]byte("client: dropping frame\n"))
	got := buf.String()
	if !strings.Contains(got, " INFO  [app] client: dropping frame\n") {
		t.Errorf("legacy Write produced %q, want normalized INFO [app] line", got)
	}
}
