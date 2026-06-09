package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMe_ReturnsUserAndID: bearer session → GET /api/me → 200 with
// {user_id, email}. CSRF token field is gone post-migration.
func TestMe_ReturnsUserAndID(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)

	w := getWithBearer(srv, "/api/me", tok)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}

	if resp["user_id"] != userID {
		t.Errorf("user_id: got %v, want %v", resp["user_id"], userID)
	}
	if resp["email"] != "a@b" {
		t.Errorf("email: got %v", resp["email"])
	}
	// csrf_token field was removed alongside the cookie/CSRF stack. If anything
	// reintroduces it, this assertion catches the regression.
	if _, ok := resp["csrf_token"]; ok {
		t.Error("/api/me must not expose csrf_token under the session-token contract")
	}
}

// TestMe_RequiresAuth_401: no Authorization header → 401.
func TestMe_RequiresAuth_401(t *testing.T) {
	srv, _, _ := serverWithAuthAndSession(t)
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMe_AdminUser_OK regresses PR A: after admin sessions resolve to
// PrincipalAdmin, handleMe must NOT reject them. PrincipalAdmin is a strict
// superset of PrincipalUser.
func TestMe_AdminUser_OK(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	if err := srv.cfg.Store.SetUserAdmin(context.Background(), userID, true); err != nil {
		t.Fatal(err)
	}

	w := getWithBearer(srv, "/api/me", tok)
	if w.Code != http.StatusOK {
		t.Fatalf("admin /api/me: status %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["user_id"] != userID {
		t.Errorf("user_id: got %v, want %v", resp["user_id"], userID)
	}
	if resp["is_admin"] != true {
		t.Errorf("is_admin: got %v, want true", resp["is_admin"])
	}
}

// TestMe_BearerSessionSource_OK: the Authorization Bearer header carrying a
// session token is the canonical auth path; /api/me resolves the user from it.
func TestMe_BearerSessionSource_OK(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer session token, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["user_id"] != userID {
		t.Errorf("user_id: got %v, want %v", resp["user_id"], userID)
	}
}
