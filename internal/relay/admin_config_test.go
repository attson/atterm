package relay

import (
	"context"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// openMemStore returns an in-memory SQLiteStore for tests.
func openMemStore(t *testing.T) userstore.Store {
	t.Helper()
	ctx := context.Background()
	st, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestAdminConfigDBRoundTrip verifies that Set(ctx) writes to the DB and
// Snapshot() / LoadFromDB(ctx) both return the same values, including the DB
// version increment.
func TestAdminConfigDBRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := openMemStore(t)

	store := NewAdminConfigStore(st, AdminConfig{})

	cfg := AdminConfig{
		RateLimitPerMinute:   12,
		MaxConnectionsPerKey: 3,
	}
	if err := store.Set(ctx, cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}

	snap := store.Snapshot()
	if snap.RateLimitPerMinute != 12 || snap.MaxConnectionsPerKey != 3 {
		t.Fatalf("Snapshot limits = %d/%d; want 12/3", snap.RateLimitPerMinute, snap.MaxConnectionsPerKey)
	}
	if snap.version == 0 {
		t.Fatal("version should be >0 after Set")
	}

	// LoadFromDB on a fresh store should read back the same values.
	store2 := NewAdminConfigStore(st, AdminConfig{})
	loaded, err := store2.LoadFromDB(ctx)
	if err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	if loaded.RateLimitPerMinute != 12 || loaded.MaxConnectionsPerKey != 3 {
		t.Fatalf("LoadFromDB limits = %d/%d; want 12/3", loaded.RateLimitPerMinute, loaded.MaxConnectionsPerKey)
	}
	if loaded.version != snap.version {
		t.Fatalf("version mismatch: loaded=%d snap=%d", loaded.version, snap.version)
	}
}
