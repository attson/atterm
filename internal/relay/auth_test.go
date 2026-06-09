package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestRequireSession_Bearer_Hit(t *testing.T) {
	s, tok := serverWithSession(t)
	var called bool
	h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if u, ok := UserFromContext(r.Context()); !ok || u == nil {
			t.Fatal("expected user in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h(rec, req)
	if !called {
		t.Fatal("handler not called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rec.Code)
	}
}

func TestRequireSession_Subprotocol_Hit(t *testing.T) {
	s, tok := serverWithSession(t)
	h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "atterm-token."+tok)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rec.Code)
	}
}

func TestRequireSession_NoToken_401(t *testing.T) {
	s, _ := serverWithSession(t)
	h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})
	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rec.Code)
	}
}

func TestRequireSession_BadToken_401(t *testing.T) {
	s, _ := serverWithSession(t)
	h := s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler must not be called")
	})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer ses_not_a_real_token")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rec.Code)
	}
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
func serverWithSessionAndUser(t *testing.T) (*Server, string, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "a@b", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return NewServer(Config{Store: store}), tok, u.ID
}
