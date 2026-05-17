package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
)

// newTestServerWithStore builds a relay Server with both Resolver and Store wired
// (the same combination the production binary uses). This exercises the code path
// that mounts /api/auth/*, /api/me/*, and /admin/api/* routes.
func newTestServerWithStore(t *testing.T) (*relay.Server, userstore.Store) {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	resolver := relay.NewIdentityResolver(store)
	cfg := relay.Config{
		Resolver: resolver,
		Store:    store,
	}
	return relay.NewServer(cfg), store
}

// TestRelay_SignupLoginToken_E2E boots a server via NewServer(cfg) with the SAME
// wiring as production (Resolver + Store set), then hits POST /api/auth/signup
// without an invite and asserts the response is 400 — NOT 404 (the latter would
// prove the route is unmounted). This test guards against the regression where
// user-account routes were not registered in the production binary.
func TestRelay_SignupLoginToken_E2E(t *testing.T) {
	srv, _ := newTestServerWithStore(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	// POST /api/auth/signup with no invite code. Valid JSON so the handler
	// reaches the invite-consumption step and returns 400 "invite_invalid"
	// (or 400 "password_weak" for a short password — both are 400, not 404).
	body, _ := json.Marshal(map[string]string{
		"email":       "e2e@example.com",
		"password":    "correcthorsebattery",
		"invite_code": "inv_doesnotexist",
	})
	resp, err := http.Post(httpSrv.URL+"/api/auth/signup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/signup: %v", err)
	}
	defer resp.Body.Close()

	// 404 would mean the route is not mounted — the regression this test guards.
	// Any non-404 confirms the handler is reachable; 400 (invite_invalid) is the
	// expected production response for an unknown invite code.
	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("POST /api/auth/signup returned 404 — user-account routes are NOT mounted in NewServer; "+
			"expected 400 (invite_invalid). This is the Fix 1 regression.", )
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /api/auth/signup: got %d, want 400 (invite_invalid)", resp.StatusCode)
	}
}

// TestRelay_MeRoute_E2E verifies /api/me returns 401 (not 404) when unauthenticated,
// confirming the route is mounted.
func TestRelay_MeRoute_E2E(t *testing.T) {
	srv, _ := newTestServerWithStore(t)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/api/me")
	if err != nil {
		t.Fatalf("GET /api/me: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("GET /api/me returned 404 — user-account routes are NOT mounted in NewServer; expected 401")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/me: got %d, want 401", resp.StatusCode)
	}
}
