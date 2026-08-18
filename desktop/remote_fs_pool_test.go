package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// startFSTestRelay serves a fake /uplink endpoint. handler runs with the
// accepted connection after the initial ANNOUNCE has been drained.
func startFSTestRelay(t *testing.T, handler func(ctx context.Context, c *websocket.Conn)) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/uplink", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		if _, _, err := c.Read(r.Context()); err != nil { // ANNOUNCE
			return
		}
		handler(r.Context(), c)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// TestSlowFSRequestDoesNotBlockKeystrokes is the regression this whole change
// exists for: a filesystem request that takes a long time (an SFTP round trip,
// a sleeping external drive, a network mount) must not stall the uplink read
// loop, because that same loop carries every session's keystrokes.
//
// The assertion is about the read loop's liveness, not about the pool: an
// implementation that still called fs.handle synchronously would never reach
// the IN frame and would fail on the inbound-queue wait below.
func TestSlowFSRequestDoesNotBlockKeystrokes(t *testing.T) {
	sessionID := uuid.New()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	responses := make(chan proto.FSResponsePayload, 4)

	addr := startFSTestRelay(t, func(ctx context.Context, c *websocket.Conn) {
		reqPayload, err := proto.EncodeFSHead(proto.FSRequestPayload{
			RequestID: "slow-1",
			Op:        "list_dir",
			Path:      "/tmp",
		})
		if err != nil {
			return
		}
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeFSRequest, SessionID: sessionID, Payload: reqPayload,
		})); err != nil {
			return
		}
		// The keystroke is written immediately behind the slow FS request.
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeIn, SessionID: sessionID, Payload: []byte("k"),
		})); err != nil {
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			f, err := proto.Unmarshal(data)
			if err != nil || f.Type != proto.TypeFSResponse {
				continue
			}
			resp, err := proto.DecodeFSResponse(f.Payload, nil, f.SessionID)
			if err != nil {
				continue
			}
			select {
			case responses <- resp:
			default:
			}
		}
	})

	host := newTestRelayHost(t)
	sess := session.New(sessionID, proto.SessionInfo{ID: sessionID.String(), Cols: 80, Rows: 24})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	u := newUplink("ws://"+addr, "tok", proto.RemotePermissionFull, host, nil, nil, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	u.fsExec = func(sid uuid.UUID, req proto.FSRequestPayload) proto.Frame {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return remoteFSResponseFrame(sid, proto.FSResponsePayload{RequestID: req.RequestID, OK: true}, nil)
	}

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("FS request never reached the executor")
	}

	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "k" {
			t.Fatalf("inbound frame = type %v payload %q, want IN %q", f.Type, f.Payload, "k")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("IN frame was not processed while an FS request was in flight: the uplink read loop is blocked on the filesystem")
	}

	select {
	case resp := <-responses:
		t.Fatalf("FS response arrived before the executor was released: %+v", resp)
	default:
	}

	close(release)
	select {
	case resp := <-responses:
		if !resp.OK || resp.RequestID != "slow-1" {
			t.Fatalf("released FS response = %+v, want ok for slow-1", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FS response never arrived after the executor was released")
	}

	cancel()
	<-done
}

// TestUplinkCloseDoesNotWaitOnInFlightFSRequest covers shutdown: a worker
// stuck inside a filesystem call cannot be interrupted, so the connection must
// tear down without waiting for it, and the worker must finish afterwards
// without touching anything that has already been torn down.
func TestUplinkCloseDoesNotWaitOnInFlightFSRequest(t *testing.T) {
	sessionID := uuid.New()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	finished := make(chan struct{})

	addr := startFSTestRelay(t, func(ctx context.Context, c *websocket.Conn) {
		reqPayload, err := proto.EncodeFSHead(fsPoolRequest("shutdown-1"))
		if err != nil {
			return
		}
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeFSRequest, SessionID: sessionID, Payload: reqPayload,
		})); err != nil {
			return
		}
		<-ctx.Done()
	})

	host := newTestRelayHost(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	u := newUplink("ws://"+addr, "tok", proto.RemotePermissionFull, host, nil, nil, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	u.fsExec = func(sid uuid.UUID, req proto.FSRequestPayload) proto.Frame {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		close(finished)
		return remoteFSResponseFrame(sid, proto.FSResponsePayload{RequestID: req.RequestID, OK: true}, nil)
	}

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("FS request never reached the executor")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runOnce did not return while an FS request was still in flight")
	}

	// The worker outlives the connection; releasing it must neither panic
	// nor block (its response is simply dropped).
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight FS worker never finished after the connection closed")
	}
}

func fsPoolRequest(id string) proto.FSRequestPayload {
	return proto.FSRequestPayload{RequestID: id, Op: "list_dir", Path: "/tmp"}
}

func recvFSPoolResponse(t *testing.T, out <-chan proto.Frame) proto.FSResponsePayload {
	t.Helper()
	select {
	case f := <-out:
		return decodeFSResponse(t, f)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an FS response frame")
		return proto.FSResponsePayload{}
	}
}

// TestFSWorkerPoolFullReturnsBusy pins the "refuse, don't queue" decision: a
// session that already has its budget of requests in flight gets an immediate
// error it can render, rather than a wait it cannot see. The refused request
// must never run afterwards.
func TestFSWorkerPoolFullReturnsBusy(t *testing.T) {
	fs, _, _ := makeRemoteFS(t)
	release := make(chan struct{})
	var calls int32
	fs.exec = func(sid uuid.UUID, req proto.FSRequestPayload) proto.Frame {
		atomic.AddInt32(&calls, 1)
		<-release
		return remoteFSResponseFrame(sid, proto.FSResponsePayload{RequestID: req.RequestID, OK: true}, nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan proto.Frame, 16)
	pool := newFSWorkerPool(ctx, out, proto.RemotePermissionFull, fs, 2)

	sessionID := uuid.New()
	pool.submit(sessionID, fsPoolRequest("a"))
	pool.submit(sessionID, fsPoolRequest("b"))

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&calls) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d requests started; want 2 running concurrently", atomic.LoadInt32(&calls))
		}
		time.Sleep(5 * time.Millisecond)
	}

	submitted := make(chan struct{})
	go func() {
		pool.submit(sessionID, fsPoolRequest("busy"))
		close(submitted)
	}()
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("submit blocked while the pool was full: a full pool must refuse, not queue")
	}

	busy := recvFSPoolResponse(t, out)
	if busy.RequestID != "busy" || busy.OK || !strings.Contains(busy.Error, "busy") {
		t.Fatalf("full-pool response = %+v, want a busy error for request \"busy\"", busy)
	}

	// The cap is per session: another session still gets served.
	pool.submit(uuid.New(), fsPoolRequest("other-session"))
	deadline = time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&calls) < 3 {
		if time.Now().After(deadline) {
			t.Fatal("a second session was refused while the first session filled its budget")
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(release)
	got := map[string]bool{}
	for i := 0; i < 3; i++ {
		resp := recvFSPoolResponse(t, out)
		got[resp.RequestID] = resp.OK
	}
	for _, id := range []string{"a", "b", "other-session"} {
		if !got[id] {
			t.Fatalf("missing ok response for %q; got %+v", id, got)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("executor ran %d times, want 3: the refused request must not be queued", n)
	}
	select {
	case f := <-out:
		t.Fatalf("unexpected extra response frame: %+v", decodeFSResponse(t, f))
	case <-time.After(100 * time.Millisecond):
	}
}

// TestFSResponsesStillMatchTheirRequestID guards the ordering assumption that
// serial execution used to provide for free: with several requests completing
// concurrently, each response must carry its own request's result.
func TestFSResponsesStillMatchTheirRequestID(t *testing.T) {
	fs, _, root := makeRemoteFS(t)
	ids := []string{"r1", "r2", "r3", "r4"}
	for _, id := range ids {
		if err := os.WriteFile(filepath.Join(root, id+".txt"), []byte("body-"+id), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Gate every request so they all complete in a scramble rather than in
	// submission order; the real executor still does the work.
	gate := make(chan struct{})
	fs.exec = func(sid uuid.UUID, req proto.FSRequestPayload) proto.Frame {
		<-gate
		return fs.execute(sid, req)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan proto.Frame, 16)
	pool := newFSWorkerPool(ctx, out, proto.RemotePermissionFull, fs, len(ids))

	sessionID := uuid.New()
	for _, id := range ids {
		pool.submit(sessionID, proto.FSRequestPayload{
			RequestID: id,
			Op:        "read_file",
			Path:      filepath.Join(root, id+".txt"),
			MaxBytes:  1024,
		})
	}
	close(gate)

	seen := map[string]string{}
	for range ids {
		resp := recvFSPoolResponse(t, out)
		if !resp.OK || resp.Content == nil {
			t.Fatalf("response %+v not ok", resp)
		}
		if _, dup := seen[resp.RequestID]; dup {
			t.Fatalf("duplicate response for request_id %q", resp.RequestID)
		}
		seen[resp.RequestID] = string(resp.Content.Data)
	}
	for _, id := range ids {
		if seen[id] != "body-"+id {
			t.Fatalf("request %q got body %q, want %q", id, seen[id], "body-"+id)
		}
	}
}
