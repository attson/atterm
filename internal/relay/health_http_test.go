package relay

import (
	"context"
	"encoding/json"
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

func TestHealthz_Public_ReturnsOKAndVersion(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:  "v0.3.99",
		Resolver: resolver,
		Store:    store,
	})

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control: got %q want %q", got, "no-store")
	}

	var body struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.OK || body.Version != "v0.3.99" {
		t.Fatalf("body: %+v", body)
	}
}

func TestHealthz_Public_NoAuthRequired(t *testing.T) {
	// Even when Resolver is configured, /healthz must not require auth.
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	// no Authorization header, no cookies
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// findUserByEmail iterates ListUsers to find a user by email; used in
// place of a dedicated GetUserByEmail (which the userstore does not expose).
func findUserByEmail(t *testing.T, store *userstore.SQLiteStore, email string) *userstore.User {
	t.Helper()
	users, err := store.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	for i := range users {
		if users[i].Email == email {
			return &users[i]
		}
	}
	t.Fatalf("user %q not found", email)
	return nil
}

func TestAdminHealthAPI_Unauthenticated_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminHealthAPI_RegularUser_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (not admin), got %d", w.Code)
	}
}

func TestAdminHealthAPI_Admin_200_AllFieldsPresent(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	if _, err := store.EnsureAdminUser(ctx, "admin@example.com", "correcthorsebatterystaple"); err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	u := findUserByEmail(t, store, "admin@example.com")
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test-admin")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:        "v0.3.99",
		AllowedOrigins: []string{"capacitor://localhost"},
		Resolver:       resolver,
		Store:          store,
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got HealthPayload
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Version != "v0.3.99" {
		t.Errorf("Version: got %q", got.Version)
	}
	if !got.BootstrapAdminConfigured {
		t.Errorf("BootstrapAdminConfigured: got false")
	}
	if !got.MobileOriginCompatible {
		t.Errorf("MobileOriginCompatible: got false")
	}
	if got.GeneratedAt == "" {
		t.Errorf("GeneratedAt empty")
	}
}

func TestAdminHealth_HTML_Admin_200_AllLabelsPresent(t *testing.T) {
	store := newTestStoreForRelay(t)
	ctx := context.Background()
	if _, err := store.EnsureAdminUser(ctx, "admin@example.com", "correcthorsebatterystaple"); err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	u := findUserByEmail(t, store, "admin@example.com")
	secret, _, err := store.CreateAPIToken(ctx, u.ID, "test-admin")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Version:              "v0.3.99",
		AllowedOrigins:       []string{"capacitor://localhost"},
		RateLimitPerMinute:   600,
		MaxConnectionsPerKey: 64,
		Resolver:             resolver,
		Store:                store,
	})

	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type: got %q", got)
	}
	body := w.Body.String()
	for _, label := range []string{
		"Version",
		"Uptime",
		"HTTPS",
		"Configured origins",
		"Bootstrap admin",
		"Rate limit",
		"Max conn",
		"Active uplinks",
		"Mobile compat",
		"Copy diagnostics",
	} {
		if !strings.Contains(body, label) {
			t.Errorf("HTML missing label %q", label)
		}
	}
	if !strings.Contains(body, "v0.3.99") {
		t.Errorf("HTML missing version value")
	}
	// __HEALTH__ JSON should be injected so the copy button has data.
	if !strings.Contains(body, "window.__HEALTH__") {
		t.Errorf("HTML missing window.__HEALTH__ injection")
	}
}

func TestAdminHealth_HTML_Unauthenticated_401(t *testing.T) {
	store := newTestStoreForRelay(t)
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{Resolver: resolver, Store: store})

	r := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
