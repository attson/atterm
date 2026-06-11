package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestGetPreferences_ReturnsEmptyItemsForFreshUser(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)

	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("status %d: %s", rec.Code, rec.Body.String()) }
	var body struct {
		Items []userstore.PreferenceItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("expected empty items, got %d", len(body.Items))
	}
}

func TestGetPreferences_RequiresAuth(t *testing.T) {
	s, _, _ := serverWithSessionAndUser(t)
	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGetPreferences_ReturnsStoredRows(t *testing.T) {
	s, tok, userID := serverWithSessionAndUser(t)

	// Seed one row directly through the store.
	store := s.Store()
	_, err := store.SetUserPreferences(context.Background(), userID,
		time.Now().UnixMilli(),
		[]userstore.PreferenceItem{
			{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 100},
		})
	if err != nil { t.Fatalf("seed: %v", err) }

	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status %d", rec.Code) }
	if !strings.Contains(rec.Body.String(), `"locale_preference"`) {
		t.Fatalf("missing key in body: %s", rec.Body.String())
	}
}
