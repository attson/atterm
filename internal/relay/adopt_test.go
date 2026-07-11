package relay

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// fakePtyForAdopt is a PtyHost with no output (blocks Read forever) so the
// adopt read-loop doesn't spam OUT frames while we exercise the inbound
// side. Write is a no-op. Optionally implements FilePasteHost.
type fakePtyForAdopt struct {
	blockRead chan struct{}

	mu       sync.Mutex
	fileCall int
	lastSID  uuid.UUID
	lastPay  proto.PasteFilePayload
	panicOn  bool // if true, PasteFile isn't defined — omit the method
}

func newFakePty() *fakePtyForAdopt {
	return &fakePtyForAdopt{blockRead: make(chan struct{})}
}

func (f *fakePtyForAdopt) Read(p []byte) (int, error) {
	<-f.blockRead
	return 0, io.EOF
}
func (f *fakePtyForAdopt) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakePtyForAdopt) Resize(cols, rows uint16) error { return nil }
func (f *fakePtyForAdopt) stop()                       { close(f.blockRead) }

func (f *fakePtyForAdopt) PasteFile(_ context.Context, sid uuid.UUID, p proto.PasteFilePayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileCall++
	f.lastSID = sid
	f.lastPay = p
	return nil
}

func TestAdoptRoutesPasteFileToFilePasteHost(t *testing.T) {
	srv, _, ownerUser := serverWithSessionAndUser(t)
	sid := uuid.New()
	pty := newFakePty()
	defer pty.stop()

	cleanup := srv.AdoptSession(context.Background(), sid, proto.SessionInfo{ID: sid.String()}, pty, ownerUser)
	defer cleanup()

	sess, ok := srv.registry.Get(sid)
	if !ok {
		t.Fatal("session not registered")
	}
	body, err := json.Marshal(proto.PasteFilePayload{
		Filename: "foo.pdf", ContentType: "application/pdf", Data: []byte("abc"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok := sess.SendInbound(proto.Frame{Type: proto.TypePasteFile, SessionID: sid, Payload: body}); !ok {
		t.Fatal("SendInbound rejected")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		pty.mu.Lock()
		n := pty.fileCall
		pty.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	pty.mu.Lock()
	defer pty.mu.Unlock()
	if pty.fileCall != 1 {
		t.Fatalf("PasteFile calls = %d, want 1", pty.fileCall)
	}
	if pty.lastPay.Filename != "foo.pdf" || string(pty.lastPay.Data) != "abc" {
		t.Fatalf("payload mismatch: %+v", pty.lastPay)
	}
}

// hostWithoutPasteFile only implements PtyHost, not FilePasteHost. A
// PASTE_FILE frame reaching adopt should be dropped with a log line, not
// crash the loop.
type hostWithoutPasteFile struct {
	blockRead chan struct{}
}

func (h *hostWithoutPasteFile) Read(p []byte) (int, error) {
	<-h.blockRead
	return 0, io.EOF
}
func (h *hostWithoutPasteFile) Write(p []byte) (int, error) { return len(p), nil }
func (h *hostWithoutPasteFile) Resize(cols, rows uint16) error { return nil }

func TestAdoptDropsPasteFileWhenHostLacksInterface(t *testing.T) {
	srv, _, ownerUser := serverWithSessionAndUser(t)
	sid := uuid.New()
	pty := &hostWithoutPasteFile{blockRead: make(chan struct{})}
	defer close(pty.blockRead)

	cleanup := srv.AdoptSession(context.Background(), sid, proto.SessionInfo{ID: sid.String()}, pty, ownerUser)
	defer cleanup()

	sess, ok := srv.registry.Get(sid)
	if !ok {
		t.Fatal("session not registered")
	}
	body, _ := json.Marshal(proto.PasteFilePayload{Filename: "x.log", Data: []byte("y")})
	if ok := sess.SendInbound(proto.Frame{Type: proto.TypePasteFile, SessionID: sid, Payload: body}); !ok {
		t.Fatal("SendInbound rejected")
	}
	// Just wait a moment to ensure adopt goroutine had a chance to process
	// and did not panic. Absence of panic == pass.
	time.Sleep(50 * time.Millisecond)
}
