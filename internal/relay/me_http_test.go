package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// signupAndLogin is a test helper: creates an invite, signs up, logs in,
// and returns the session cookie value and the signed-in user's ID + email.
func signupAndLogin(t *testing.T, handler http.Handler, store *userstore.SQLiteStore, email, password string) (cookie *http.Cookie, userID, cookieValue string) {
	t.Helper()
	code := createInvite(t, store)
	wSignup := doSignup(t, handler, email, password, code)
	if wSignup.Code != http.StatusOK {
		t.Fatalf("signup: expected 200, got %d: %s", wSignup.Code, wSignup.Body.String())
	}
	for _, c := range wSignup.Result().Cookies() {
		if c.Name == "atterm_session" {
			cookie = c
			cookieValue = c.Value
		}
	}
	if cookie == nil {
		t.Fatal("signupAndLogin: no session cookie")
	}
	var resp map[string]interface{}
	json.Unmarshal(wSignup.Body.Bytes(), &resp)
	userID, _ = resp["user_id"].(string)
	return
}

// getWithCookie sends a GET to handler with the given cookie.
func getWithCookie(handler http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// csrfTokenFor fetches /api/me and extracts the csrf_token field.
func csrfTokenFor(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	w := getWithCookie(handler, "/api/me", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("csrfTokenFor: /api/me returned %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	tok, _ := resp["csrf_token"].(string)
	return tok
}

// postJSONWithCSRF sends a POST with JSON body and CSRF header.
func postJSONWithCSRF(handler http.Handler, path string, body interface{}, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrfToken != "" {
		r.Header.Set("X-CSRF-Token", csrfToken)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// deleteWithCSRF sends a DELETE with CSRF header.
func deleteWithCSRF(handler http.Handler, path string, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodDelete, path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrfToken != "" {
		r.Header.Set("X-CSRF-Token", csrfToken)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestMe_ReturnsUserAndCSRFToken: cookie session → GET /api/me → 200 with
// {user_id, email, csrf_token}. csrf_token matches CSRFToken(cookieValue, csrfSecret).
func TestMe_ReturnsUserAndCSRFToken(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, userID, cookieValue := signupAndLogin(t, handler, store, "me@example.com", "correcthorsebattery")

	w := getWithCookie(handler, "/api/me", cookie)
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
	if resp["email"] != "me@example.com" {
		t.Errorf("email: got %v", resp["email"])
	}
	csrfToken, ok := resp["csrf_token"].(string)
	if !ok || csrfToken == "" {
		t.Fatal("csrf_token missing or empty")
	}

	// Derive expected CSRF token using the same helper.
	user, err := store.GetUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	expected := CSRFToken(cookieValue, user.CSRFSecret())
	if csrfToken != expected {
		t.Errorf("csrf_token: got %q, want %q", csrfToken, expected)
	}
}

// TestMe_RequiresAuth_401: no cookie, no API token → 401.
func TestMe_RequiresAuth_401(t *testing.T) {
	srv, _ := newTestAuthServer(t)
	handler := srv.Routes()

	w := getWithCookie(handler, "/api/me", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestMe_AdminUser_OK regressing PR A: after admin sessions resolve to
// PrincipalAdmin, handleMe's old `p.Kind != PrincipalUser` check rejected
// admins with 401, breaking the entire post-login frontend flow.
// PrincipalAdmin must be a strict superset of PrincipalUser everywhere.
func TestMe_AdminUser_OK(t *testing.T) {
    srv, store := newTestAuthServer(t)
    handler := srv.Routes()

    cookie, userID, _ := signupAndLogin(t, handler, store, "admin@example.com", "correcthorsebattery")
    if err := store.SetUserAdmin(context.Background(), userID, true); err != nil {
        t.Fatal(err)
    }

    w := getWithCookie(handler, "/api/me", cookie)
    if w.Code != http.StatusOK {
        t.Fatalf("admin /api/me: status %d, want 200; body=%s", w.Code, w.Body.String())
    }
    var resp map[string]interface{}
    json.Unmarshal(w.Body.Bytes(), &resp)
    if resp["user_id"] != userID {
        t.Errorf("user_id: got %v, want %v", resp["user_id"], userID)
    }
    if _, ok := resp["csrf_token"].(string); !ok {
        t.Error("csrf_token missing for admin cookie session")
    }
}

// TestMe_ApiTokenSource_OK: API token (not cookie) → 200; csrf_token may be
// empty because there is no cookie to derive it from.
func TestMe_ApiTokenSource_OK(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	// Create a user.
	cookie, userID, _ := signupAndLogin(t, handler, store, "apiuser@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	// Create an API token for the user.
	wCreate := postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "test-device"}, cookie, csrfTok)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create token: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	var tokResp map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &tokResp)
	plaintext, _ := tokResp["plaintext"].(string)
	if !strings.HasPrefix(plaintext, "atk_") {
		t.Fatalf("plaintext does not start with atk_: %q", plaintext)
	}

	// Now use the API token (not cookie) to call /api/me.
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with API token, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["user_id"] != userID {
		t.Errorf("user_id: got %v, want %v", resp["user_id"], userID)
	}
	// csrf_token is empty when sourced from API token (no cookie session to derive it from).
	// Do not assert it is non-empty here.
}

// TestCreateAPIToken_ReturnsPlaintext: POST /api/me/tokens with CSRF → 201 with
// {id, plaintext, prefix, created_at}. plaintext starts with "atk_".
// Subsequent GET /api/me/tokens does not include plaintext in any row.
func TestCreateAPIToken_ReturnsPlaintext(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "tok@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	w := postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "laptop"}, cookie, csrfTok)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if resp["id"] == "" {
		t.Error("id missing")
	}
	plaintext, _ := resp["plaintext"].(string)
	if !strings.HasPrefix(plaintext, "atk_") {
		t.Errorf("plaintext should start with atk_, got %q", plaintext)
	}
	if resp["prefix"] == "" {
		t.Error("prefix missing")
	}
	if resp["created_at"] == "" {
		t.Error("created_at missing")
	}

	// Subsequent GET must not include plaintext.
	wList := getWithCookie(handler, "/api/me/tokens", cookie)
	if wList.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", wList.Code)
	}
	var listResp []map[string]interface{}
	json.Unmarshal(wList.Body.Bytes(), &listResp)
	for _, row := range listResp {
		if _, hasPlaintext := row["plaintext"]; hasPlaintext {
			t.Error("list response must not include plaintext field")
		}
	}
}

// TestListAPITokens_OK: GET /api/me/tokens → 200 array with expected fields,
// no plaintext in any row.
func TestListAPITokens_OK(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "list@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	// Create two tokens.
	postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "token-one"}, cookie, csrfTok)
	postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "token-two"}, cookie, csrfTok)

	w := getWithCookie(handler, "/api/me/tokens", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d", len(rows))
	}
	for _, row := range rows {
		if row["id"] == "" || row["id"] == nil {
			t.Error("row missing id")
		}
		if row["name"] == "" || row["name"] == nil {
			t.Error("row missing name")
		}
		if row["prefix"] == "" || row["prefix"] == nil {
			t.Error("row missing prefix")
		}
		if row["created_at"] == "" || row["created_at"] == nil {
			t.Error("row missing created_at")
		}
		if _, hasPlaintext := row["plaintext"]; hasPlaintext {
			t.Error("row must not have plaintext field")
		}
		if _, hasTokenHash := row["token_hash"]; hasTokenHash {
			t.Error("row must not have token_hash field")
		}
	}
}

// TestRevokeAPIToken_OK: DELETE /api/me/tokens/{id} → 204.
// Subsequent LookupAPIToken on the plaintext returns ErrTokenInvalid.
func TestRevokeAPIToken_OK(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "revoke@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	// Create a token.
	wCreate := postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "to-revoke"}, cookie, csrfTok)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	var tokResp map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &tokResp)
	tokID, _ := tokResp["id"].(string)
	plaintext, _ := tokResp["plaintext"].(string)

	// Revoke it.
	w := deleteWithCSRF(handler, "/api/me/tokens/"+tokID, cookie, csrfTok)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the token is now invalid via the store.
	_, _, err := store.LookupAPIToken(context.Background(), plaintext)
	if err == nil {
		t.Fatal("LookupAPIToken should fail after revoke")
	}
}

// TestRevokeAPIToken_CannotRevokeAnotherUsers_404: user_B attempts to revoke
// user_A's token → 404 (not 403, to avoid existence leak).
func TestRevokeAPIToken_CannotRevokeAnotherUsers_404(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	// User A: create and get token.
	cookieA, _, _ := signupAndLogin(t, handler, store, "usera@example.com", "correcthorsebattery")
	csrfA := csrfTokenFor(t, handler, cookieA)
	wCreate := postJSONWithCSRF(handler, "/api/me/tokens", map[string]string{"name": "userA-token"}, cookieA, csrfA)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("userA create token: expected 201, got %d", wCreate.Code)
	}
	var tokResp map[string]interface{}
	json.Unmarshal(wCreate.Body.Bytes(), &tokResp)
	tokAID, _ := tokResp["id"].(string)

	// User B: try to delete user A's token.
	cookieB, _, _ := signupAndLogin(t, handler, store, "userb@example.com", "correcthorsebattery")
	csrfB := csrfTokenFor(t, handler, cookieB)
	w := deleteWithCSRF(handler, "/api/me/tokens/"+tokAID, cookieB, csrfB)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
