package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

const testAdminToken = "admin-token-for-tests"

// newTestAdminServer returns an AdminServer backed by a real in-memory SQLiteStore.
func newTestAdminServer(t *testing.T) (*AdminServer, *userstore.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	resolver := NewIdentityResolver(store, testAdminToken)
	srv := &AdminServer{
		Store:    store,
		Resolver: resolver,
	}
	return srv, store
}

// adminPost sends a POST with Bearer admin token and JSON body.
func adminPost(handler http.Handler, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// adminGet sends a GET with Bearer admin token.
func adminGet(handler http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestAdmin_CreateInvitation: POST /admin/api/invitations with admin token → 201,
// body contains {plaintext, code_prefix, note}. The plaintext starts with "inv_".
func TestAdmin_CreateInvitation(t *testing.T) {
	srv, _ := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	w := adminPost(handler, "/admin/api/invitations", map[string]interface{}{
		"expires_at": nil,
		"note":       "bob",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}

	plaintext, _ := resp["plaintext"].(string)
	if !strings.HasPrefix(plaintext, "inv_") {
		t.Errorf("plaintext should start with inv_, got %q", plaintext)
	}
	if len(plaintext) < 10 {
		t.Errorf("plaintext too short: %q", plaintext)
	}

	codePrefix, _ := resp["code_prefix"].(string)
	if !strings.HasPrefix(codePrefix, "inv_") {
		t.Errorf("code_prefix should start with inv_, got %q", codePrefix)
	}

	note, _ := resp["note"].(string)
	if note != "bob" {
		t.Errorf("note: got %q, want %q", note, "bob")
	}

	// A subsequent GET /admin/api/invitations should list the invite but NOT expose plaintext.
	wList := adminGet(handler, "/admin/api/invitations")
	if wList.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", wList.Code, wList.Body.String())
	}
	var listResp []map[string]interface{}
	if err := json.Unmarshal(wList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp) != 1 {
		t.Fatalf("expected 1 invite in list, got %d", len(listResp))
	}
	// Must not contain plaintext in list response.
	listJSON := wList.Body.String()
	if strings.Contains(listJSON, plaintext) {
		t.Error("list response leaks plaintext")
	}
}

// TestAdmin_CreateInvitation_DefaultExpiry7Days: when the request body omits
// expires_at, the relay defaults to 7 days from now. Operators don't have to
// remember to set one, and stale codes don't accumulate forever.
func TestAdmin_CreateInvitation_DefaultExpiry7Days(t *testing.T) {
	srv, _ := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	before := time.Now().Add(defaultInviteExpiry).Add(-2 * time.Second)
	w := adminPost(handler, "/admin/api/invitations", map[string]interface{}{"note": ""})
	after := time.Now().Add(defaultInviteExpiry).Add(2 * time.Second)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	expStr, _ := resp["expires_at"].(string)
	if expStr == "" {
		t.Fatalf("expected expires_at to be set by default, got empty/nil; resp=%v", resp)
	}
	got, err := time.Parse(time.RFC3339, expStr)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", expStr, err)
	}
	if got.Before(before) || got.After(after) {
		t.Errorf("expires_at %v not within [%v, %v]", got, before, after)
	}
}

// TestAdmin_CreateInvitation_Batch: count > 1 returns an invites array, each
// invite gets its own plaintext + code_prefix; all share the same expires_at
// and note. Verifies the bulk-create path.
func TestAdmin_CreateInvitation_Batch(t *testing.T) {
	srv, _ := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	w := adminPost(handler, "/admin/api/invitations", map[string]interface{}{
		"note":  "Q3 team",
		"count": 5,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Invites []map[string]interface{} `json:"invites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Invites) != 5 {
		t.Fatalf("expected 5 invites, got %d", len(resp.Invites))
	}
	seen := make(map[string]bool, 5)
	for i, inv := range resp.Invites {
		pt, _ := inv["plaintext"].(string)
		if !strings.HasPrefix(pt, "inv_") {
			t.Errorf("invite %d plaintext bad prefix: %q", i, pt)
		}
		if seen[pt] {
			t.Errorf("duplicate plaintext at index %d: %q", i, pt)
		}
		seen[pt] = true
		if note, _ := inv["note"].(string); note != "Q3 team" {
			t.Errorf("invite %d note=%q, want %q", i, note, "Q3 team")
		}
	}
}

// TestAdmin_CreateInvitation_CountTooLarge: counts > 50 are rejected to bound
// per-request work.
func TestAdmin_CreateInvitation_CountTooLarge(t *testing.T) {
	srv, _ := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	w := adminPost(handler, "/admin/api/invitations", map[string]interface{}{"count": 51})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for count=51, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdmin_ListInvitations: list endpoint returns all invitations.
func TestAdmin_ListInvitations(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	// Create two invitations directly in the store.
	ctx := context.Background()
	for _, note := range []string{"alice", "bob"} {
		if _, _, err := store.CreateInvitation(ctx, nil, note); err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}
	}

	w := adminGet(handler, "/admin/api/invitations")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 invitations, got %d", len(resp))
	}
}

// TestAdmin_CreateInvitation_RequiresAdmin: with a user cookie → 401/403.
func TestAdmin_CreateInvitation_RequiresAdmin(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	// Create a user + web session.
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "user@example.com", "somepassword12345")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sessionSecret, err := store.CreateWebSession(ctx, u.ID, "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	b, _ := json.Marshal(map[string]interface{}{"expires_at": nil, "note": "test"})
	r := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "atterm_session", Value: sessionSecret.Expose()})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("expected 401 or 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdmin_ListUsers: GET /admin/api/users → 200 with user rows.
func TestAdmin_ListUsers(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	ctx := context.Background()
	if _, err := store.CreateUser(ctx, "user1@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.CreateUser(ctx, "user2@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	w := adminGet(handler, "/admin/api/users")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp))
	}

	// Each row must have id, email, created_at.
	for i, row := range resp {
		if row["id"] == nil || row["id"] == "" {
			t.Errorf("row %d: missing id", i)
		}
		if row["email"] == nil || row["email"] == "" {
			t.Errorf("row %d: missing email", i)
		}
		if row["created_at"] == nil || row["created_at"] == "" {
			t.Errorf("row %d: missing created_at", i)
		}
	}
}

// TestAdmin_ResetPassword: reset → new tmp_ password works for login;
// old web_sessions gone; CSRF tokens invalidated (csrf_secret rotated).
func TestAdmin_ResetPassword(t *testing.T) {
	adminSrv, store := newTestAdminServer(t)
	adminHandler := adminSrv.AdminRoutes()

	// Also build an AuthServer so we can test login.
	pool := NewArgon2Pool(1)
	resolver := NewIdentityResolver(store, testAdminToken)
	authSrv := &AuthServer{
		Store:        store,
		Resolver:     resolver,
		Argon:        pool,
		FailureFloor: 0, // no floor in tests
	}
	authHandler := authSrv.Routes()

	ctx := context.Background()

	// Create a user and a web session.
	u, err := store.CreateUser(ctx, "alice@example.com", "originalpassword12345")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sessionSecret, err := store.CreateWebSession(ctx, u.ID, "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}
	oldCookieValue := sessionSecret.Expose()

	// Capture csrf_secret before reset.
	var csrfBefore []byte
	if err := store.DB().QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&csrfBefore); err != nil {
		t.Fatalf("csrf before: %v", err)
	}

	// POST /admin/api/users/{id}/reset-password.
	w := adminPost(adminHandler, "/admin/api/users/"+u.ID+"/reset-password", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reset-password: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resetResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resetResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	plaintext, _ := resetResp["plaintext"].(string)
	if !strings.HasPrefix(plaintext, "tmp_") {
		t.Fatalf("plaintext should start with tmp_, got %q", plaintext)
	}

	// New password must work via /api/auth/login.
	loginResp := postJSON(authHandler, "/api/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": plaintext,
	})
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login with new password: expected 200, got %d: %s", loginResp.Code, loginResp.Body.String())
	}

	// Old password must NOT work.
	oldLoginResp := postJSON(authHandler, "/api/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": "originalpassword12345",
	})
	if oldLoginResp.Code == http.StatusOK {
		t.Error("old password still works after reset")
	}

	// Old web session must be gone — LookupWebSession should fail.
	_, _, err = store.LookupWebSession(ctx, oldCookieValue)
	if err == nil {
		t.Error("old web session still valid after reset")
	}

	// csrf_secret must be rotated.
	var csrfAfter []byte
	if err := store.DB().QueryRowContext(ctx,
		`SELECT csrf_secret FROM users WHERE id=?`, u.ID).Scan(&csrfAfter); err != nil {
		t.Fatalf("csrf after: %v", err)
	}
	if string(csrfBefore) == string(csrfAfter) {
		t.Error("csrf_secret was not rotated by password reset")
	}
}

// TestAdmin_DisableUser: disable → subsequent login returns 401.
func TestAdmin_DisableUser(t *testing.T) {
	adminSrv, store := newTestAdminServer(t)
	adminHandler := adminSrv.AdminRoutes()

	pool := NewArgon2Pool(1)
	resolver := NewIdentityResolver(store, testAdminToken)
	authSrv := &AuthServer{
		Store:        store,
		Resolver:     resolver,
		Argon:        pool,
		FailureFloor: 0,
	}
	authHandler := authSrv.Routes()

	ctx := context.Background()
	u, err := store.CreateUser(ctx, "bob@example.com", "bobspassword12345")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Verify login works before disable.
	wBefore := postJSON(authHandler, "/api/auth/login", map[string]string{
		"email": "bob@example.com", "password": "bobspassword12345",
	})
	if wBefore.Code != http.StatusOK {
		t.Fatalf("login before disable: expected 200, got %d", wBefore.Code)
	}

	// Disable via admin endpoint.
	w := adminPost(adminHandler, "/admin/api/users/"+u.ID+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Login must now fail.
	wAfter := postJSON(authHandler, "/api/auth/login", map[string]string{
		"email": "bob@example.com", "password": "bobspassword12345",
	})
	if wAfter.Code != http.StatusUnauthorized {
		t.Errorf("login after disable: expected 401, got %d: %s", wAfter.Code, wAfter.Body.String())
	}
}
