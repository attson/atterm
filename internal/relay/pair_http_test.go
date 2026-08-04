package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// pairTestUser bundles the seeded user's ID with a fresh session token, so
// pair tests can both authenticate as the user and assert on ownership.
type pairTestUser struct {
	ID           string
	SessionToken string
}

// newPairTestServer builds a Server with RealmID and InstancePublicURL set
// (so /api/pair/consume can echo them) and seeds one OPAQUE user with a
// valid session token. Model: helpers_test.go's serverWithSessionAndUser.
func newPairTestServer(t *testing.T) (*Server, *userstore.DBStore, pairTestUser) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "pair-test@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "go-test", "127.0.0.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	srv := NewServer(Config{
		Store:             store,
		Resolver:          NewIdentityResolver(store),
		RealmID:           "test-realm",
		InstancePublicURL: "https://node.example",
	})
	return srv, store, pairTestUser{ID: u.ID, SessionToken: tok}
}

// mustJSON marshals v to a []byte, failing the test on error.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func TestPairConsume_ReturnsSessionToken(t *testing.T) {
	// Create a server with an authenticated user.
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "a@b")
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

	// User creates a pair code via the auth handler. requireSession reads
	// the bearer token from the Authorization header (Task 1.10).
	req := httptest.NewRequest("POST", "/api/pair/create", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
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
	u, err := store.CreateOpaqueUser(ctx, "test@example.com")
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
	req.Header.Set("Authorization", "Bearer "+tok)
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

func TestPairCreate_AcceptsWrap(t *testing.T) {
	srv, store, u := newPairTestServer(t)
	wrap := bytes.Repeat([]byte{0xAB}, 73)

	body := bytes.NewReader(mustJSON(t, map[string]string{"wrap": base64.StdEncoding.EncodeToString(wrap)}))
	req := httptest.NewRequest("POST", "/api/pair/create", body)
	req.Header.Set("Authorization", "Bearer "+u.SessionToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body)
	}
	var create struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &create); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}

	// Verify the wrap made it into the DB via ConsumePairingToken.
	_, gotWrap, err := store.ConsumePairingToken(context.Background(), create.Token)
	if err != nil {
		t.Fatalf("ConsumePairingToken: %v", err)
	}
	if !bytes.Equal(gotWrap, wrap) {
		t.Fatalf("wrap mismatch: got %x want %x", gotWrap, wrap)
	}
}

func TestPairConsume_ReturnsRealmHomeWrap(t *testing.T) {
	srv, store, u := newPairTestServer(t)
	// Seed a pair token with a known wrap. The server was constructed with
	// RealmID=test-realm, InstancePublicURL="https://node.example".
	sec, _, err := store.CreatePairingToken(context.Background(), u.ID, 5*time.Minute, []byte("WRAPBYTES"))
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(mustJSON(t, map[string]string{"token": sec.Expose()})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal consume: %v", err)
	}
	if got["realm_id"] != "test-realm" {
		t.Fatalf("realm_id = %v", got["realm_id"])
	}
	if got["home_instance_url"] == nil {
		t.Fatalf("home_instance_url missing")
	}
	if got["wrap"] != base64.StdEncoding.EncodeToString([]byte("WRAPBYTES")) {
		t.Fatalf("wrap = %v", got["wrap"])
	}
}

func TestPairConsume_NoWrap_OmitsWrapField(t *testing.T) {
	srv, store, u := newPairTestServer(t)
	sec, _, err := store.CreatePairingToken(context.Background(), u.ID, 5*time.Minute, nil)
	if err != nil {
		t.Fatalf("CreatePairingToken: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/pair/consume", bytes.NewReader(mustJSON(t, map[string]string{"token": sec.Expose()})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal consume: %v", err)
	}
	if _, exists := got["wrap"]; exists {
		t.Fatalf("wrap key should be omitted")
	}
}
