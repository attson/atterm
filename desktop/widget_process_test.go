package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePipe is an io.WriteCloser that records everything written to it.
type fakePipe struct {
	mu     sync.Mutex
	buf    strings.Builder
	closed bool
}

func (f *fakePipe) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}

func (f *fakePipe) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakePipe) String() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.String()
}

// attached builds a widgetProcess wired to a fake stdin, bypassing exec so the
// push policy can be tested without spawning a window.
func attached() (*widgetProcess, *fakePipe) {
	pipe := &fakePipe{}
	p := &widgetProcess{running: true, stdin: pipe}
	return p, pipe
}

func TestWidgetPushStateWritesNDJSON(t *testing.T) {
	p, pipe := attached()

	if err := p.PushState(`{"mood":"running"}`); err != nil {
		t.Fatalf("push: %v", err)
	}
	got := pipe.String()
	if got != `{"mood":"running"}`+"\n" {
		t.Fatalf("expected one newline-terminated line, got %q", got)
	}
}

func TestWidgetPushStateSkipsIdenticalPayload(t *testing.T) {
	p, pipe := attached()
	payload := `{"mood":"running"}`

	_ = p.PushState(payload)
	// Clear the throttle so only the dedupe rule can suppress the second push.
	p.lastPushAt = time.Now().Add(-time.Hour)
	_ = p.PushState(payload)

	if n := strings.Count(pipe.String(), "\n"); n != 1 {
		t.Fatalf("identical payload must not be re-sent; got %d lines", n)
	}
}

func TestWidgetPushStateThrottlesRapidChanges(t *testing.T) {
	p, pipe := attached()

	_ = p.PushState(`{"n":1}`)
	// A distinct payload arriving inside the throttle window is held back, not
	// written immediately.
	_ = p.PushState(`{"n":2}`)

	if n := strings.Count(pipe.String(), "\n"); n != 1 {
		t.Fatalf("expected the second push to be throttled; got %d lines", n)
	}
}

// Regression: the throttle used to DROP anything inside its window. A command
// that starts and finishes within 200ms — `ls` does — pushed "running" and
// then "completed" moments later, and the second was discarded. The widget
// then showed a finished command as still running until some unrelated
// session-list change happened to land outside a throttle window.
func TestWidgetPushStateDeliversTheLastStateAfterTheWindow(t *testing.T) {
	p, pipe := attached()

	_ = p.PushState(`{"state":"running"}`)
	_ = p.PushState(`{"state":"completed"}`) // throttled, must not be lost

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(pipe.String(), `{"state":"completed"}`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal state never delivered; pipe had %q", pipe.String())
}

func TestWidgetPushStateCoalescesToTheNewestPayload(t *testing.T) {
	p, pipe := attached()

	_ = p.PushState(`{"n":1}`)
	for i := 2; i <= 6; i++ {
		_ = p.PushState(fmt.Sprintf(`{"n":%d}`, i))
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(pipe.String(), `{"n":6}`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	out := pipe.String()
	if !strings.Contains(out, `{"n":6}`) {
		t.Fatalf("newest payload never delivered; got %q", out)
	}
	// Each snapshot is complete on its own, so only the newest needs to land;
	// replaying the intermediates would be wasted IPC and visible flicker.
	for _, stale := range []string{`{"n":2}`, `{"n":3}`, `{"n":4}`, `{"n":5}`} {
		if strings.Contains(out, stale) {
			t.Fatalf("intermediate payload %s should have been coalesced away; got %q", stale, out)
		}
	}
}

func TestWidgetStopCancelsAPendingFlush(t *testing.T) {
	p, pipe := attached()

	_ = p.PushState(`{"n":1}`)
	_ = p.PushState(`{"n":2}`) // queued behind the throttle
	p.Stop()                   // must cancel it — the pipe is closed now

	time.Sleep(widgetPushInterval + 150*time.Millisecond)
	if strings.Contains(pipe.String(), `{"n":2}`) {
		t.Fatal("a queued flush fired after Stop; it would write to a closed pipe")
	}
}

func TestWidgetPushStateNoopWhenNotRunning(t *testing.T) {
	pipe := &fakePipe{}
	p := &widgetProcess{running: false, stdin: pipe}

	// Callers push on every session-list change; a disabled widget must not make
	// that an error path.
	if err := p.PushState(`{"mood":"idle"}`); err != nil {
		t.Fatalf("push while stopped must be a no-op, got %v", err)
	}
	if pipe.String() != "" {
		t.Fatalf("nothing should have been written, got %q", pipe.String())
	}
}

func TestWidgetReadEventsDecodesLines(t *testing.T) {
	r, w := io.Pipe()

	var (
		mu   sync.Mutex
		got  []widgetEvent
		done = make(chan struct{})
	)
	p := &widgetProcess{onEvent: func(ev widgetEvent) {
		mu.Lock()
		got = append(got, ev)
		if len(got) == 3 {
			close(done)
		}
		mu.Unlock()
	}}

	go p.readEvents(r)

	lines := []string{
		`{"type":"activate","sessionId":"abc"}`,
		`not json at all`, // must be skipped without killing the stream
		`{"type":"collapse","collapsed":true}`,
		`{"type":"move","x":1400,"y":820}`,
	}
	go func() {
		for _, l := range lines {
			_, _ = io.WriteString(w, l+"\n")
		}
		_ = w.Close()
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for decoded events")
	}

	mu.Lock()
	defer mu.Unlock()
	if got[0].Type != "activate" || got[0].SessionID != "abc" {
		t.Fatalf("activate decoded wrong: %+v", got[0])
	}
	if got[1].Type != "collapse" || !got[1].Collapsed {
		t.Fatalf("collapse decoded wrong: %+v", got[1])
	}
	if got[2].Type != "move" || got[2].X != 1400 || got[2].Y != 820 {
		t.Fatalf("move decoded wrong: %+v", got[2])
	}
}

func TestWidgetBootstrapCarriesNoCredentials(t *testing.T) {
	// Red line #21: nothing secret may reach the child. Assert on the encoded
	// shape so adding a token field to widgetBootstrap fails loudly here.
	blob, err := json.Marshal(widgetBootstrap{
		Type: "bootstrap", Collapsed: true, X: 10, Y: 20, Locale: "zh-CN",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(blob, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	allowed := map[string]bool{"type": true, "collapsed": true, "x": true, "y": true, "locale": true}
	for k := range fields {
		if !allowed[k] {
			t.Fatalf("unexpected field %q in widget bootstrap — the companion process must never receive credentials", k)
		}
	}
}

func TestWidgetStopClosesStdinAndClearsRunning(t *testing.T) {
	p, pipe := attached()

	// No cmd attached: Stop must still close stdin (the child's EOF suicide
	// path) and flip the flag rather than panicking on a nil process.
	p.Stop()

	if p.Running() {
		t.Fatal("Running() must be false after Stop")
	}
	if !pipe.closed {
		t.Fatal("Stop must close stdin so the child sees EOF")
	}
}

func TestWidgetStopIsIdempotent(t *testing.T) {
	p, _ := attached()
	p.Stop()
	p.Stop() // must not panic on the now-nil cmd/stdin
	if p.Running() {
		t.Fatal("Running() must stay false")
	}
}
