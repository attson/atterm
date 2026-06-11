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

func TestPutPreferences_InsertsAndReturnsFullState(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)

	body := `{"items":[
		{"key":"locale_preference","value":"zh-CN","client_updated_at":1700000000000},
		{"key":"quick_templates","value":[{"id":"a","label":"a","text":"a"}],"client_updated_at":1700000000000}
	]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("status %d: %s", rec.Code, rec.Body.String()) }
	var resp struct {
		Items []userstore.PreferenceItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil { t.Fatalf("decode: %v", err) }
	if len(resp.Items) != 2 { t.Fatalf("got %d items: %s", len(resp.Items), rec.Body.String()) }
}

func TestPutPreferences_RejectsUnknownKey(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	body := `{"items":[{"key":"evil","value":"x","client_updated_at":1}]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutPreferences_OlderTimestampIsRejectedAndCurrentReturned(t *testing.T) {
	s, tok, userID := serverWithSessionAndUser(t)

	store := s.Store()
	_, _ = store.SetUserPreferences(context.Background(), userID, 5000,
		[]userstore.PreferenceItem{
			{Key: "locale_preference", ValueJSON: json.RawMessage(`"zh-CN"`), UpdatedAt: 5000},
		})

	body := `{"items":[{"key":"locale_preference","value":"en","client_updated_at":1000}]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status %d", rec.Code) }
	if !strings.Contains(rec.Body.String(), `"zh-CN"`) {
		t.Fatalf("expected server value preserved, body=%s", rec.Body.String())
	}
}
