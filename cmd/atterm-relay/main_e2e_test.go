package main

import (
	"context"
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
