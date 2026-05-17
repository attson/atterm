package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func TestListSessions_ReturnsRowsWithIsCurrent(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, cookieValue := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")

	// Create a second session for the same user (simulates another device).
	_, _ = store.CreateWebSession(context.Background(), userID, "other-device", "1.2.3.0/24")

	req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var rows []map[string]any
	json.NewDecoder(rec.Body).Decode(&rows)
	if len(rows) != 2 {
		t.Fatalf("rows=%d; want 2", len(rows))
	}

	currentHash := userstore.SessionHash(cookieValue)
	var foundCurrent bool
	for _, r := range rows {
		if r["id_hash"] == currentHash && r["is_current"] == true {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		t.Error("current session not marked is_current=true")
	}
}

func TestListSessions_RequiresAuth(t *testing.T) {
	srv, _ := newTestAuthServer(t)
	handler := srv.Routes()
	req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d; want 401", rec.Code)
	}
}
