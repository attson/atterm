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
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	// Create a second session for the same user (simulates another device).
	_, _, _ = store.CreateSession(context.Background(), userID, "other-device", "1.2.3.0/24", userstore.DefaultSessionTTL)

	req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var rows []map[string]any
	json.NewDecoder(rec.Body).Decode(&rows)
	if len(rows) != 2 {
		t.Fatalf("rows=%d; want 2", len(rows))
	}

	currentHash := userstore.SessionHash(tok)
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
	srv, _, _ := serverWithAuthAndSession(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me/sessions", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status=%d; want 401", rec.Code)
	}
}

func TestDeleteSession_OwnerDeletes_204(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)
	_, _, _ = store.CreateSession(context.Background(), userID, "other-device", "", userstore.DefaultSessionTTL)
	list, _ := store.ListSessions(context.Background(), userID)
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
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	after, _ := store.ListSessions(context.Background(), userID)
	if len(after) != 1 {
		t.Errorf("expected 1 session left, got %d", len(after))
	}
}

func TestDeleteSession_OtherUserSession_404(t *testing.T) {
	srv, tokA, _ := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	userB, _ := store.CreateUser(context.Background(), "b@example.com", "passphrase-1234")
	_, _, _ = store.CreateSession(context.Background(), userB.ID, "ua-b", "", userstore.DefaultSessionTTL)
	listB, _ := store.ListSessions(context.Background(), userB.ID)

	req := httptest.NewRequest(http.MethodDelete, "/api/me/sessions/"+listB[0].IDHash, nil)
	req.Header.Set("Authorization", "Bearer "+tokA)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d; want 404 (cross-user)", rec.Code)
	}
	// B's session must still exist.
	afterB, _ := store.ListSessions(context.Background(), userB.ID)
	if len(afterB) != 1 {
		t.Errorf("B's session lost: %+v", afterB)
	}
}

func TestSignOutOthers_DeletesAllButCurrent(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	_, _, _ = store.CreateSession(context.Background(), userID, "device-2", "", userstore.DefaultSessionTTL)
	_, _, _ = store.CreateSession(context.Background(), userID, "device-3", "", userstore.DefaultSessionTTL)

	req := httptest.NewRequest(http.MethodPost, "/api/me/sessions/sign-out-others", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]int64
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["deleted"] != 2 {
		t.Errorf("deleted=%d; want 2", resp["deleted"])
	}

	after, _ := store.ListSessions(context.Background(), userID)
	if len(after) != 1 {
		t.Errorf("expected 1 session left, got %d", len(after))
	}
}
