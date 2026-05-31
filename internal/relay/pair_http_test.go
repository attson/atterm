package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestPairServer reuses newTestAuthServer (from auth_http_test.go); the
// pairing routes are registered by the same AuthServer.RegisterInto call.
func newTestPairServer(t *testing.T) (http.Handler, *AuthServer) {
	t.Helper()
	srv, _ := newTestAuthServer(t)
	srv.Limits = NewLimitRegistry()
	return srv.Routes(), srv
}

// authedRequest builds a request with a Bearer atk_ token for the given user.
func authedRequest(t *testing.T, srv *AuthServer, method, path, body string) *http.Request {
	t.Helper()
	ctx := context.Background()
	u, err := srv.Store.CreateUser(ctx, "alice@example.com", "correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, _, err := srv.Store.CreateAPIToken(ctx, u.ID, "test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+secret.Expose())
	r.Header.Set("Content-Type", "application/json")
	r.Host = "relay.example.com"
	return r
}

func TestPairCreate_Unauthorized(t *testing.T) {
	handler, _ := newTestPairServer(t)
	r := httptest.NewRequest(http.MethodPost, "/api/pair/create", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPairCreate_HappyPath_ReturnsTokenAndQRURL(t *testing.T) {
	handler, srv := newTestPairServer(t)
	r := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		QRURL     string `json:"qr_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(resp.Token, "pair_") {
		t.Fatalf("token: got %q", resp.Token)
	}
	if resp.ExpiresAt == 0 {
		t.Fatalf("ExpiresAt zero")
	}
	if !strings.HasPrefix(resp.QRURL, "http://relay.example.com/pair?t=pair_") {
		// httptest.NewRequest has TLS == nil so derivation falls back to http
		t.Fatalf("QRURL: got %q", resp.QRURL)
	}
}
