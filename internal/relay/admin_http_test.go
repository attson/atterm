package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// newTestAdminServer returns an AdminServer backed by a real in-memory SQLiteStore.
func newTestAdminServer(t *testing.T) (*AdminServer, *userstore.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	resolver := NewIdentityResolver(store)
	srv := &AdminServer{
		Store:    store,
		Resolver: resolver,
	}
	return srv, store
}

// bootstrapAdminUser creates an admin user backed by the given store and returns
// a valid session cookie + CSRF token for that user. Replaces the old Bearer
// admin-token pattern: every admin test gets a fresh admin user whose cookie
// the IdentityResolver promotes to PrincipalAdmin.
func bootstrapAdminUser(t *testing.T, store userstore.Store) (userID string, cookie *http.Cookie, csrfToken string) {
	t.Helper()
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "admin@example.com", "passphrase-fixture-1234")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserAdmin: %v", err)
	}
	secret, err := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}
	cookieValue := secret.Expose()
	cookie = &http.Cookie{Name: "atterm_session", Value: cookieValue}
	csrfToken = CSRFToken(cookieValue, u.CSRFSecret())
	return u.ID, cookie, csrfToken
}

// adminPostAs sends a POST with the supplied admin cookie + CSRF token.
func adminPostAs(handler http.Handler, path string, body interface{}, cookie *http.Cookie, csrfToken string) *httptest.ResponseRecorder {
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

// adminGetAs sends a GET with the supplied admin cookie. CSRF header is not
// required for GET (RequireCSRF bypasses GET/HEAD/OPTIONS).
func adminGetAs(handler http.Handler, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

// TestAdmin_CreateInvitation: POST /admin/api/invitations with admin cookie → 201,
// body contains {plaintext, code_prefix, note}. The plaintext starts with "inv_".
func TestAdmin_CreateInvitation(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()
	_, cookie, csrf := bootstrapAdminUser(t, store)

	w := adminPostAs(handler, "/admin/api/invitations", map[string]interface{}{
		"expires_at": nil,
		"note":       "bob",
	}, cookie, csrf)
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
	wList := adminGetAs(handler, "/admin/api/invitations", cookie)
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
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()
	_, cookie, csrf := bootstrapAdminUser(t, store)

	before := time.Now().Add(defaultInviteExpiry).Add(-2 * time.Second)
	w := adminPostAs(handler, "/admin/api/invitations", map[string]interface{}{"note": ""}, cookie, csrf)
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
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()
	_, cookie, csrf := bootstrapAdminUser(t, store)

	w := adminPostAs(handler, "/admin/api/invitations", map[string]interface{}{
		"note":  "Q3 team",
		"count": 5,
	}, cookie, csrf)
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
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()
	_, cookie, csrf := bootstrapAdminUser(t, store)

	w := adminPostAs(handler, "/admin/api/invitations", map[string]interface{}{"count": 51}, cookie, csrf)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for count=51, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdmin_ListInvitations: list endpoint returns all invitations.
func TestAdmin_ListInvitations(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()
	_, cookie, _ := bootstrapAdminUser(t, store)

	// Create two invitations directly in the store.
	ctx := context.Background()
	for _, note := range []string{"alice", "bob"} {
		if _, _, err := store.CreateInvitation(ctx, nil, note); err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}
	}

	w := adminGetAs(handler, "/admin/api/invitations", cookie)
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

// TestAdmin_CreateInvitation_RequiresAdmin: with a non-admin user cookie → 401/403.
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
	_, cookie, _ := bootstrapAdminUser(t, store)

	ctx := context.Background()
	if _, err := store.CreateUser(ctx, "user1@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.CreateUser(ctx, "user2@example.com", "correcthorsebattery"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	w := adminGetAs(handler, "/admin/api/users", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 3 users: bootstrapAdminUser's admin + the two created above.
	if len(resp) != 3 {
		t.Fatalf("expected 3 users, got %d", len(resp))
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
	_, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

	// Also build an AuthServer so we can test login.
	pool := NewArgon2Pool(1)
	resolver := NewIdentityResolver(store)
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
	w := adminPostAs(adminHandler, "/admin/api/users/"+u.ID+"/reset-password", nil, adminCookie, adminCSRF)
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
	_, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

	pool := NewArgon2Pool(1)
	resolver := NewIdentityResolver(store)
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
	w := adminPostAs(adminHandler, "/admin/api/users/"+u.ID+"/disable", nil, adminCookie, adminCSRF)
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

// TestAdminAPI_NonAdminUser_Unauthorized: a non-admin user's cookie session
// hitting any /admin/api endpoint must be rejected. Verifies the requireAdmin
// gate denies PrincipalUser even for read-only routes (GET, no CSRF needed).
func TestAdminAPI_NonAdminUser_Unauthorized(t *testing.T) {
	srv, store := newTestAdminServer(t)
	handler := srv.AdminRoutes()

	ctx := context.Background()
	u, err := store.CreateUser(ctx, "user@example.com", "passphrase-1234")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	secret, err := store.CreateWebSession(ctx, u.ID, "ua/test", "203.0.113.0/24")
	if err != nil {
		t.Fatalf("CreateWebSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/invitations", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Errorf("non-admin user hitting /admin/api/invitations: status=%d, want 401 or 403", rec.Code)
	}
}

// TestAdminPromoteUser_Success: POST /admin/api/users/{id}/admin promotes a user to admin.
// Response 204 (No Content). Target user's is_admin flag is set.
func TestAdminPromoteUser_Success(t *testing.T) {
	handler, store := newTestAdminServer(t)
	_, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

	ctx := context.Background()
	target, _ := store.CreateUser(ctx, "target@example.com", "passphrase-1234")
	if target.IsAdmin {
		t.Fatal("target already admin")
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+target.ID+"/admin", nil)
	req.AddCookie(adminCookie)
	req.Header.Set("X-CSRF-Token", adminCSRF)
	rec := httptest.NewRecorder()
	handler.AdminRoutes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204; body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUser(ctx, target.ID)
	if !got.IsAdmin {
		t.Error("target user not promoted after POST .../admin")
	}
}

// TestAdminPromoteUser_AuditLog: POST /admin/api/users/{id}/admin writes an audit log
// with the format "admin role change: actor=<id> target=<id> op=promote".
func TestAdminPromoteUser_AuditLog(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	oldLogger := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(oldLogger) })

	handler, store := newTestAdminServer(t)
	actorID, adminCookie, adminCSRF := bootstrapAdminUser(t, store)

	ctx := context.Background()
	target, _ := store.CreateUser(ctx, "t@example.com", "passphrase-1234")

	req := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+target.ID+"/admin", nil)
	req.AddCookie(adminCookie)
	req.Header.Set("X-CSRF-Token", adminCSRF)
	handler.AdminRoutes().ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if !strings.Contains(out, "admin role change") ||
		!strings.Contains(out, "actor="+actorID) ||
		!strings.Contains(out, "target="+target.ID) ||
		!strings.Contains(out, "op=promote") {
		t.Errorf("audit log line missing or malformed:\n%s", out)
	}
}
