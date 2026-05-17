package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// fakeWebDir creates a temp directory with placeholder files for / and /admin/
// so http.FileServer has something to serve. Returns the dir.
func fakeWebDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>home</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin", "index.html"), []byte("<html>admin</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "admin", "admin.js"), []byte("/* admin */"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "login.html"), []byte("<html>login</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStaticHandler_AdminGate_AnonymousRedirectsToLogin(t *testing.T) {
	dir := fakeWebDir(t)
	store, _ := userstore.Open(context.Background(), ":memory:")
	defer store.Close()
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, dir)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d; want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login.html" {
		t.Errorf("Location=%q; want /login.html", got)
	}
}

func TestStaticHandler_AdminGate_NonAdminRedirectsToHome(t *testing.T) {
	dir := fakeWebDir(t)
	ctx := context.Background()
	store, _ := userstore.Open(ctx, ":memory:")
	defer store.Close()
	u, _ := store.CreateUser(ctx, "u@example.com", "passphrase-1234")
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, dir)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d; want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/" {
		t.Errorf("Location=%q; want /", got)
	}
}

func TestStaticHandler_AdminGate_AdminServesPage(t *testing.T) {
	dir := fakeWebDir(t)
	ctx := context.Background()
	store, _ := userstore.Open(ctx, ":memory:")
	defer store.Close()
	u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, dir)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "atterm_session", Value: secret.Expose()})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "admin") {
		t.Errorf("body did not contain admin shell HTML; got %q", rec.Body.String())
	}
}

func TestStaticHandler_AdminSubresources_NotGated(t *testing.T) {
	dir := fakeWebDir(t)
	store, _ := userstore.Open(context.Background(), ":memory:")
	defer store.Close()
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, dir)

	// No cookie — anonymous request.
	req := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("admin subresource status=%d; want 200 (static skeleton is fine to serve to anyone; API endpoints are the real auth boundary)", rec.Code)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
