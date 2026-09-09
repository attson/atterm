package main

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// recordWriter records every Write as its own entry. The split between a line
// and its carriage return is the whole point of the sequence below, so a
// writer that concatenated would hide the only bug worth testing for.
type recordWriter struct {
	mu     sync.Mutex
	writes []string
}

func (w *recordWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, string(p))
	return len(p), nil
}

func (w *recordWriter) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.writes...)
}

func TestStartupCommandLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"blank only", "  \n\t\n", nil},
		{"single", "10.0.3.17", []string{"10.0.3.17"}},
		// The JumpServer flow: pick the asset at `Opt>`, then the system
		// user at `ID>`. Two prompts, so two lines.
		{"two steps", "php-compose-仓库\n2", []string{"php-compose-仓库", "2"}},
		{"crlf", "a\r\nb\r\n", []string{"a", "b"}},
		// A blank line in the middle is dropped rather than sent as a bare
		// Enter: an accidental trailing newline is far more likely than a
		// deliberate "just press Enter here" step, and a stray Enter at a
		// JumpServer prompt re-prints the menu.
		{"drops interior blanks", "a\n\n\nb\n", []string{"a", "b"}},
		{"trims trailing spaces", "  a  \n b ", []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := startupCommandLines(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("startupCommandLines(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestStartupDelay(t *testing.T) {
	if got := startupDelay(0); got != defaultStartupDelay {
		t.Errorf("zero should default, got %v", got)
	}
	if got := startupDelay(-5); got != defaultStartupDelay {
		t.Errorf("negative should default, got %v", got)
	}
	if got := startupDelay(3000); got != 3*time.Second {
		t.Errorf("3000ms = %v", got)
	}
	if got := startupDelay(5); got != minStartupDelay {
		t.Errorf("below floor should clamp, got %v", got)
	}
	if got := startupDelay(999999); got != maxStartupDelay {
		t.Errorf("above ceiling should clamp, got %v", got)
	}
}

// TestSendStartupCommandSplitsTextFromReturn is the load-bearing one. Sending
// "text\r" in a single write reads as a *paste* to a program in raw mode, and
// this repo has fixed that same bug three times on the template path (#63,
// #110, #129). The sequence therefore writes the line and its carriage return
// as two separate writes, forever.
func TestSendStartupCommandSplitsTextFromReturn(t *testing.T) {
	w := &recordWriter{}
	sendStartupCommand(context.Background(), w, []string{"php-compose-仓库", "2"}, time.Millisecond)

	want := []string{"php-compose-仓库", "\r", "2", "\r"}
	if got := w.snapshot(); !reflect.DeepEqual(got, want) {
		t.Errorf("writes = %#v, want %#v", got, want)
	}
}

func TestSendStartupCommandWritesNothingForNoLines(t *testing.T) {
	w := &recordWriter{}
	sendStartupCommand(context.Background(), w, nil, time.Millisecond)
	if got := w.snapshot(); len(got) != 0 {
		t.Errorf("expected no writes, got %#v", got)
	}
}

// A cancelled context stops the sequence rather than typing the rest of it
// into a session that is going away.
func TestSendStartupCommandStopsOnContextCancel(t *testing.T) {
	w := &recordWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sendStartupCommand(ctx, w, []string{"a", "b"}, time.Second)
	if got := w.snapshot(); len(got) != 0 {
		t.Errorf("expected no writes after cancel, got %#v", got)
	}
}
