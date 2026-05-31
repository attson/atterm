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

func TestPairConsume_HappyPath_ReturnsCredsAndAuthenticates(t *testing.T) {
	handler, srv := newTestPairServer(t)

	// Mint via the authed create endpoint.
	rCreate := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	wCreate := httptest.NewRecorder()
	handler.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create: %d", wCreate.Code)
	}
	var created struct {
		Token string `json:"token"`
	}
	json.Unmarshal(wCreate.Body.Bytes(), &created)

	// Consume — no auth header.
	body, _ := json.Marshal(map[string]string{"token": created.Token})
	rConsume := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
	rConsume.Header.Set("Content-Type", "application/json")
	rConsume.Host = "relay.example.com"
	wConsume := httptest.NewRecorder()
	handler.ServeHTTP(wConsume, rConsume)

	if wConsume.Code != http.StatusOK {
		t.Fatalf("consume: expected 200, got %d: %s", wConsume.Code, wConsume.Body.String())
	}
	var resp struct {
		RelayURL string `json:"relay_url"`
		APIToken string `json:"api_token"`
		User     struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(wConsume.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(resp.APIToken, "atk_") {
		t.Fatalf("api_token: got %q", resp.APIToken)
	}
	if resp.User.Email != "alice@example.com" {
		t.Fatalf("user.email: got %q", resp.User.Email)
	}
	if resp.RelayURL == "" {
		t.Fatalf("relay_url empty")
	}

	// The minted api_token must authenticate against /api/me.
	rMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rMe.Header.Set("Authorization", "Bearer "+resp.APIToken)
	wMe := httptest.NewRecorder()
	handler.ServeHTTP(wMe, rMe)
	if wMe.Code != http.StatusOK {
		t.Fatalf("/api/me with new token: %d %s", wMe.Code, wMe.Body.String())
	}
}

func TestPairConsume_SecondTime_404(t *testing.T) {
	handler, srv := newTestPairServer(t)
	rCreate := authedRequest(t, srv, http.MethodPost, "/api/pair/create", "{}")
	wCreate := httptest.NewRecorder()
	handler.ServeHTTP(wCreate, rCreate)
	var created struct {
		Token string `json:"token"`
	}
	json.Unmarshal(wCreate.Body.Bytes(), &created)

	doConsume := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"token": created.Token})
		r := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := doConsume(); w.Code != http.StatusOK {
		t.Fatalf("first consume: %d", w.Code)
	}
	w := doConsume()
	if w.Code != http.StatusNotFound {
		t.Fatalf("second consume: expected 404, got %d", w.Code)
	}
	var errBody map[string]string
	json.Unmarshal(w.Body.Bytes(), &errBody)
	if errBody["code"] != "pair_invalid" {
		t.Fatalf("code: got %q want pair_invalid", errBody["code"])
	}
}

func TestPairConsume_UnknownToken_404(t *testing.T) {
	handler, _ := newTestPairServer(t)
	body, _ := json.Marshal(map[string]string{"token": "pair_DEFINITELYNOTREAL"})
	r := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
