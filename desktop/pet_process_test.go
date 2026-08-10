package main

import (
	"encoding/json"
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

// attached builds a petProcess wired to a fake stdin, bypassing exec so the
// push policy can be tested without spawning a window.
func attached() (*petProcess, *fakePipe) {
	pipe := &fakePipe{}
	p := &petProcess{running: true, stdin: pipe}
	return p, pipe
}

func TestPetPushStateWritesNDJSON(t *testing.T) {
	p, pipe := attached()

	if err := p.PushState(`{"mood":"running"}`); err != nil {
		t.Fatalf("push: %v", err)
	}
	got := pipe.String()
	if got != `{"mood":"running"}`+"\n" {
		t.Fatalf("expected one newline-terminated line, got %q", got)
	}
}

func TestPetPushStateSkipsIdenticalPayload(t *testing.T) {
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

func TestPetPushStateThrottlesRapidChanges(t *testing.T) {
	p, pipe := attached()

	_ = p.PushState(`{"n":1}`)
	// A distinct payload arriving inside the throttle window is dropped, not
	// queued — the next snapshot is complete, so nothing is lost.
	_ = p.PushState(`{"n":2}`)

	if n := strings.Count(pipe.String(), "\n"); n != 1 {
		t.Fatalf("expected throttling to drop the second push; got %d lines", n)
	}

	p.lastPushAt = time.Now().Add(-petPushInterval - time.Millisecond)
	_ = p.PushState(`{"n":3}`)
	if n := strings.Count(pipe.String(), "\n"); n != 2 {
		t.Fatalf("expected push after the throttle window; got %d lines", n)
	}
}

func TestPetPushStateNoopWhenNotRunning(t *testing.T) {
	pipe := &fakePipe{}
	p := &petProcess{running: false, stdin: pipe}

	// Callers push on every session-list change; a disabled pet must not make
	// that an error path.
	if err := p.PushState(`{"mood":"idle"}`); err != nil {
		t.Fatalf("push while stopped must be a no-op, got %v", err)
	}
	if pipe.String() != "" {
		t.Fatalf("nothing should have been written, got %q", pipe.String())
	}
}

func TestPetReadEventsDecodesLines(t *testing.T) {
	r, w := io.Pipe()

	var (
		mu   sync.Mutex
		got  []petEvent
		done = make(chan struct{})
	)
	p := &petProcess{onEvent: func(ev petEvent) {
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

func TestPetBootstrapCarriesNoCredentials(t *testing.T) {
	// Red line #21: nothing secret may reach the child. Assert on the encoded
	// shape so adding a token field to petBootstrap fails loudly here.
	blob, err := json.Marshal(petBootstrap{
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
			t.Fatalf("unexpected field %q in pet bootstrap — the companion process must never receive credentials", k)
		}
	}
}

func TestPetStopClosesStdinAndClearsRunning(t *testing.T) {
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

func TestPetStopIsIdempotent(t *testing.T) {
	p, _ := attached()
	p.Stop()
	p.Stop() // must not panic on the now-nil cmd/stdin
	if p.Running() {
		t.Fatal("Running() must stay false")
	}
}
