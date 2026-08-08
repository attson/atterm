package main

import (
	"strings"
	"testing"

	"github.com/attson/atterm/internal/logging"
)

func TestPtyInputDebugEnabledOrDefault(t *testing.T) {
	var c appConfig
	if c.PtyInputDebugEnabledOrDefault() {
		t.Fatal("default should be false")
	}
	v := true
	c.PtyInputDebugEnabled = &v
	if !c.PtyInputDebugEnabledOrDefault() {
		t.Fatal("should be true when set")
	}
}

func TestPtyInputDebugTag(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want string
	}{
		"lone esc": {[]byte{0x1b}, " LONE-ESC"},
		"esc lead": {[]byte{0x1b, '[', 'O'}, " ESC-LEAD"},
		"plain":    {[]byte("a"), ""},
		"empty":    {[]byte{}, ""},
	}
	for name, c := range cases {
		if got := ptyInputDebugTag(c.in); got != c.want {
			t.Errorf("%s: ptyInputDebugTag = %q, want %q", name, got, c.want)
		}
	}
}

func TestLogPtyInputGating(t *testing.T) {
	buf := captureLogSink(t)

	off := &configStore{}
	logPtyInput(off, []byte{0x1b})
	if buf.Len() != 0 {
		t.Fatalf("disabled: expected no output, got %q", buf.String())
	}

	on := &configStore{}
	v := true
	on.cfg.PtyInputDebugEnabled = &v
	logPtyInput(on, []byte{0x1b})
	got := buf.String()
	if !strings.Contains(got, "DEBUG [pty-input] write n=1 hex=1b LONE-ESC") {
		t.Fatalf("enabled: missing expected debug line, got %q", got)
	}
}

// The toggle is the gate for this trace: a user who turns it on must see
// output regardless of where the general log level happens to sit.
func TestLogPtyInputIgnoresLogLevel(t *testing.T) {
	buf := captureLogSink(t)
	logging.SetLevel(logging.LevelError)

	on := &configStore{}
	v := true
	on.cfg.PtyInputDebugEnabled = &v
	logPtyInput(on, []byte{0x1b, '[', 'O'})

	if !strings.Contains(buf.String(), "DEBUG [pty-input] write n=3 hex=1b5b4f ESC-LEAD") {
		t.Fatalf("pty-input trace suppressed by log level: %q", buf.String())
	}
}
