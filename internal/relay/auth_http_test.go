package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// serverWithAuth builds a Server with Resolver+Store wired so the
// AuthServer's routes (/api/auth/*, /api/me/*) are mounted. Returns the
// Server, the in-memory store, and the created user.
func serverWithAuth(t *testing.T) (*Server, *userstore.SQLiteStore, *userstore.User) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "alice@example.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	resolver := NewIdentityResolver(store)
	s := NewServer(Config{
		Store:    store,
		Resolver: resolver,
	})
	return s, store, u
}

func TestLogin_ReturnsSessionTokenAndNoSetCookie(t *testing.T) {
	s, _, _ := serverWithAuth(t)
	body := strings.NewReader(`{"email":"alice@example.com","password":"Correct-Horse-Battery-Staple-1!"}`)
	req := httptest.NewRequest("POST", "/api/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if cookies := rec.Result().Cookies(); len(cookies) > 0 {
		t.Fatalf("login must not Set-Cookie; got %d cookies", len(cookies))
	}
	var resp struct {
		SessionToken string `json:"session_token"`
		ExpiresAt    int64  `json:"expires_at"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if resp.SessionToken == "" {
		t.Fatal("missing session_token")
	}
	if resp.ExpiresAt == 0 {
		t.Fatal("missing expires_at")
	}
	if resp.User.Email != "alice@example.com" {
		t.Fatalf("user.email: got %q want alice@example.com", resp.User.Email)
	}
}

func TestLogout_RevokesBearerSession(t *testing.T) {
	s, store, u := serverWithAuth(t)
	ctx := context.Background()
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if _, _, err := store.LookupSession(ctx, tok); err == nil {
		t.Fatal("session still resolves after logout")
	}
}

func TestMeTokens_RoutesGone(t *testing.T) {
	s, store, u := serverWithAuth(t)
	ctx := context.Background()
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		req := httptest.NewRequest(method, "/api/me/tokens", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /api/me/tokens: got %d want 404", method, rec.Code)
		}
	}
}
