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
// FailureFloor is zeroed so the rate-limit / login tests don't sleep 200ms
// on every wrong-password attempt.
func newTestAuthServer(t *testing.T) (*AuthServer, *userstore.SQLiteStore) {
	t.Helper()
	store := userstore.NewInMemory(t)
	return &AuthServer{
		Store:  store,
		Argon:  NewArgon2Pool(1),
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

// doSignup performs POST /api/auth/signup against handler and returns the
// recorder. Tests can decode the session_token out of the body to follow up
// with authenticated requests.
func doSignup(t *testing.T, handler http.Handler, email, password, inviteCode string) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(handler, "/api/auth/signup", map[string]string{
		"email":       email,
		"password":    password,
		"invite_code": inviteCode,
	})
}

// serverWithAuthAndSession builds a Server with both Resolver and Store
// wired (so /api/me/* routes are mounted) and pre-creates a user + active
// session. Returns the Server, the session token, and the user ID.
func serverWithAuthAndSession(t *testing.T) (*Server, string, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "a@b", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	resolver := NewIdentityResolver(store)
	return NewServer(Config{Store: store, Resolver: resolver}), tok, u.ID
}

// signupAndLogin signs up a fresh user and returns the session token plus
// the user's ID. Used by tests that need an authenticated principal without
// caring about the underlying signup mechanics.
func signupAndLogin(t *testing.T, handler http.Handler, store *userstore.SQLiteStore, email, password string) (token, userID string) {
	t.Helper()
	code := createInvite(t, store)
	w := doSignup(t, handler, email, password, code)
	if w.Code != http.StatusOK {
		t.Fatalf("signup: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		SessionToken string `json:"session_token"`
		User         struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("signupAndLogin: unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.SessionToken == "" {
		t.Fatal("signupAndLogin: empty session_token in signup response")
	}
	return resp.SessionToken, resp.User.ID
}
