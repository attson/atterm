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

func TestDeleteSession_OwnerDeletes_204(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
	csrf := csrfTokenFor(t, handler, cookie)
	_, _ = store.CreateWebSession(context.Background(), userID, "other-device", "")
	list, _ := store.ListUserWebSessions(context.Background(), userID)
	var target string
	for _, s := range list {
		if s.UserAgent == "other-device" {
			target = s.IDHash
		}
	}
	if target == "" {
		t.Fatal("setup: other-device session missing")
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/me/sessions/"+target, nil)
	req.AddCookie(cookie)
	req.Header.Set("X-CSRF-Token", csrf)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	after, _ := store.ListUserWebSessions(context.Background(), userID)
	if len(after) != 1 {
		t.Errorf("expected 1 session left, got %d", len(after))
	}
}

func TestDeleteSession_OtherUserSession_404(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookieA, _, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
	csrfA := csrfTokenFor(t, handler, cookieA)

	userB, _ := store.CreateUser(context.Background(), "b@example.com", "passphrase-1234")
	_, _ = store.CreateWebSession(context.Background(), userB.ID, "ua-b", "")
	listB, _ := store.ListUserWebSessions(context.Background(), userB.ID)

	req := httptest.NewRequest(http.MethodDelete, "/api/me/sessions/"+listB[0].IDHash, nil)
	req.AddCookie(cookieA)
	req.Header.Set("X-CSRF-Token", csrfA)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d; want 404 (cross-user)", rec.Code)
	}
	// B's session must still exist.
	afterB, _ := store.ListUserWebSessions(context.Background(), userB.ID)
	if len(afterB) != 1 {
		t.Errorf("B's session lost: %+v", afterB)
	}
}
