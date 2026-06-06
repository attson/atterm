package relay

import (
	"context"
	"net/http"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

// newTestSeenServer builds a full relay *Server backed by an in-memory SQLiteStore
// with Resolver and Store wired, so auth routes and the /api/sessions/seen route
// are registered. Returns the server and the store.
func newTestSeenServer(t *testing.T) (*Server, *userstore.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
	})
	return srv, store
}

// addOwnedSession creates a session with the given AttentionAt and registers it
// in srv.registry under the specified ownerUserID. Returns the session UUID.
func addOwnedSession(t *testing.T, srv *Server, ownerUserID string, attentionAt int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	sess := session.New(id, proto.SessionInfo{
		TaskState:   proto.TaskStateCompleted,
		AttentionAt: attentionAt,
	})
	sess.OwnerUserID = ownerUserID
	if _, err := srv.registry.Add(sess); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	return id
}

// TestSessionsSeen_MarkAll: POST /api/sessions/seen {"all":true} marks every
// session owned by the caller as seen.
func TestSessionsSeen_MarkAll(t *testing.T) {
	srv, store := newTestSeenServer(t)
	handler := http.Handler(srv)

	// Sign up and log in as user A.
	cookieA, userAID, _ := signupAndLogin(t, handler, store, "seenall@example.com", "correcthorsebattery")
	csrfA := csrfTokenFor(t, handler, cookieA)

	// Create a session owned by A with AttentionAt > 0.
	sessID := addOwnedSession(t, srv, userAID, 1000)

	// POST /api/sessions/seen {"all":true} with A's cookie + CSRF.
	w := postJSONWithCSRF(handler, "/api/sessions/seen", map[string]interface{}{"all": true}, cookieA, csrfA)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the seen row was written.
	ctx := context.Background()
	seen, err := store.SeenAt(ctx, userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	val, ok := seen[sessID.String()]
	if !ok {
		t.Fatalf("expected session %s to be marked seen for user %s, got map=%v", sessID, userAID, seen)
	}
	if val <= 0 {
		t.Errorf("seen timestamp should be >0, got %d", val)
	}
}

// TestSessionsSeen_CrossUserIgnored: marking another user's session by ID is
// silently ignored — the endpoint returns 204 but writes no row for the caller.
func TestSessionsSeen_CrossUserIgnored(t *testing.T) {
	srv, store := newTestSeenServer(t)
	handler := http.Handler(srv)

	// Sign up user A and user B.
	cookieA, userAID, _ := signupAndLogin(t, handler, store, "seenA@example.com", "correcthorsebattery")
	_, userBID, _ := signupAndLogin(t, handler, store, "seenB@example.com", "correcthorsebattery")
	csrfA := csrfTokenFor(t, handler, cookieA)

	// Create a session owned by B.
	sessBID := addOwnedSession(t, srv, userBID, 2000)

	// User A tries to mark B's session as seen.
	w := postJSONWithCSRF(handler, "/api/sessions/seen",
		map[string]interface{}{"session_ids": []string{sessBID.String()}},
		cookieA, csrfA)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// No seen row should be written for A.
	ctx := context.Background()
	seen, err := store.SeenAt(ctx, userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if _, ok := seen[sessBID.String()]; ok {
		t.Errorf("cross-user: session %s must NOT be marked seen under user A; got seen=%v", sessBID, seen)
	}
}

// TestSessionsSeen_NoCookie: POST without a session cookie → 401.
func TestSessionsSeen_NoCookie(t *testing.T) {
	srv, _ := newTestSeenServer(t)
	handler := http.Handler(srv)

	w := postJSONWithCSRF(handler, "/api/sessions/seen", map[string]interface{}{"all": true}, nil, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSessionsSeen_NoCSRF: POST with cookie but without X-CSRF-Token → 403.
func TestSessionsSeen_NoCSRF(t *testing.T) {
	srv, store := newTestSeenServer(t)
	handler := http.Handler(srv)

	cookieA, _, _ := signupAndLogin(t, handler, store, "seencsrf@example.com", "correcthorsebattery")

	// Pass empty CSRF token (postJSONWithCSRF omits the header when csrfToken == "").
	w := postJSONWithCSRF(handler, "/api/sessions/seen", map[string]interface{}{"all": true}, cookieA, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("no CSRF: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
