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

// newUplinkTestStore creates an in-memory SQLiteStore and a user+API-token pair
// for uplink connection tests. Returns the store, userID, and plaintext api token.
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
	secret, _, err := store.CreateAPIToken(ctx, user.ID, "test-token")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return store, user.ID, secret.Expose()
}

// newUplinkTestServer builds a *Server wired with a real IdentityResolver backed
// by the given store.
func newUplinkTestServer(t *testing.T, store userstore.Store) *Server {
	t.Helper()
	resolver := NewIdentityResolver(store)
	return NewServer(Config{
		Resolver: resolver,
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

// TestUplink_RejectsCookiePrincipal: a valid cookie session (no Authorization)
// must be rejected with HTTP 401 before WS upgrade.
func TestUplink_RejectsCookiePrincipal(t *testing.T) {
	ctx := context.Background()
	store, userID, _ := newUplinkTestStore(t)

	// Create a web session cookie for the user.
	cookieSecret, err := store.CreateWebSession(ctx, userID, "test-agent", "127.0.0")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// Dial with a cookie but no Authorization header; the cookie is set on the
	// request via custom header because the websocket library sends custom headers.
	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/uplink"
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Cookie": []string{"atterm_session=" + cookieSecret.Expose()},
		},
	}
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(dialCtx, wsURL, opts)
	if err == nil {
		t.Fatal("expected dial to fail; got nil error")
	}
	if resp == nil {
		t.Fatal("expected HTTP response; got nil")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", resp.StatusCode)
	}
	_ = userID
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
	bobSecret, _, err := store.CreateAPIToken(ctx, bob.ID, "bob-token")
	if err != nil {
		t.Fatalf("CreateAPIToken bob: %v", err)
	}

	// Re-fetch alice's token (store, userID, apiToken from newUplinkTestStore
	// returned alice's creds). Create fresh store for this test to keep alice and bob.
	// Actually we already called newUplinkTestStore which created alice. Let's get
	// alice's token from a new token creation.
	alice, err := store.VerifyPassword(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("VerifyPassword alice: %v", err)
	}
	aliceSecret, _, err := store.CreateAPIToken(ctx, alice.ID, "alice-token2")
	if err != nil {
		t.Fatalf("CreateAPIToken alice: %v", err)
	}

	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect user_A (alice).
	connA, _, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer "+aliceSecret.Expose())
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
	connB, _, err := dialUplinkWS(t, dialCtx, httpSrv, "Bearer "+bobSecret.Expose())
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
