package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// TestNonDriverInDroppedAtRelay: a viewer subscriber's IN frames are silently
// dropped at the relay rather than reaching sess.Inbound(). Driver's IN does
// reach Inbound.
func TestNonDriverInDroppedAtRelay(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	driver := dialClientAttach(t, ctx, httpSrv, tok, id, "client-alpha")
	defer driver.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, driver)

	viewer := dialClientAttach(t, ctx, httpSrv, tok, id, "client-beta")
	defer viewer.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, viewer)

	writeClientFrame(t, ctx, viewer, proto.TypeIn, id, []byte("hello"))
	select {
	case f := <-sess.Inbound():
		t.Fatalf("Inbound received %v; expected viewer drop", f)
	case <-time.After(150 * time.Millisecond):
	}

	writeClientFrame(t, ctx, driver, proto.TypeIn, id, []byte("ok"))
	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "ok" {
			t.Fatalf("got %+v; want IN with payload ok", f)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for driver IN to reach Inbound")
	}
}

// dialClientAttach dials /client with a Bearer session token, then sends an
// ATTACH for sid+clientID. Returns the connected websocket.
func dialClientAttach(t *testing.T, ctx context.Context, srv *httptest.Server, token string, sid uuid.UUID, clientID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/client"
	opts := &websocket.DialOptions{}
	if token != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + token},
		}
	}
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	payload, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: clientID})
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	return c
}

// drainAttachIntro consumes the frames Subscribe queues for a new attacher:
// REPLAY_PROGRESS start + end, then one snapshot META.
func drainAttachIntro(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	for i := 0; i < 2; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read progress %d: %v", i, err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal progress %d: %v", i, err)
		}
		if f.Type != proto.TypeReplayProgress {
			t.Fatalf("frame %d type 0x%02x; want REPLAY_PROGRESS", i, f.Type)
		}
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read snapshot META: %v", err)
	}
	f, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal snapshot META: %v", err)
	}
	if f.Type != proto.TypeMeta {
		t.Fatalf("snapshot frame type 0x%02x; want META", f.Type)
	}
}

func writeClientFrame(t *testing.T, ctx context.Context, c *websocket.Conn, typ proto.Type, sid uuid.UUID, payload []byte) {
	t.Helper()
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: typ, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("write 0x%02x: %v", typ, err)
	}
}

// newClientTestStore creates an in-memory SQLiteStore, two users (A and B),
// and a session token for each. Returns store, userAID, tokenA, userBID, tokenB.
func newClientTestStore(t *testing.T) (store *userstore.SQLiteStore, userAID, tokenA, userBID, tokenB string) {
	t.Helper()
	ctx := context.Background()
	var err error
	store, err = userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	userA, err := store.CreateOpaqueUser(ctx, "usera@example.com")
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userAID = userA.ID
	tokenA, _, err = store.CreateSession(ctx, userAID, "test-agent", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession A: %v", err)
	}

	userB, err := store.CreateOpaqueUser(ctx, "userb@example.com")
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	userBID = userB.ID
	tokenB, _, err = store.CreateSession(ctx, userBID, "test-agent", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession B: %v", err)
	}
	return
}

// newClientTestServer builds a Server wired with a real IdentityResolver +
// Store so /client and /api/sessions can validate Bearer session tokens.
func newClientTestServer(t *testing.T, store userstore.Store) *Server {
	t.Helper()
	resolver := NewIdentityResolver(store)
	return NewServer(Config{
		Resolver: resolver,
		Store:    store,
	})
}

// dialClientWSBearer dials /client WS with a Bearer token Authorization header.
func dialClientWSBearer(t *testing.T, ctx context.Context, srv *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/client"
	opts := &websocket.DialOptions{}
	if token != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + token},
		}
	}
	return websocket.Dial(ctx, wsURL, opts)
}

// getSessionsWithBearer issues GET /api/sessions with the given Bearer token.
func getSessionsWithBearer(t *testing.T, srv *httptest.Server, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/sessions", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/sessions: %v", err)
	}
	return resp
}

// TestClient_ListFilteredByOwner: u_A has sessions; u_B has none.
// With u_B's token, GET /api/sessions returns [].
// With u_A's token, returns u_A's sessions.
func TestClient_ListFilteredByOwner(t *testing.T) {
	store, userAID, tokenA, _, tokenB := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// Register a session owned by user A.
	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	sess.OwnerUserID = userAID
	srv.registry.Add(sess)

	// u_B sees no sessions.
	respB := getSessionsWithBearer(t, httpSrv, tokenB)
	defer respB.Body.Close()
	if respB.StatusCode != http.StatusOK {
		t.Fatalf("u_B GET /api/sessions: status = %d; want 200", respB.StatusCode)
	}
	var sessionsB []proto.SessionInfo
	if err := json.NewDecoder(respB.Body).Decode(&sessionsB); err != nil {
		t.Fatalf("decode u_B sessions: %v", err)
	}
	if len(sessionsB) != 0 {
		t.Errorf("u_B sees %d sessions; want 0", len(sessionsB))
	}

	// u_A sees their session.
	respA := getSessionsWithBearer(t, httpSrv, tokenA)
	defer respA.Body.Close()
	if respA.StatusCode != http.StatusOK {
		t.Fatalf("u_A GET /api/sessions: status = %d; want 200", respA.StatusCode)
	}
	var sessionsA []proto.SessionInfo
	if err := json.NewDecoder(respA.Body).Decode(&sessionsA); err != nil {
		t.Fatalf("decode u_A sessions: %v", err)
	}
	if len(sessionsA) != 1 {
		t.Errorf("u_A sees %d sessions; want 1", len(sessionsA))
	}
	if len(sessionsA) > 0 && sessionsA[0].ID != sid.String() {
		t.Errorf("u_A sees session %q; want %q", sessionsA[0].ID, sid.String())
	}
}

// TestClient_AttachOtherUsersSessionRejected: u_A owns sid=X; u_B connects
// WS /client and tries to ATTACH to X → close with code 4003 and reason "forbidden".
func TestClient_AttachOtherUsersSessionRejected(t *testing.T) {
	store, userAID, _, _, tokenB := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// Register a session owned by user A.
	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	sess.OwnerUserID = userAID
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// u_B connects.
	conn, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenB)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send ATTACH for u_A's session.
	payload, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: "client-b"})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("write ATTACH: %v", err)
	}

	// Expect a close with code 4003 and reason "forbidden".
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Fatal("expected connection to be closed; got nil error")
	}
	if !strings.Contains(readErr.Error(), "4003") && !strings.Contains(readErr.Error(), CloseReasonForbidden) &&
		!strings.Contains(readErr.Error(), "forbidden") {
		t.Errorf("expected close code 4003/forbidden; got: %v", readErr)
	}
}

// TestClient_ListFrameFilteredByOwner: u_A owns session X; u_B connects /client WS,
// sends a LIST frame, and must receive a LIST_RESP that does NOT include X.
// u_A sends LIST and must receive X.
func TestClient_ListFrameFilteredByOwner(t *testing.T) {
	store, userAID, tokenA, _, tokenB := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// Register a session owned by user A.
	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	sess.OwnerUserID = userAID
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sendListAndRead := func(token string) []proto.SessionInfo {
		t.Helper()
		conn, _, err := dialClientWSBearer(t, ctx, httpSrv, token)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Send a LIST frame (empty payload, zero session id).
		listFrame := proto.Frame{Type: proto.TypeList}
		if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(listFrame)); err != nil {
			t.Fatalf("write LIST: %v", err)
		}

		// Read the LIST_RESP.
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read LIST_RESP: %v", err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal LIST_RESP: %v", err)
		}
		if f.Type != proto.TypeListResp {
			t.Fatalf("got frame type 0x%02x; want LIST_RESP (0x%02x)", f.Type, proto.TypeListResp)
		}
		var infos []proto.SessionInfo
		if err := json.Unmarshal(f.Payload, &infos); err != nil {
			t.Fatalf("unmarshal infos: %v", err)
		}
		return infos
	}

	// u_B's LIST must return 0 sessions (does not own X).
	sessionsB := sendListAndRead(tokenB)
	if len(sessionsB) != 0 {
		t.Errorf("u_B LIST: got %d sessions; want 0 (owner filter broken)", len(sessionsB))
	}

	// u_A's LIST must return the 1 session they own.
	sessionsA := sendListAndRead(tokenA)
	if len(sessionsA) != 1 {
		t.Errorf("u_A LIST: got %d sessions; want 1", len(sessionsA))
	}
	if len(sessionsA) > 0 && sessionsA[0].ID != sid.String() {
		t.Errorf("u_A LIST: session id %q; want %q", sessionsA[0].ID, sid.String())
	}
}

// TestAttachMarksSeen: when user A attaches to their own session, the relay
// must call SetSeen so the session appears as read in the store.
func TestAttachMarksSeen(t *testing.T) {
	store, userAID, tokenA, _, _ := newClientTestStore(t)

	// Build a server that has both Resolver and Store wired in (so the
	// auto-mark-seen path is active).
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
	})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// Create a session owned by user A with AttentionAt > 0 so it would
	// normally appear as unread.
	sid := uuid.New()
	sess := session.New(sid, proto.SessionInfo{
		Type:        session.SessionTypeAI,
		TaskState:   proto.TaskStateCompleted,
		AttentionAt: 1000,
	})
	sess.OwnerUserID = userAID
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect as user A and send ATTACH.
	conn, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	payload, _ := json.Marshal(proto.AttachPayload{SessionID: sid.String(), ClientID: "client-a"})
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sid, Payload: payload,
	})); err != nil {
		t.Fatalf("write ATTACH: %v", err)
	}

	// Consume the attach intro frames so the server has time to process.
	drainAttachIntro(t, ctx, conn)

	// Allow a brief moment for the SetSeen call (it is async-ish but
	// happens synchronously in the attach handler before startWriter).
	time.Sleep(50 * time.Millisecond)

	// Assert that the session now has a seen_at > 0 for user A.
	seen, err := store.SeenAt(ctx, userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if seen[sid.String()] <= 0 {
		t.Errorf("SeenAt[%s] = %d; want > 0 (session should be marked seen after attach)", sid, seen[sid.String()])
	}
}

// TestClient_RejectsInvalidBearer: invalid bearer token at /client → 401.
func TestClient_RejectsInvalidBearer(t *testing.T) {
	store, _, _, _, _ := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Dial with an invalid bearer token.
	_, resp, err := dialClientWSBearer(t, ctx, httpSrv, "ses_invalid_does_not_exist")
	if err == nil {
		t.Fatal("expected dial to fail; got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response; got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", resp.StatusCode)
	}
}

// TestClient_EchoesPingPayloadAsPong verifies the /client reader echoes the
// payload of a TypePing back as TypePong. Used by web/PWA/mobile to compute
// application-level RTT.
func TestClient_EchoesPingPayloadAsPong(t *testing.T) {
	srv, tok, userID := serverWithSessionAndUser(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	sess := session.New(id, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24})
	sess.OwnerUserID = userID
	srv.registry.Add(sess)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialClientAttach(t, ctx, httpSrv, tok, id, "client-ping")
	defer c.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, c)

	wantTS := uint64(0xFEED_FACE_CAFE_BEEF)
	writeClientFrame(t, ctx, c, proto.TypePing, id, proto.EncodePingTimestamp(wantTS))

	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for PONG echo")
		}
		readCtx, readCancel := context.WithTimeout(ctx, time.Until(deadline))
		_, data, err := c.Read(readCtx)
		readCancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type != proto.TypePong {
			continue
		}
		got, ok := proto.DecodePingTimestamp(f.Payload)
		if !ok || got != wantTS {
			t.Fatalf("PONG payload mismatch: ok=%v got=0x%x want=0x%x", ok, got, wantTS)
		}
		return
	}
}
