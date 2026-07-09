package feishu

import (
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
