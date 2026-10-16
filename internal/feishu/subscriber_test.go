package feishu

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestFeishuSubscriber_DrainsOutToChunker(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})

	var mu sync.Mutex
	var flushes []string
	flush := func(body string) {
		mu.Lock()
		flushes = append(flushes, body)
		mu.Unlock()
	}

	sub := AttachFeishuSubscriber(sess, "ou_owner", true /*pumpPTYBytes*/, flush)
	defer sub.Detach()

	sess.PushOut(1, []byte("hello world\n"))

	// Give the goroutine a moment to drain + flush window to elapse.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(flushes) == 0 {
		t.Fatal("expected at least one flush")
	}
	if !strings.Contains(flushes[len(flushes)-1], "hello world") {
		t.Errorf("last flush = %q, want it to contain 'hello world'", flushes[len(flushes)-1])
	}
}

func TestFeishuSubscriber_StripsOutSequencePrefix(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	flushed := make(chan string, 4)
	sub := AttachFeishuSubscriber(sess, "ou_owner", true, func(body string) {
		flushed <- body
	})
	defer sub.Detach()

	// The TypeOut wire payload starts with an 8-byte sequence number. These
	// low bytes are LF, CR, and space respectively; leaking the prefix into
	// the roller used to create output-dependent blank lines and indentation.
	sess.PushOut(10, []byte("ten\n"))
	sess.PushOut(13, []byte("thirteen\n"))
	sess.PushOut(32, []byte("thirty-two\n"))

	var body string
	deadline := time.After(time.Second)
	for !strings.Contains(body, "thirty-two") {
		select {
		case body = <-flushed:
		case <-deadline:
			t.Fatal("timed out waiting for shell body flush")
		}
	}
	if body != "ten\nthirteen\nthirty-two" {
		t.Fatalf("body contains TypeOut sequence bytes: %q", body)
	}
}

func TestFeishuSubscriber_NavigatePageFlushesHistory(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	flushed := make(chan string, 8)
	sub := AttachFeishuSubscriber(sess, "ou_owner", true, func(body string) {
		flushed <- body
	})
	defer sub.Detach()

	var output strings.Builder
	for i := 1; i <= 65; i++ {
		fmt.Fprintf(&output, "line-%02d\n", i)
	}
	sess.PushOut(1, []byte(output.String()))

	var latest string
	deadline := time.After(time.Second)
	for !strings.Contains(latest, "line-65") {
		select {
		case latest = <-flushed:
		case <-deadline:
			t.Fatal("timed out waiting for latest page")
		}
	}
	if !sub.NavigatePage("previous") {
		t.Fatal("previous page should change the roller")
	}
	select {
	case previous := <-flushed:
		if !strings.Contains(previous, "line-42") || !strings.Contains(previous, "line-53") || strings.Contains(previous, "line-65") {
			t.Fatalf("previous page mismatch: %q", previous)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for previous-page flush")
	}
}

// AI sessions rely on AIChunker (👤/🤖/🛠 turn events) as the body source.
// pumpPTYBytes=false must suppress the shell roller's flush entirely, or the
// claude TUI's per-frame ANSI noise overwrites the AI body on every PATCH.
func TestFeishuSubscriber_PumpPTYBytesFalse_SuppressesShellFlush(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeAI})

	var mu sync.Mutex
	flushed := 0
	flush := func(string) {
		mu.Lock()
		flushed++
		mu.Unlock()
	}

	sub := AttachFeishuSubscriber(sess, "ou_owner", false /*pumpPTYBytes*/, flush)
	defer sub.Detach()

	// Pump shell-shaped noise; with pumpPTYBytes=false the drain loop must
	// not feed the chunker, so no flush should ever fire from this side.
	sess.PushOut(1, []byte("noise that would otherwise overwrite the AI body\n"))
	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if flushed != 0 {
		t.Errorf("flush fired %d times; want 0 when pumpPTYBytes=false", flushed)
	}
}

func TestFeishuSubscriber_DisablingPTYBytesStopsLaterShellFlushes(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	flushed := make(chan string, 8)
	sub := AttachFeishuSubscriber(sess, "ou_owner", true, func(body string) {
		flushed <- body
	})
	defer sub.Detach()

	sess.PushOut(1, []byte("shell-before-upgrade\n"))
	select {
	case body := <-flushed:
		if !strings.Contains(body, "shell-before-upgrade") {
			t.Fatalf("initial shell flush = %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for initial shell flush")
	}

	sub.SetPumpPTYBytes(false)
	for i := 0; i < 20; i++ {
		sess.PushOut(uint64(i+2), []byte("ai-tui-redraw-noise\r"))
	}
	select {
	case body := <-flushed:
		t.Fatalf("raw PTY flushed after AI upgrade: %q", body)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestFeishuSubscriber_DoesNotAutoClaimDriver(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	sub := AttachFeishuSubscriber(sess, "ou_owner", true, func(string) {})
	defer sub.Detach()

	if sess.DriverClientID() != "" {
		t.Errorf("driver should be empty (viewer), got %q", sess.DriverClientID())
	}
}

func TestFeishuSubscriber_ClaimDriverPromotes(t *testing.T) {
	sess := session.New(uuid.New(), proto.SessionInfo{Type: session.SessionTypeShell})
	sub := AttachFeishuSubscriber(sess, "ou_owner", true, func(string) {})
	defer sub.Detach()

	sub.ClaimDriver()
	if sess.DriverClientID() != feishuDriverClientID(sub) {
		t.Errorf("driver = %q, want %q", sess.DriverClientID(), feishuDriverClientID(sub))
	}
}
