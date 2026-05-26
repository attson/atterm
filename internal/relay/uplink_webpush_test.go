package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

type recordingHTTPClientForRelayTest struct {
	mu sync.Mutex
	n  int
}

func (r *recordingHTTPClientForRelayTest) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return &http.Response{
		StatusCode: 201,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func (r *recordingHTTPClientForRelayTest) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func addRelayWebPushSubscription(t *testing.T, svc *webpush.Service, userID string) {
	t.Helper()
	sub := webpush.Subscription{Endpoint: "https://push.example/" + userID}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"
	if err := svc.AddSubscription(userID, sub); err != nil {
		t.Fatalf("AddSubscription: %v", err)
	}
}

func waitForRelayPushCount(t *testing.T, rec *recordingHTTPClientForRelayTest, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("push count = %d; want at least %d", rec.count(), want)
}

func sendAnnounceInfos(t *testing.T, ctx context.Context, c *websocket.Conn, sessions []proto.SessionInfo) {
	t.Helper()
	ann := proto.AnnouncePayload{Sessions: sessions}
	payload, _ := json.Marshal(ann)
	frame := proto.Frame{Type: proto.TypeAnnounce, Payload: payload}
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(frame)); err != nil {
		t.Fatalf("sendAnnounceInfos write: %v", err)
	}
}

func waitForSessionOwner(t *testing.T, srv *Server, sid uuid.UUID, ownerUserID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := srv.Registry().Get(sid)
		if ok && s.OwnerUserID == ownerUserID {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, ok := srv.Registry().Get(sid)
	if !ok {
		t.Fatalf("session %s not found in registry", sid)
	}
	t.Fatalf("session owner = %q; want %q", s.OwnerUserID, ownerUserID)
}

func TestUplinkCommandEventTriggersDispatchWhenSessionInManifest(t *testing.T) {
	const ownerUserID = "user_uplink_test"
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	// Register a subscription under the owner's user ID (not a token hash).
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"
	_ = svc.AddSubscription(ownerUserID, sub)
	// Set up a synthetic mirrors map containing one session id; register a
	// session in the registry so the resolver can find it.
	sid := uuid.New()
	info := proto.SessionInfo{
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	// The mirror session must carry the OwnerUserID so dispatch targets it.
	mirrorSess := session.New(sid, info)
	mirrorSess.OwnerUserID = ownerUserID
	ms := &mirrorState{sess: mirrorSess}
	mirrors := map[uuid.UUID]*mirrorState{sid: ms}
	var mu sync.Mutex
	// Encode the frame.
	frame, _ := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{ExitCode: 0, ElapsedMS: 12500, Label: "atterm"})
	// Call the handler.
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
	// Wait for at least one HTTP push.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("uplink command event did not trigger Web Push dispatch; got %d", rec.count())
	_ = context.Background
}

func TestUplinkWaitingInputStateTriggersWebPush(t *testing.T) {
	store, userID, apiToken := newUplinkTestStore(t)
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	addRelayWebPushSubscription(t, svc, userID)

	srv := NewServer(Config{Resolver: NewIdentityResolver(store), WebPush: svc})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readAndDiscardAuthInfo(t, ctx, conn)

	sid := uuid.New()
	sendAnnounce(t, ctx, conn, sid)
	waitForSessionOwner(t, srv, sid, userID)

	frame := proto.EncodeOut(sid, 1, []byte("Proceed? [y/N]"))
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(frame)); err != nil {
		t.Fatalf("write OUT: %v", err)
	}
	waitForRelayPushCount(t, rec, 1)
}

func TestUplinkRunningSessionIdleTimeoutTriggersWebPush(t *testing.T) {
	store, userID, apiToken := newUplinkTestStore(t)
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	addRelayWebPushSubscription(t, svc, userID)

	srv := NewServer(Config{
		Resolver:           NewIdentityResolver(store),
		WebPush:            svc,
		WebPushIdleTimeout: 30 * time.Millisecond,
	})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	readAndDiscardAuthInfo(t, ctx, conn)

	sid := uuid.New()
	now := time.Now().Unix()
	sendAnnounceInfos(t, ctx, conn, []proto.SessionInfo{{
		ID:               sid.String(),
		Command:          "bash",
		HostID:           uuid.New().String(),
		TaskState:        proto.TaskStateRunning,
		CurrentCommand:   "codex",
		CommandStartedAt: now,
		LastOutputAt:     now,
	}})
	waitForSessionOwner(t, srv, sid, userID)
	waitForRelayPushCount(t, rec, 1)
}

func TestUplinkDisconnectTriggersWebPush(t *testing.T) {
	store, userID, apiToken := newUplinkTestStore(t)
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	addRelayWebPushSubscription(t, svc, userID)

	srv := NewServer(Config{Resolver: NewIdentityResolver(store), WebPush: svc})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	readAndDiscardAuthInfo(t, ctx, conn)

	sid := uuid.New()
	sendAnnounce(t, ctx, conn, sid)
	waitForSessionOwner(t, srv, sid, userID)
	_ = conn.Close(websocket.StatusNormalClosure, "")
	waitForRelayPushCount(t, rec, 1)
}

func TestUplinkCommandEventDropsUnknownSession(t *testing.T) {
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"
	_ = svc.AddSubscription("user_drop_test", sub)
	// Mirrors map is empty (no sessions known to this uplink).
	mirrors := map[uuid.UUID]*mirrorState{}
	var mu sync.Mutex
	frame, _ := proto.EncodeCommandEvent(uuid.New(), proto.CommandEventPayload{ExitCode: 0})
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
	// Allow goroutines a moment.
	time.Sleep(150 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("dispatched %d pushes for unknown session; want 0", rec.count())
	}
}

func TestUplinkCommandEventNoOpWhenWebPushNil(t *testing.T) {
	srv := NewServer(Config{Token: "write-token", WebPush: nil})
	mirrors := map[uuid.UUID]*mirrorState{}
	var mu sync.Mutex
	frame, _ := proto.EncodeCommandEvent(uuid.New(), proto.CommandEventPayload{ExitCode: 0})
	// Must not panic.
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
}
