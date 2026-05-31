package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestIsMobileOriginCompatible(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
		want    bool
	}{
		{"empty (wildcard)", nil, true},
		{"capacitor", []string{"capacitor://localhost"}, true},
		{"capacitor wildcard", []string{"capacitor://*"}, true},
		{"ionic", []string{"ionic://localhost"}, true},
		{"https-localhost", []string{"https://localhost"}, true},
		{"https-localhost-port", []string{"https://localhost:1234"}, true},
		{"null", []string{"null"}, true},
		{"wails only", []string{"wails.localhost"}, false},
		{"mixed has capacitor", []string{"wails.localhost", "capacitor://localhost"}, true},
		{"mixed no capacitor", []string{"wails.localhost", "https://example.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isMobileOriginCompatible(tc.origins)
			if got != tc.want {
				t.Fatalf("got %v want %v for origins=%v", got, tc.want, tc.origins)
			}
		})
	}
}

// newTestStoreForRelay opens an in-memory userstore for relay tests.
// (helper colocated here because health tests are the first relay tests
// that need an in-process store outside the AuthServer test harness.)
func newTestStoreForRelay(t *testing.T) *userstore.SQLiteStore {
	t.Helper()
	store, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCollectHealth_PopulatesAllFields(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:              "v0.3.99",
		AllowedOrigins:       []string{"wails.localhost", "capacitor://localhost"},
		RateLimitPerMinute:   600,
		MaxConnectionsPerKey: 64,
		Resolver:             resolver,
		Store:                store,
	})

	// Drive a fake request to give collectHealth an *http.Request for HTTPS detection.
	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	r.Header.Set("X-Forwarded-Proto", "https")

	payload := collectHealth(context.Background(), srv, r)

	if payload.Version != "v0.3.99" {
		t.Errorf("Version: got %q", payload.Version)
	}
	if payload.UptimeSeconds < 0 {
		t.Errorf("UptimeSeconds negative: %d", payload.UptimeSeconds)
	}
	if !payload.HTTPS {
		t.Errorf("HTTPS: got false, want true (X-Forwarded-Proto)")
	}
	if len(payload.ConfiguredOrigins) != 2 {
		t.Errorf("ConfiguredOrigins: got %v", payload.ConfiguredOrigins)
	}
	if payload.OriginsOpen {
		t.Errorf("OriginsOpen: got true, want false")
	}
	if payload.RateLimitPerMinute != 600 {
		t.Errorf("RateLimitPerMinute: got %d", payload.RateLimitPerMinute)
	}
	if payload.MaxConnectionsPerKey != 64 {
		t.Errorf("MaxConnectionsPerKey: got %d", payload.MaxConnectionsPerKey)
	}
	if payload.ActiveUplinks != 0 {
		t.Errorf("ActiveUplinks: got %d", payload.ActiveUplinks)
	}
	if !payload.MobileOriginCompatible {
		t.Errorf("MobileOriginCompatible: got false")
	}
	if !strings.HasSuffix(payload.GeneratedAt, "Z") {
		t.Errorf("GeneratedAt: got %q, want RFC3339 UTC ending in Z", payload.GeneratedAt)
	}
	// BootstrapAdminConfigured depends on Store contents; on an empty store
	// it must be false.
	if payload.BootstrapAdminConfigured {
		t.Errorf("BootstrapAdminConfigured: got true on empty store")
	}
}
