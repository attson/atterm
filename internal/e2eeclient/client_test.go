package e2eeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
)

// newRelay spins up an in-process atterm-relay HTTP server with the OPAQUE
// handler mounted at the same paths the SDK targets.
func newRelay(t *testing.T) (*httptest.Server, *userstore.SQLiteStore) {
	t.Helper()
	store := userstore.NewInMemory(t)
	opaqueSrv, err := relay.LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	mux := http.NewServeMux()
	relay.NewOpaqueAuthHandler(store, opaqueSrv).Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, store
}

func TestWrapUnwrap_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	wrap, err := wrapAccountKey("hunter2", key, DefaultKDFParams())
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if len(wrap.Wrapped) == 0 || len(wrap.Nonce) != 24 || len(wrap.Salt) != 16 {
		t.Fatalf("wrap envelope shape wrong: %+v", wrap)
	}
	got, err := unwrapAccountKey("hunter2", wrap)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if string(got) != string(key) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestUnwrap_WrongPassword(t *testing.T) {
	key := make([]byte, 32)
	wrap, _ := wrapAccountKey("hunter2", key, DefaultKDFParams())
	_, err := unwrapAccountKey("not-the-password", wrap)
	if err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestClient_RegisterAndLogin(t *testing.T) {
	ts, _ := newRelay(t)
	c := &Client{BaseURL: ts.URL}

	reg, err := c.Register(context.Background(), "alice@example.com", "hunter2", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.UserID == "" || reg.SessionToken == "" {
		t.Fatalf("Register returned empty fields: %+v", reg)
	}
	if len(reg.AccountKey) != 32 {
		t.Fatalf("account_key wrong size: %d", len(reg.AccountKey))
	}

	// Different login session, same account_key
	lg, err := c.Login(context.Background(), "alice@example.com", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if string(lg.AccountKey) != string(reg.AccountKey) {
		t.Fatalf("login account_key differs from register account_key")
	}
	if lg.UserID != reg.UserID {
		t.Fatalf("user_id changed across login: reg=%q login=%q", reg.UserID, lg.UserID)
	}
	if lg.SessionToken == reg.SessionToken {
		t.Fatalf("expected a fresh session token at login")
	}
}

func TestClient_Login_WrongPassword(t *testing.T) {
	ts, _ := newRelay(t)
	c := &Client{BaseURL: ts.URL}
	if _, err := c.Register(context.Background(), "bob@example.com", "correct", ""); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := c.Login(context.Background(), "bob@example.com", "wrong"); err == nil {
		t.Fatalf("expected error on wrong password")
	}
}

func TestClient_GetKeyWrap(t *testing.T) {
	ts, _ := newRelay(t)
	// /api/me/key needs requireSession; mount the AuthServer alongside the
	// OPAQUE handler so the session middleware is wired.
	store := userstore.NewInMemory(t)
	opaqueSrv, _ := relay.LoadOrInitOpaqueServer(context.Background(), store)
	mux := http.NewServeMux()
	relay.NewOpaqueAuthHandler(store, opaqueSrv).Register(mux)
	// Bring up the full server (with the session middleware) and use its
	// ServeHTTP as the test transport so /api/me/key routes through
	// requireSession.
	srv := relay.NewServer(relay.Config{
		Store:        store,
		Resolver:     relay.NewIdentityResolver(store),
		OpaqueServer: opaqueSrv,
	})
	full := httptest.NewServer(srv)
	t.Cleanup(full.Close)

	c := &Client{BaseURL: full.URL}
	reg, err := c.Register(context.Background(), "carol@example.com", "hunter2", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	wrap, err := c.GetKeyWrap(context.Background(), reg.SessionToken)
	if err != nil {
		t.Fatalf("GetKeyWrap: %v", err)
	}
	if wrap.Method != "password" || len(wrap.Wrapped) == 0 {
		t.Fatalf("wrap looks empty: %+v", wrap)
	}

	// Unwrapping with the user's password should recover the same key.
	got, err := UnwrapWithPassword("hunter2", wrap)
	if err != nil {
		t.Fatalf("UnwrapWithPassword: %v", err)
	}
	if string(got) != string(reg.AccountKey) {
		t.Fatalf("unwrap returned a different key than Register")
	}

	_ = ts // silence unused
}

func TestClient_PutKeyWrap_PasswordChange(t *testing.T) {
	store := userstore.NewInMemory(t)
	opaqueSrv, _ := relay.LoadOrInitOpaqueServer(context.Background(), store)
	srv := relay.NewServer(relay.Config{
		Store:        store,
		Resolver:     relay.NewIdentityResolver(store),
		OpaqueServer: opaqueSrv,
	})
	full := httptest.NewServer(srv)
	t.Cleanup(full.Close)

	c := &Client{BaseURL: full.URL}
	reg, err := c.Register(context.Background(), "dave@example.com", "old-pw", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Re-wrap with a new password (simulating the client side of a password
	// change before the OPAQUE re-registration uploads the new envelope).
	newWrap, err := ReWrapWithPassword("new-pw", reg.AccountKey, DefaultKDFParams())
	if err != nil {
		t.Fatalf("ReWrapWithPassword: %v", err)
	}
	if err := c.PutKeyWrap(context.Background(), reg.SessionToken, newWrap); err != nil {
		t.Fatalf("PutKeyWrap: %v", err)
	}

	got, err := c.GetKeyWrap(context.Background(), reg.SessionToken)
	if err != nil {
		t.Fatalf("GetKeyWrap: %v", err)
	}
	// Unwrap with the new password must recover the original account_key.
	recovered, err := UnwrapWithPassword("new-pw", got)
	if err != nil {
		t.Fatalf("Unwrap with new password: %v", err)
	}
	if string(recovered) != string(reg.AccountKey) {
		t.Fatalf("recovered key differs from original")
	}
	// Old password no longer works (Argon2id derives a different wrap_key).
	if _, err := UnwrapWithPassword("old-pw", got); err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword with old password, got %v", err)
	}
}

func TestClient_Register_ClaimToken_PromotesAdmin(t *testing.T) {
	store := userstore.NewInMemory(t)
	opaqueSrv, _ := relay.LoadOrInitOpaqueServer(context.Background(), store)
	mux := http.NewServeMux()
	relay.NewOpaqueAuthHandler(store, opaqueSrv).Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// Mint a claim token directly via the store.
	plaintext, err := store.CreateClaimToken(context.Background(), "admin@example.com", "admin", 0+24*60*60*1e9)
	if err != nil {
		t.Fatalf("CreateClaimToken: %v", err)
	}

	c := &Client{BaseURL: ts.URL}
	reg, err := c.Register(context.Background(), "admin@example.com", "hunter2", plaintext)
	if err != nil {
		t.Fatalf("Register with claim token: %v", err)
	}

	u, err := store.GetUser(context.Background(), reg.UserID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.IsAdmin {
		t.Fatalf("user should be admin after claim-token registration")
	}
}
