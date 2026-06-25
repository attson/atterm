package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNodesAndSetHome(t *testing.T) {
	srv, store := newTestSeenServer(t) // returns (*Server, *userstore.SQLiteStore)
	ctx := context.Background()
	token, userID := createUserWithSession(t, store, "nodes@example.com")
	_ = store.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", time.Now().Unix())

	// GET /api/nodes
	req := httptest.NewRequest("GET", "/api/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/nodes = %d", rec.Code)
	}
	var listResp struct {
		Nodes []struct {
			InstanceID string `json:"instance_id"`
			PublicURL  string `json:"public_url"`
		} `json:"nodes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if len(listResp.Nodes) != 1 || listResp.Nodes[0].InstanceID != "https://a.example" {
		t.Fatalf("nodes = %+v", listResp.Nodes)
	}

	// PUT /api/me/home with a live instance succeeds.
	body := strings.NewReader(`{"instance_id":"https://a.example"}`)
	req2 := httptest.NewRequest("PUT", "/api/me/home", body)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT /api/me/home = %d (%s)", rec2.Code, rec2.Body.String())
	}
	if id, ok, _ := store.GetUserHome(ctx, userID); !ok || id != "https://a.example" {
		t.Fatalf("home not set: %q %v", id, ok)
	}

	// PUT with a non-live instance is rejected.
	bad := strings.NewReader(`{"instance_id":"https://ghost.example"}`)
	req3 := httptest.NewRequest("PUT", "/api/me/home", bad)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad instance = %d, want 400", rec3.Code)
	}
}
