package relay

import (
	"context"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// newConfigRefreshTestServer builds a minimal *Server wired to the given store,
// with an AdminConfigStore loaded from it, and the initial config applied to
// the in-memory caches via applyConfigToCaches.
func newConfigRefreshTestServer(t *testing.T, store userstore.Store) *Server {
	t.Helper()
	adminStore := NewAdminConfigStore(store, AdminConfig{})
	cfg, err := adminStore.LoadFromDB(context.Background())
	if err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	srv := NewServer(Config{
		AdminConfigStore: adminStore,
		Store:            store,
	})
	srv.applyConfigToCaches(cfg)
	return srv
}

func TestConfigRefresherAppliesRemoteChange(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Seed initial config (origins A).
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := newConfigRefreshTestServer(t, store) // see note below
	// OriginPatterns normalizes "https://a.example" → "a.example" and appends
	// the desktop webview hosts. Check the host pattern, not the raw URL.
	if got := srv.currentAllowedOrigins(); len(got) == 0 || got[0] != "a.example" {
		t.Fatalf("initial origins = %v", got)
	}

	// Simulate another instance changing config in the shared DB.
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example", "https://b.example"},
	}); err != nil {
		t.Fatalf("remote change: %v", err)
	}

	// Run one refresh tick directly (no sleep): refreshOnce returns true if applied.
	applied, err := srv.refreshConfigOnce(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !applied {
		t.Fatalf("expected refresh to apply a version change")
	}
	// After normalization: "https://a.example" → "a.example", "https://b.example" → "b.example".
	if got := srv.currentAllowedOrigins(); len(got) < 2 || got[0] != "a.example" || got[1] != "b.example" {
		t.Fatalf("after refresh origins = %v", got)
	}
}

func TestConfigRefresherNoOpWhenVersionUnchanged(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := newConfigRefreshTestServer(t, store)

	// No remote change — second refresh should return false.
	applied, err := srv.refreshConfigOnce(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if applied {
		t.Fatal("expected no-op refresh when version unchanged")
	}
}

// TestOriginNormalizationThroughRefresh verifies that refreshConfigOnce passes
// raw DB origins through OriginPatterns so only host patterns (not full URLs)
// end up in the hot-cache. Fails before the Bug 2 fix.
func TestOriginNormalizationThroughRefresh(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Seed initial config with a full URL origin.
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://relay.example.com"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := newConfigRefreshTestServer(t, store)

	// Simulate a remote instance adding another URL origin via a new version.
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://relay.example.com", "https://extra.example.com"},
	}); err != nil {
		t.Fatalf("remote change: %v", err)
	}

	applied, err := srv.refreshConfigOnce(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !applied {
		t.Fatalf("expected refresh to apply a version change")
	}

	origins := srv.currentAllowedOrigins()
	// OriginPatterns strips the scheme, so "https://relay.example.com" → "relay.example.com".
	for _, o := range origins {
		if o == "https://relay.example.com" || o == "https://extra.example.com" {
			t.Errorf("raw URL %q found in allowed origins; want host-only patterns: %v", o, origins)
		}
	}
	found := false
	for _, o := range origins {
		if o == "relay.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("normalized host pattern %q not found in origins: %v", "relay.example.com", origins)
	}
}

func TestStartConfigRefresherRunsInBackground(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := newConfigRefreshTestServer(t, store)

	// Start a fast refresher.
	srv.StartConfigRefresher(ctx, 50*time.Millisecond)

	// Change config while refresher is running.
	if _, err := store.SetRelayConfig(ctx, userstore.RelayConfig{
		AllowedOrigins: []string{"https://a.example", "https://b.example"},
	}); err != nil {
		t.Fatalf("remote change: %v", err)
	}

	// Wait for the refresher to pick it up.
	// OriginPatterns normalizes URLs to host patterns and appends desktop webview
	// hosts, so check that "b.example" appears rather than checking a raw count.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, o := range srv.currentAllowedOrigins() {
			if o == "b.example" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("refresher never applied the change; origins = %v", srv.currentAllowedOrigins())
}
