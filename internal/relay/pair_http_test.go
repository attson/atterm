package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestPairConsume_ReturnsSessionToken(t *testing.T) {
	// Create a server with an authenticated user.
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
	s := NewServer(Config{
		Store:    store,
		Resolver: NewIdentityResolver(store),
	})

	// User creates a pair code via the auth handler.
	// For now, use a cookie since Bearer tokens aren't wired in the Resolver yet (Task 1.9).
	req := httptest.NewRequest("POST", "/api/pair/create", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: tok})
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status: %d body=%s", rec.Code, rec.Body.String())
	}
	var create struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &create); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if create.Token == "" {
		t.Fatal("no pair token returned")
	}

	// New device consumes the pair code (public route, no auth).
	body, _ := json.Marshal(map[string]string{"token": create.Token})
	req = httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("consume status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionToken string `json:"session_token"`
		ExpiresAt    int64  `json:"expires_at"`
		RelayURL     string `json:"relay_url"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal consume: %v", err)
	}
	if resp.SessionToken == "" {
		t.Fatal("missing session_token")
	}
	if resp.ExpiresAt == 0 {
		t.Fatal("missing expires_at")
	}
	if resp.User.Email != "a@b" {
		t.Fatalf("user.email: got %q want a@b", resp.User.Email)
	}

	// The returned token must resolve to the original owner.
	_, gotUser, err := store.LookupSession(context.Background(), resp.SessionToken)
	if err != nil {
		t.Fatalf("LookupSession after consume: %v", err)
	}
	if gotUser.Email != "a@b" {
		t.Fatalf("session resolves to wrong user: got %q want a@b", gotUser.Email)
	}
}

func TestPairConsume_InvalidToken_404(t *testing.T) {
	store := userstore.NewInMemory(t)
	s := NewServer(Config{
		Store:    store,
		Resolver: NewIdentityResolver(store),
	})

	body, _ := json.Marshal(map[string]string{"token": "pair_NOTREAL"})
	req := httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPairConsume_ConsumedTwice_Conflict(t *testing.T) {
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "test@example.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s := NewServer(Config{
		Store:    store,
		Resolver: NewIdentityResolver(store),
	})

	// Create a pair token.
	req := httptest.NewRequest("POST", "/api/pair/create", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: tok})
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	var create struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &create)

	// Consume once (should succeed).
	body, _ := json.Marshal(map[string]string{"token": create.Token})
	req = httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first consume: expected 200, got %d", rec.Code)
	}

	// Consume again (should fail with 409 Conflict).
	req = httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second consume: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
