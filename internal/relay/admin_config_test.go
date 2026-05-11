package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminConfigPersistsWithoutMainToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	store := NewAdminConfigStore(path, AdminConfig{
		RateLimitPerMinute:   12,
		MaxConnectionsPerKey: 3,
		ReadOnlyTokens: []StoredToken{{
			ID:        "viewer",
			Hash:      HashBearerToken("secret"),
			CreatedAt: 123,
		}},
	})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "main-write-token") || strings.Contains(string(data), "secret\"") {
		t.Fatalf("config leaked secret: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v; want 0600", got)
	}
}

func TestAdminConfigLoadsHashedReadOnlyToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.json")
	store := NewAdminConfigStore(path, AdminConfig{
		ReadOnlyTokens: []StoredToken{{
			ID:        "viewer",
			Hash:      HashBearerToken("secret"),
			CreatedAt: 123,
		}},
	})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAdminConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ReadOnlyTokens) != 1 {
		t.Fatalf("tokens = %#v; want one", loaded.ReadOnlyTokens)
	}
	if !tokenMatchesHash("secret", loaded.ReadOnlyTokens[0].Hash) {
		t.Fatal("stored hash did not authenticate original token")
	}
}
