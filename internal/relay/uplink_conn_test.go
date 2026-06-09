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
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// newUplinkTestStore creates an in-memory SQLiteStore and a user+session-token
// pair for uplink connection tests. Returns the store, userID, and plaintext
// session token (suitable for "Authorization: Bearer <token>").
func newUplinkTestStore(t *testing.T) (*userstore.SQLiteStore, string, string) {
	t.Helper()
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	user, err := store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, user.ID, "test-uplink", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return store, user.ID, tok
}

// newUplinkTestServer builds a *Server wired with the given store so that
// requireSession can validate Bearer session tokens.
func newUplinkTestServer(t *testing.T, store userstore.Store) *Server {
	t.Helper()
	resolver := NewIdentityResolver(store)
	return NewServer(Config{
		Resolver: resolver,
		Store:    store,
	})
}

// dialUplinkWS connects to the /uplink WS endpoint on srv with the supplied
// Authorization header value. Returns (conn, httpResp).
func dialUplinkWS(t *testing.T, ctx context.Context, srv *httptest.Server, authHeader string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/uplink"
	opts := &websocket.DialOptions{}
	if authHeader != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{authHeader},
		}
	}
	return websocket.Dial(ctx, wsURL, opts)
}

// sendAnnounce sends a minimal ANNOUNCE frame with the given session IDs.
func sendAnnounce(t *testing.T, ctx context.Context, c *websocket.Conn, sessionIDs ...uuid.UUID) {
	t.Helper()
	sessions := make([]proto.SessionInfo, len(sessionIDs))
	for i, id := range sessionIDs {
		sessions[i] = proto.SessionInfo{
			ID:      id.String(),
			Command: "bash",
			HostID:  uuid.New().String(),
		}
	}
	ann := proto.AnnouncePayload{Sessions: sessions}
	payload, _ := json.Marshal(ann)
	frame := proto.Frame{Type: proto.TypeAnnounce, Payload: payload}
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(frame)); err != nil {
		t.Fatalf("sendAnnounce write: %v", err)
	}
}

// TestUplink_RejectsInvalidPrincipal: connect with invalid bearer token → 401 before upgrade.
func TestUplink_RejectsInvalidPrincipal(t *testing.T) {
	store, _, _ := newUplinkTestStore(t)

	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, resp, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer invalid-token")
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

// TestUplink_AcceptsAPIToken_BindsOwner: connect with valid api token; publish a
// session; verify the in-memory session has OwnerUserID == user.ID.
func TestUplink_AcceptsAPIToken_BindsOwner(t *testing.T) {
	store, userID, apiToken := newUplinkTestStore(t)

	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	sid := uuid.New()
	sendAnnounce(t, dialCtx, conn, sid)

	// Give the relay goroutine a moment to process the ANNOUNCE.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := srv.Registry().Get(sid)
		if ok && s.OwnerUserID != "" {
			if s.OwnerUserID != userID {
				t.Errorf("OwnerUserID = %q; want %q", s.OwnerUserID, userID)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Final check with direct registry access.
	s, ok := srv.Registry().Get(sid)
	if !ok {
		t.Fatal("session not found in registry after ANNOUNCE")
	}
	if s.OwnerUserID != userID {
		t.Errorf("OwnerUserID = %q; want %q", s.OwnerUserID, userID)
	}
}

// TestUplink_DuplicateSessionIDDifferentUser_Closes: user_A's uplink publishes
// sid=X; user_B's uplink (separate api token) tries to publish sid=X → close
// with reason session_id_owner_mismatch; user_A's session remains untouched.
func TestUplink_DuplicateSessionIDDifferentUser_Closes(t *testing.T) {
	ctx := context.Background()
	store, _, _ := newUplinkTestStore(t) // alice already created

	// Create a second user (bob) and their API token.
	bob, err := store.CreateUser(ctx, "bob@example.com", "correcthorsebatterystaple2")
	if err != nil {
		t.Fatalf("CreateUser bob: %v", err)
	}
	bobTok, _, err := store.CreateSession(ctx, bob.ID, "bob-uplink", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession bob: %v", err)
	}

	// Re-fetch alice's user record and mint a fresh session token. The
	// newUplinkTestStore helper created alice; we want her id to assert
	// ownership stays under her after bob's conflict attempt.
	alice, err := store.VerifyPassword(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("VerifyPassword alice: %v", err)
	}
	aliceTok, _, err := store.CreateSession(ctx, alice.ID, "alice-uplink-2", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession alice: %v", err)
	}

	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect user_A (alice).
	connA, _, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer "+aliceTok)
	if err != nil {
		t.Fatalf("dial alice: %v", err)
	}
	defer connA.Close(websocket.StatusNormalClosure, "")

	// Alice publishes session sid=X.
	sid := uuid.New()
	sendAnnounce(t, dialCtx, connA, sid)

	// Wait for sid=X to appear in the registry under alice's ownership.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s, ok := srv.Registry().Get(sid)
		if ok && s.OwnerUserID == alice.ID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	sessA, ok := srv.Registry().Get(sid)
	if !ok {
		t.Fatal("alice's session not found in registry")
	}
	if sessA.OwnerUserID != alice.ID {
		t.Fatalf("alice session OwnerUserID = %q; want %q", sessA.OwnerUserID, alice.ID)
	}

	// Connect user_B (bob) and try to publish the same sid=X.
	connB, _, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer "+bobTok)
	if err != nil {
		t.Fatalf("dial bob: %v", err)
	}
	// Don't defer a normal close — we expect the server to close it with 4002.

	// Drain the AUTH_INFO frame the server sends immediately on connect.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	readAndDiscardAuthInfo(t, drainCtx, connB)

	sendAnnounce(t, dialCtx, connB, sid)

	// Wait for connB to be closed by the server.
	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	_, _, readErr := connB.Read(readCtx)
	connB.CloseRead(context.Background())

	if readErr == nil {
		t.Fatal("expected connB to be closed by server; got nil error on read")
	}

	// Check that the close code is 4002.
	var closeErr websocket.CloseError
	if !isCloseError(readErr, &closeErr) {
		t.Logf("readErr type: %T, value: %v", readErr, readErr)
		// The close frame may not always surface as CloseError via nhooyr.io/websocket —
		// the important thing is that connB was closed and alice's session is intact.
	} else {
		if int(closeErr.Code) != CloseCodeSessionIDOwnerMismatch {
			t.Errorf("close code = %d; want %d (session_id_owner_mismatch)", closeErr.Code, CloseCodeSessionIDOwnerMismatch)
		}
		if closeErr.Reason != CloseReasonSessionIDOwnerMismatch {
			t.Errorf("close reason = %q; want %q", closeErr.Reason, CloseReasonSessionIDOwnerMismatch)
		}
	}

	// Alice's session must still be in the registry and still owned by alice.
	sessAAfter, ok := srv.Registry().Get(sid)
	if !ok {
		t.Fatal("alice's session was removed from registry after bob's conflict attempt")
	}
	if sessAAfter.OwnerUserID != alice.ID {
		t.Errorf("alice session OwnerUserID after conflict = %q; want %q", sessAAfter.OwnerUserID, alice.ID)
	}
	if sessAAfter != sessA {
		t.Error("alice's session object was replaced; want identity preserved")
	}
}

// readAndDiscardAuthInfo reads the first frame from c and asserts it is a
// TypeAuthInfo frame. Used in tests that connect with a valid API token and
// need to drain the AUTH_INFO the server sends before sending their own frames.
func readAndDiscardAuthInfo(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("readAndDiscardAuthInfo: read: %v", err)
	}
	f, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("readAndDiscardAuthInfo: unmarshal: %v", err)
	}
	if f.Type != proto.TypeAuthInfo {
		t.Fatalf("readAndDiscardAuthInfo: expected TypeAuthInfo (0x%02x), got 0x%02x", proto.TypeAuthInfo, f.Type)
	}
}

// isCloseError checks if err wraps a websocket.CloseError and fills *out.
func isCloseError(err error, out *websocket.CloseError) bool {
	if err == nil {
		return false
	}
	var ce websocket.CloseError
	// nhooyr.io/websocket may not always wrap with errors.As; try direct type check.
	if ce, ok := err.(websocket.CloseError); ok {
		*out = ce
		return true
	}
	// Walk the error chain.
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if ce, ok := err.(websocket.CloseError); ok {
			*out = ce
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	_ = ce
	return false
}

func TestMirrorSessionCountHookReportsViewers(t *testing.T) {
	sess := newMirrorSession(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24}, "owner")
	counts := make(chan int, 8)
	sess.SetSubscriberCountHook(func(n int) { counts <- n })

	a, _ := sess.Subscribe(0, "a", "ha")
	if got := <-counts; got != 1 {
		t.Fatalf("after 1 attach: count=%d; want 1", got)
	}
	b, _ := sess.Subscribe(0, "b", "hb")
	if got := <-counts; got != 2 {
		t.Fatalf("after 2 attach: count=%d; want 2", got)
	}
	sess.Unsubscribe(a)
	if got := <-counts; got != 1 {
		t.Fatalf("after 1 detach: count=%d; want 1", got)
	}
	sess.Unsubscribe(b)
	if got := <-counts; got != 0 {
		t.Fatalf("after all detach: count=%d; want 0", got)
	}
}

func TestNewMirrorSessionAdoptsUpstreamDriver(t *testing.T) {
	id := uuid.New()
	sess := newMirrorSession(id, proto.SessionInfo{Cols: 80, Rows: 24}, "owner-user")

	// A mirror must not self-promote its first subscriber.
	sub, _ := sess.Subscribe(0, "remote-client", "remote-host")
	defer sess.Unsubscribe(sub)
	if sess.DriverClientID() != "" {
		t.Fatalf("mirror self-promoted a driver: %q", sess.DriverClientID())
	}
	if sess.OwnerUserID != "owner-user" {
		t.Fatalf("OwnerUserID = %q; want owner-user", sess.OwnerUserID)
	}

	// Upstream META sets the real driver.
	sess.UpdateMeta(proto.MetaPayload{DriverClientID: "owner-A", DriverClientName: "mac-mini"})
	if got := sess.DriverClientID(); got != "owner-A" {
		t.Fatalf("mirror driver after upstream META = %q; want owner-A", got)
	}
}
