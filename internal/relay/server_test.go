package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
