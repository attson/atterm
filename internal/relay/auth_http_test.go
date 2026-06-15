package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRemovedAuthRoutes_ReturnGone asserts the three retired password-auth
// endpoints (login / signup / change-password) respond 410 Gone with an
// OPAQUE-upgrade hint, so the bundled web UI surfaces a clear error instead
// of a bare 404 when it hits an upgraded relay.
func TestRemovedAuthRoutes_ReturnGone(t *testing.T) {
	s, _ := serverWithSession(t)
	for _, path := range []string{"/api/auth/login", "/api/auth/signup", "/api/me/password"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusGone {
			t.Fatalf("%s: status=%d, want 410", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "OPAQUE") {
			t.Fatalf("%s: body does not mention OPAQUE upgrade: %s", path, rec.Body.String())
		}
	}
}
