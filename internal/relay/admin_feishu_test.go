package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func adminPutBearer(srv http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// newFeishuAdminServer builds a Server with a file-backed admin config store so
// the Feishu PUT path can persist + hot-apply.
func newFeishuAdminServer(t *testing.T) (*Server, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserAdmin: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua", "203.0.113.0/24", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	adminStore := NewAdminConfigStore(filepath.Join(t.TempDir(), "relay.json"), AdminConfig{})
	srv := NewServer(Config{
		Resolver:         NewIdentityResolver(store),
		Store:            store,
		AdminConfigStore: adminStore,
	})
	return srv, tok
}

func key32(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func TestAdminFeishu_EnableDisableLifecycle(t *testing.T) {
	srv, tok := newFeishuAdminServer(t)

	// Disabled at boot: binding route gated (503), events route 404.
	if w := adminGetBearer(srv, "/v1/feishu/bindings/me", tok); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled binding route: want 503, got %d", w.Code)
	}
	if w := adminGetBearer(srv, "/v1/feishu/events/abc", tok); w.Code != http.StatusNotFound {
		t.Fatalf("disabled events route: want 404, got %d", w.Code)
	}

	// Enable with a key.
	key := key32(7)
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": true, "encrypt_key": key}, tok); w.Code != http.StatusOK {
		t.Fatalf("enable PUT: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !srv.FeishuEnabled() {
		t.Fatal("FeishuEnabled = false after enable")
	}
	// Binding route no longer gated.
	if w := adminGetBearer(srv, "/v1/feishu/bindings/me", tok); w.Code == http.StatusServiceUnavailable {
		t.Fatal("binding route still gated after enable")
	}

	// GET masks the key — plaintext must not appear; last4 must match.
	w := adminGetBearer(srv, "/admin/api/feishu", tok)
	if w.Code != http.StatusOK {
		t.Fatalf("GET feishu: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), key) {
		t.Fatalf("plaintext key leaked in GET body: %s", w.Body.String())
	}
	var resp feishuAdminResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled || !resp.KeySet || resp.KeyLast4 != key[len(key)-4:] {
		t.Fatalf("masked response wrong: %+v (want last4=%q)", resp, key[len(key)-4:])
	}

	// Rotation to a different key without force → 409; with force → 200.
	key2 := key32(9)
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": true, "encrypt_key": key2}, tok); w.Code != http.StatusConflict {
		t.Fatalf("rotation without force: want 409, got %d", w.Code)
	}
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": true, "encrypt_key": key2, "force": true}, tok); w.Code != http.StatusOK {
		t.Fatalf("forced rotation: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	// Disable → handler detached, route gated again.
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": false}, tok); w.Code != http.StatusOK {
		t.Fatalf("disable PUT: %d", w.Code)
	}
	if srv.FeishuEnabled() {
		t.Fatal("FeishuEnabled = true after disable")
	}
	if w := adminGetBearer(srv, "/v1/feishu/bindings/me", tok); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("binding route not re-gated after disable: %d", w.Code)
	}
}

func TestAdminFeishu_EnableRejectsBadKey(t *testing.T) {
	srv, tok := newFeishuAdminServer(t)
	// Enable with a too-short key → 400.
	short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": true, "encrypt_key": short}, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("short key: want 400, got %d", w.Code)
	}
	// Enable with no key at all → 400.
	if w := adminPutBearer(srv, "/admin/api/feishu", map[string]any{"enabled": true}, tok); w.Code != http.StatusBadRequest {
		t.Fatalf("missing key: want 400, got %d", w.Code)
	}
}

func TestAdminConfig_DebugHotToggle(t *testing.T) {
	srv, tok := newFeishuAdminServer(t)
	if srv.debugOn() || srv.debugPayloadOn() {
		t.Fatal("debug on before any toggle")
	}
	// Enabling payload alone must also enable debug (payload implies debug).
	if w := adminPutBearer(srv, "/admin/api/config",
		map[string]any{"rate_limit_per_minute": 0, "max_connections_per_key": 0, "debug_payload": true}, tok); w.Code != http.StatusOK {
		t.Fatalf("enable PUT: %d (%s)", w.Code, w.Body.String())
	}
	if !srv.debugOn() || !srv.debugPayloadOn() {
		t.Fatalf("payload toggle failed: debug=%v payload=%v", srv.debugOn(), srv.debugPayloadOn())
	}
	// Disable both.
	if w := adminPutBearer(srv, "/admin/api/config",
		map[string]any{"rate_limit_per_minute": 0, "max_connections_per_key": 0, "debug": false, "debug_payload": false}, tok); w.Code != http.StatusOK {
		t.Fatalf("disable PUT: %d", w.Code)
	}
	if srv.debugOn() || srv.debugPayloadOn() {
		t.Fatal("debug not cleared after disable")
	}
}

func TestAdminConfig_FeishuRoundTripAndValidate(t *testing.T) {
	// Round-trip new fields through Set/Load.
	path := filepath.Join(t.TempDir(), "relay.json")
	in := AdminConfig{
		FeishuEnabled:    true,
		FeishuEncryptKey: key32(3),
		FeishuBaseURL:    "https://open.feishu.cn",
		AllowedOrigins:   []string{"https://relay.example.com"},
		VAPIDSubject:     "mailto:ops@example.com",
	}
	st := NewAdminConfigStore(path, AdminConfig{})
	if err := st.Set(in); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := LoadAdminConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !got.FeishuEnabled || got.FeishuEncryptKey != in.FeishuEncryptKey || len(got.AllowedOrigins) != 1 || got.VAPIDSubject != in.VAPIDSubject {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// validate rejects enabled + unusable key.
	bad := AdminConfig{FeishuEnabled: true, FeishuEncryptKey: "not-base64!!"}
	if err := bad.validate(); err == nil {
		t.Fatal("validate accepted enabled config with bad key")
	}

	// Old configs without the new fields still load (omitempty back-compat).
	legacy := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"rate_limit_per_minute":60,"max_connections_per_key":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAdminConfig(legacy); err != nil {
		t.Fatalf("legacy config failed to load: %v", err)
	}
}
