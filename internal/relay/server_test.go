package relay

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// The 10 tests below verify that the five WS/API routes are gated by the new
// requireSession middleware (Task 1.9). Each route is asserted twice:
//   - No Authorization → 401
//   - Valid session token → not 401 (the route is reached). For WS routes we
//     send synthetic upgrade headers; the WS handshake itself fails inside
//     httptest (no hijack), but that failure path runs AFTER auth, so we just
//     assert "not 401". For /api/sessions (pure HTTP) we expect 200 OK.

func TestServer_AgentRoute_RejectsWithoutSession(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest("GET", "/agent", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/agent unauth: got %d want 401", rec.Code)
	}
}

func TestServer_AgentRoute_AcceptsSession(t *testing.T) {
	s, tok := serverWithSession(t)
	req := httptest.NewRequest("GET", "/agent", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	// 101 (Switching Protocols) or 400/500 (synthetic WS handshake fails)
	// both indicate auth gate passed. 401 is the failure mode.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("/agent rejected a valid session token: %d", rec.Code)
	}
}

func TestServer_UplinkRoute_RejectsWithoutSession(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest("GET", "/uplink", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/uplink unauth: got %d want 401", rec.Code)
	}
}

func TestServer_UplinkRoute_AcceptsSession(t *testing.T) {
	s, tok := serverWithSession(t)
	req := httptest.NewRequest("GET", "/uplink", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("/uplink rejected a valid session token: %d", rec.Code)
	}
}

func TestServer_ClientRoute_RejectsWithoutSession(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest("GET", "/client", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/client unauth: got %d want 401", rec.Code)
	}
}

func TestServer_ClientRoute_AcceptsSession(t *testing.T) {
	s, tok := serverWithSession(t)
	req := httptest.NewRequest("GET", "/client", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("/client rejected a valid session token: %d", rec.Code)
	}
}

func TestServer_ServiceRoutesRequireSession(t *testing.T) {
	for _, path := range []string{"/service-client", "/service-host"} {
		t.Run(path+" unauthenticated", func(t *testing.T) {
			s, _ := serverWithSession(t)
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s unauth: got %d want 401", path, rec.Code)
			}
		})
		t.Run(path+" authenticated", func(t *testing.T) {
			s, token := serverWithSession(t)
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("%s rejected valid session", path)
			}
		})
	}
}

func TestServer_ClientSessionsRoute_RejectsWithoutSession(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest("GET", "/client-sessions", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/client-sessions unauth: got %d want 401", rec.Code)
	}
}

func TestServer_ClientSessionsRoute_AcceptsSession(t *testing.T) {
	s, tok := serverWithSession(t)
	req := httptest.NewRequest("GET", "/client-sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGVzdC1rZXktMTIzNDU2Nzg=")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("/client-sessions rejected a valid session token: %d", rec.Code)
	}
}

func TestServer_SessionsRoute_RejectsWithoutSession(t *testing.T) {
	s, _ := serverWithSession(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api/sessions unauth: got %d want 401", rec.Code)
	}
}

func TestServer_SessionsRoute_AcceptsSession(t *testing.T) {
	s, tok := serverWithSession(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("/api/sessions rejected a valid session token: %d", rec.Code)
	}
}

// TestServer_OPAQUERoutes_Mounted is the T11 wire-in smoke test. It boots
// NewServer with Config.OpaqueServer set (the production path) and POSTs
// to each of the four OPAQUE endpoints. The bodies are intentionally
// malformed — we only care that the routes are MOUNTED (not 404). Any
// status other than 404 (typically 400 from json.Decode or the "missing
// fields" guard) confirms the handler ran. If this test ever returns 404
// the wire-in regressed, which is the regression T11 is meant to prevent.
func TestServer_OPAQURoutes_Mounted(t *testing.T) {
	store := userstore.NewInMemory(t)
	opaqueSrv, err := LoadOrInitOpaqueServer(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrInitOpaqueServer: %v", err)
	}
	s := NewServer(Config{
		Store:        store,
		Resolver:     NewIdentityResolver(store),
		OpaqueServer: opaqueSrv,
	})

	paths := []string{
		"/api/auth/register/init",
		"/api/auth/register/finalize",
		"/api/auth/login/init",
		"/api/auth/login/finalize",
	}
	for _, p := range paths {
		req := httptest.NewRequest("POST", p, bytes.NewReader([]byte("{}")))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s returned 404 — OPAQUE handler not wired into NewServer", p)
		}
	}
}
