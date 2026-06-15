package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// newTestAuthServer returns an AuthServer + the in-memory store backing it.
// FailureFloor is zeroed so the rate-limit / invite tests don't sleep 200ms
// on every wrong-invite attempt.
func newTestAuthServer(t *testing.T) (*AuthServer, *userstore.SQLiteStore) {
	t.Helper()
	store := userstore.NewInMemory(t)
	return &AuthServer{
		Store:  store,
		Limits: NewLimitRegistry(),
	}, store
}

// createInvite mints a fresh invitation code via the store and returns
// the plaintext token. The empty-string note keeps tests succinct.
func createInvite(t *testing.T, store *userstore.SQLiteStore) string {
	t.Helper()
	code, _, err := store.CreateInvitation(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	return code.Expose()
}

// postJSON POSTs a JSON body to handler at path. The recorder returned can be
// inspected for status code and body.
func postJSON(handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// postJSONWithBearer POSTs a JSON body with the given Authorization: Bearer
// header set. Used by tests that exercise protected routes under the
// session-token contract.
func postJSONWithBearer(handler http.Handler, path string, body any, bearer string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// getWithBearer sends a GET with the given Authorization: Bearer header set.
func getWithBearer(handler http.Handler, path, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// deleteWithBearer sends a DELETE with the given Authorization: Bearer header.
func deleteWithBearer(handler http.Handler, path, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodDelete, path, nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// serverWithSession returns a Server backed by an in-memory store + a
// pre-created user with one fresh session, returning the plaintext token.
func serverWithSession(t *testing.T) (*Server, string) {
	s, tok, _ := serverWithSessionAndUser(t)
	return s, tok
}

// serverWithSessionAndUser is like serverWithSession but also returns the
// user ID. Tests that put sessions on the registry need the userID to set
// session.OwnerUserID so that ownerUserID-filtered queries return them.
// Resolver is wired alongside Store so the full /api/me/* route set is
// registered on the returned Server.
func serverWithSessionAndUser(t *testing.T) (*Server, string, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "a@b")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return NewServer(Config{Store: store, Resolver: NewIdentityResolver(store)}), tok, u.ID
}

// serverWithAuthAndSession is an alias for serverWithSessionAndUser kept
// for tests that pre-date the helper rename. It returns the Server, the
// session token, and the user ID.
func serverWithAuthAndSession(t *testing.T) (*Server, string, string) {
	return serverWithSessionAndUser(t)
}

// createUserWithSession inserts a fresh OPAQUE user, mints a session, and
// returns (token, userID). Replaces the old signup-flow helper that
// posted to /api/auth/signup.
func createUserWithSession(t *testing.T, store *userstore.SQLiteStore, email string) (token, userID string) {
	t.Helper()
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, email)
	if err != nil {
		t.Fatalf("CreateOpaqueUser %s: %v", email, err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession %s: %v", email, err)
	}
	return tok, u.ID
}
