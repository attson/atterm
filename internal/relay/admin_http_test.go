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

// newAdminTestServer returns a Server backed by an in-memory DBStore with
// a fresh admin user. Returns the Server, the store, the admin user ID, and a
// Bearer session token for that admin.
func newAdminTestServer(t *testing.T) (*Server, *userstore.DBStore, string, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatalf("SetUserAdmin: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua/test", "203.0.113.0/24", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
	})
	return srv, store, u.ID, tok
}

// adminPostBearer posts a JSON body to path with the admin Bearer header set.
func adminPostBearer(srv http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// adminGetBearer issues a GET with the admin Bearer header set.
func adminGetBearer(srv http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// adminDeleteBearer issues a DELETE with the admin Bearer header set.
func adminDeleteBearer(srv http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodDelete, path, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

// TestAdmin_CreateInvitation: POST /admin/api/invitations with admin Bearer → 201,
// body contains {plaintext, code_prefix, note}. The plaintext starts with "inv_".
func TestAdmin_CreateInvitation(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)

	w := adminPostBearer(srv, "/admin/api/invitations", map[string]interface{}{
		"expires_at": nil,
		"note":       "bob",
	}, tok)
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
	wList := adminGetBearer(srv, "/admin/api/invitations", tok)
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
// expires_at, the relay defaults to 7 days from now.
func TestAdmin_CreateInvitation_DefaultExpiry7Days(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)

	before := time.Now().Add(defaultInviteExpiry).Add(-2 * time.Second)
	w := adminPostBearer(srv, "/admin/api/invitations", map[string]interface{}{"note": ""}, tok)
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

// TestAdmin_CreateInvitation_Batch: count > 1 returns an invites array.
func TestAdmin_CreateInvitation_Batch(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)

	w := adminPostBearer(srv, "/admin/api/invitations", map[string]interface{}{
		"note":  "Q3 team",
		"count": 5,
	}, tok)
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

// TestAdmin_CreateInvitation_CountTooLarge: counts > 50 are rejected.
func TestAdmin_CreateInvitation_CountTooLarge(t *testing.T) {
	srv, _, _, tok := newAdminTestServer(t)

	w := adminPostBearer(srv, "/admin/api/invitations", map[string]interface{}{"count": 51}, tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for count=51, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdmin_ListInvitations: list endpoint returns all invitations.
func TestAdmin_ListInvitations(t *testing.T) {
	srv, store, _, tok := newAdminTestServer(t)

	// Create two invitations directly in the store.
	ctx := context.Background()
	for _, note := range []string{"alice", "bob"} {
		if _, _, err := store.CreateInvitation(ctx, nil, note); err != nil {
			t.Fatalf("CreateInvitation: %v", err)
		}
	}

	w := adminGetBearer(srv, "/admin/api/invitations", tok)
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

// TestAdmin_CreateInvitation_RequiresAdmin: with a non-admin user's session
// token → 401/403.
func TestAdmin_CreateInvitation_RequiresAdmin(t *testing.T) {
	srv, store, _, _ := newAdminTestServer(t)

	// Create a non-admin user + session token.
	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	nonAdminTok, _, err := store.CreateSession(ctx, u.ID, "test-agent", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	w := adminPostBearer(srv, "/admin/api/invitations", map[string]interface{}{"expires_at": nil, "note": "test"}, nonAdminTok)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("expected 401 or 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdmin_ListUsers: GET /admin/api/users → 200 with user rows.
func TestAdmin_ListUsers(t *testing.T) {
	srv, store, _, tok := newAdminTestServer(t)

	ctx := context.Background()
	if _, err := store.CreateOpaqueUser(ctx, "user1@example.com"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.CreateOpaqueUser(ctx, "user2@example.com"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	w := adminGetBearer(srv, "/admin/api/users", tok)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 3 users: bootstrap admin + the two created above.
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

// TestAdmin_DisableUser_Idempotent: disable the user, verify GetUser sees
// disabled_at set, then disable again — must be a no-op (200 either way).
// The previous version also verified /api/auth/login returns 401 after
// disable, but that route was removed in M1a T12; the OPAQUE login flow's
// disabled-user gating is covered by opaque_auth_test.go.
func TestAdmin_DisableUser_Idempotent(t *testing.T) {
	srv, store, _, adminTok := newAdminTestServer(t)

	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("CreateOpaqueUser: %v", err)
	}

	// Disable via admin endpoint.
	w := adminPostBearer(srv, "/admin/api/users/"+u.ID+"/disable", nil, adminTok)
	if w.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got, err := store.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUser after disable: %v", err)
	}
	if got.DisabledAt == nil {
		t.Errorf("user.DisabledAt = nil after disable; want non-nil")
	}

	// Second disable: still 200.
	w2 := adminPostBearer(srv, "/admin/api/users/"+u.ID+"/disable", nil, adminTok)
	if w2.Code != http.StatusOK {
		t.Errorf("disable (second): expected 200, got %d", w2.Code)
	}
}

// TestAdminAPI_NonAdminUser_Unauthorized: a non-admin user's session
// hitting any /admin/api endpoint must be rejected.
func TestAdminAPI_NonAdminUser_Unauthorized(t *testing.T) {
	srv, store, _, _ := newAdminTestServer(t)

	ctx := context.Background()
	u, err := store.CreateOpaqueUser(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tok, _, err := store.CreateSession(ctx, u.ID, "ua/test", "203.0.113.0/24", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := adminGetBearer(srv, "/admin/api/invitations", tok)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Errorf("non-admin user hitting /admin/api/invitations: status=%d, want 401 or 403", rec.Code)
	}
}

// TestAdminPromoteUser_Success: POST /admin/api/users/{id}/admin promotes a user.
func TestAdminPromoteUser_Success(t *testing.T) {
	srv, store, _, adminTok := newAdminTestServer(t)

	ctx := context.Background()
	target, _ := store.CreateOpaqueUser(ctx, "target@example.com")
	if target.IsAdmin {
		t.Fatal("target already admin")
	}

	rec := adminPostBearer(srv, "/admin/api/users/"+target.ID+"/admin", nil, adminTok)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204; body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUser(ctx, target.ID)
	if !got.IsAdmin {
		t.Error("target user not promoted after POST .../admin")
	}
}

// TestAdminPromoteUser_AuditLog: POST /admin/api/users/{id}/admin writes an audit log.
func TestAdminPromoteUser_AuditLog(t *testing.T) {
	buf := captureDebugLog(t)

	srv, store, actorID, adminTok := newAdminTestServer(t)

	ctx := context.Background()
	target, _ := store.CreateOpaqueUser(ctx, "t@example.com")

	adminPostBearer(srv, "/admin/api/users/"+target.ID+"/admin", nil, adminTok)

	out := buf.String()
	if !strings.Contains(out, "[relay-admin] role change") ||
		!strings.Contains(out, "actor="+actorID) ||
		!strings.Contains(out, "target="+target.ID) ||
		!strings.Contains(out, "op=promote") {
		t.Errorf("audit log line missing or malformed:\n%s", out)
	}
}

// TestAdminDemoteUser_Success: DELETE /admin/api/users/{id}/admin demotes a user.
func TestAdminDemoteUser_Success(t *testing.T) {
	srv, store, _, adminTok := newAdminTestServer(t)

	// Second admin so demoting other doesn't trip last-admin guard.
	ctx := context.Background()
	other, _ := store.CreateOpaqueUser(ctx, "other@example.com")
	_ = store.SetUserAdmin(ctx, other.ID, true)

	rec := adminDeleteBearer(srv, "/admin/api/users/"+other.ID+"/admin", adminTok)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204; body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.GetUser(ctx, other.ID)
	if got.IsAdmin {
		t.Error("user still admin after DELETE .../admin")
	}
}

// TestAdminDemoteUser_Self_400: DELETE /admin/api/users/{id}/admin on self returns 400.
func TestAdminDemoteUser_Self_400(t *testing.T) {
	srv, _, actorID, adminTok := newAdminTestServer(t)

	rec := adminDeleteBearer(srv, "/admin/api/users/"+actorID+"/admin", adminTok)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (self-demote); body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cannot_demote_self") {
		t.Errorf("body = %q; want error code cannot_demote_self", rec.Body.String())
	}
}

// TestAdminDemoteUser_AuditLog: DELETE /admin/api/users/{id}/admin writes an audit log.
func TestAdminDemoteUser_AuditLog(t *testing.T) {
	buf := captureDebugLog(t)

	srv, store, actorID, adminTok := newAdminTestServer(t)

	ctx := context.Background()
	other, _ := store.CreateOpaqueUser(ctx, "other@example.com")
	_ = store.SetUserAdmin(ctx, other.ID, true)

	adminDeleteBearer(srv, "/admin/api/users/"+other.ID+"/admin", adminTok)

	out := buf.String()
	if !strings.Contains(out, "[relay-admin] role change") ||
		!strings.Contains(out, "actor="+actorID) ||
		!strings.Contains(out, "target="+other.ID) ||
		!strings.Contains(out, "op=demote") {
		t.Errorf("audit log line missing or malformed:\n%s", out)
	}
}

// TestCountAdmins_OneTriggersLastAdminGuard: countAdmins returns correct count.
func TestCountAdmins_OneTriggersLastAdminGuard(t *testing.T) {
	ctx := context.Background()
	store, err := userstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, _ := store.CreateOpaqueUser(ctx, "only@example.com")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	n, err := countAdmins(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("countAdmins = %d; want 1", n)
	}
}

// TestAdminListUsers_IncludesIsAdmin: GET /admin/api/users response includes
// is_admin field for each user, correctly reflecting their admin status.
func TestAdminListUsers_IncludesIsAdmin(t *testing.T) {
	srv, store, _, adminTok := newAdminTestServer(t)

	nonAdmin, _ := store.CreateOpaqueUser(context.Background(), "u@example.com")

	rec := adminGetBearer(srv, "/admin/api/users", adminTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	var foundAdmin, foundNonAdmin bool
	for _, r := range rows {
		if r["email"] == "admin@example.com" && r["is_admin"] == true {
			foundAdmin = true
		}
		if r["id"] == nonAdmin.ID && r["is_admin"] == false {
			foundNonAdmin = true
		}
	}
	if !foundAdmin {
		t.Errorf("admin row missing or is_admin=false; rows=%v", rows)
	}
	if !foundNonAdmin {
		t.Errorf("non-admin row missing or is_admin=true; rows=%v", rows)
	}
}
