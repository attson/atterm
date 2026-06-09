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

// newTestSeenServer builds a full relay *Server backed by an in-memory
// SQLiteStore with Resolver and Store wired, so the /api/sessions/seen
// route is registered. Returns the server and the store.
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

// issueSessionToken mints a fresh session token for userID via the store.
// Returned plaintext can be used directly as a Bearer credential.
func issueSessionToken(t *testing.T, store *userstore.SQLiteStore, userID string) string {
	t.Helper()
	tok, _, err := store.CreateSession(context.Background(), userID, "test-mobile", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return tok
}

// TestSessionsSeen_MarkAll: POST /api/sessions/seen {"all":true} marks every
// session owned by the caller as seen.
func TestSessionsSeen_MarkAll(t *testing.T) {
	srv, store := newTestSeenServer(t)

	_, userAID := signupAndLogin(t, srv, store, "seenall@example.com", "correcthorsebattery")
	tokenA := issueSessionToken(t, store, userAID)
	sessID := addOwnedSession(t, srv, userAID, 1000)

	w := postJSONWithBearer(srv, "/api/sessions/seen", map[string]any{"all": true}, tokenA)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

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

	_, userAID := signupAndLogin(t, srv, store, "seenA@example.com", "correcthorsebattery")
	_, userBID := signupAndLogin(t, srv, store, "seenB@example.com", "correcthorsebattery")
	tokenA := issueSessionToken(t, store, userAID)
	sessBID := addOwnedSession(t, srv, userBID, 2000)

	w := postJSONWithBearer(srv, "/api/sessions/seen",
		map[string]any{"session_ids": []string{sessBID.String()}},
		tokenA)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	seen, err := store.SeenAt(ctx, userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if _, ok := seen[sessBID.String()]; ok {
		t.Errorf("cross-user: session %s must NOT be marked seen under user A; got seen=%v", sessBID, seen)
	}
}

// TestSessionsSeen_NoAuth: POST without an Authorization header → 401.
func TestSessionsSeen_NoAuth(t *testing.T) {
	srv, _ := newTestSeenServer(t)

	w := postJSONWithBearer(srv, "/api/sessions/seen", map[string]any{"all": true}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSessionsSeen_BodyTooLarge: a body exceeding 64 KB → 400.
func TestSessionsSeen_BodyTooLarge(t *testing.T) {
	srv, store := newTestSeenServer(t)

	_, userAID := signupAndLogin(t, srv, store, "seenlarge@example.com", "correcthorsebattery")
	token := issueSessionToken(t, store, userAID)

	// 5000 padded UUIDs in a JSON array far exceeds 64 KB.
	huge := make([]string, 5000)
	for i := range huge {
		huge[i] = "00000000-0000-0000-0000-000000000000"
	}
	w := postJSONWithBearer(srv, "/api/sessions/seen", map[string]any{"session_ids": huge}, token)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized body: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionsSeen_Bearer_InvalidToken(t *testing.T) {
	srv, _ := newTestSeenServer(t)

	w := postJSONWithBearer(srv, "/api/sessions/seen",
		map[string]any{"all": true},
		"ses_does_not_exist_in_store")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

