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
// version increment and ReadOnlyTokens (in-memory only, not persisted to DB).
func TestAdminConfigDBRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := openMemStore(t)

	initial := AdminConfig{
		ReadOnlyTokens: []StoredToken{{
			ID:        "viewer",
			Hash:      HashBearerToken("secret"),
			CreatedAt: 123,
		}},
	}
	store := NewAdminConfigStore(st, initial)

	cfg := AdminConfig{
		RateLimitPerMinute:   12,
		MaxConnectionsPerKey: 3,
		ReadOnlyTokens: []StoredToken{{
			ID:        "viewer",
			Hash:      HashBearerToken("secret"),
			CreatedAt: 123,
		}},
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
	// ReadOnlyTokens must survive through Snapshot (in-memory only).
	if len(snap.ReadOnlyTokens) != 1 || !tokenMatchesHash("secret", snap.ReadOnlyTokens[0].Hash) {
		t.Fatalf("ReadOnlyTokens round-trip failed: %+v", snap.ReadOnlyTokens)
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

// TestAdminConfigLoadsHashedReadOnlyToken verifies that ReadOnlyTokens survive
// in-memory across Set/Snapshot cycles even though they are not stored in DB.
func TestAdminConfigLoadsHashedReadOnlyToken(t *testing.T) {
	ctx := context.Background()
	st := openMemStore(t)

	tok := StoredToken{
		ID:        "viewer",
		Hash:      HashBearerToken("secret"),
		CreatedAt: 123,
	}
	store := NewAdminConfigStore(st, AdminConfig{
		ReadOnlyTokens: []StoredToken{tok},
	})
	if err := store.Set(ctx, AdminConfig{
		ReadOnlyTokens: []StoredToken{tok},
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	snap := store.Snapshot()
	if len(snap.ReadOnlyTokens) != 1 {
		t.Fatalf("tokens = %#v; want one", snap.ReadOnlyTokens)
	}
	if !tokenMatchesHash("secret", snap.ReadOnlyTokens[0].Hash) {
		t.Fatal("stored hash did not authenticate original token")
	}
}

// TestAdminConfigPersistsWithoutMainToken verifies that a secret plaintext
// token is never exposed via Snapshot (which is the replacement for the old
// file read check). The hash is stored; the plaintext is not.
func TestAdminConfigPersistsWithoutMainToken(t *testing.T) {
	ctx := context.Background()
	st := openMemStore(t)

	store := NewAdminConfigStore(st, AdminConfig{})
	err := store.Set(ctx, AdminConfig{
		RateLimitPerMinute:   12,
		MaxConnectionsPerKey: 3,
		ReadOnlyTokens: []StoredToken{{
			ID:        "viewer",
			Hash:      HashBearerToken("secret"),
			CreatedAt: 123,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	snap := store.Snapshot()
	// The plaintext "secret" must not appear in the hash.
	for _, tok := range snap.ReadOnlyTokens {
		if tok.Hash == "secret" {
			t.Fatalf("config exposed plaintext secret in ReadOnlyTokens")
		}
	}
	// The hash must be present and the token must match it.
	if len(snap.ReadOnlyTokens) != 1 || !tokenMatchesHash("secret", snap.ReadOnlyTokens[0].Hash) {
		t.Fatalf("hash round-trip failed: %+v", snap.ReadOnlyTokens)
	}
}
