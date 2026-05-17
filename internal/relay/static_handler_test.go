package relay

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/attson/atterm/internal/userstore"
)

// fakeWebFS returns an in-memory fs.FS that mimics what web-dist/
// holds: an index.html, a login.html, an admin/index.html, plus one
// admin subresource so the "ungated subresources" test has a target.
func fakeWebFS(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"index.html":       {Data: []byte("<html>home</html>")},
		"admin/index.html": {Data: []byte("<html>admin</html>")},
		"admin/admin.js":   {Data: []byte("/* admin */")},
		"login.html":       {Data: []byte("<html>login</html>")},
	}
}

func TestStaticHandler_AdminGate_AnonymousRedirectsToLogin(t *testing.T) {
	fsys := fakeWebFS(t)
	store, _ := userstore.Open(context.Background(), ":memory:")
	defer store.Close()
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, fsys)

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
	fsys := fakeWebFS(t)
	ctx := context.Background()
	store, _ := userstore.Open(ctx, ":memory:")
	defer store.Close()
	u, _ := store.CreateUser(ctx, "u@example.com", "passphrase-1234")
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, fsys)

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
	fsys := fakeWebFS(t)
	ctx := context.Background()
	store, _ := userstore.Open(ctx, ":memory:")
	defer store.Close()
	u, _ := store.CreateUser(ctx, "a@example.com", "passphrase-1234")
	_ = store.SetUserAdmin(ctx, u.ID, true)
	secret, _ := store.CreateWebSession(ctx, u.ID, "ua", "")
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, fsys)

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
	fsys := fakeWebFS(t)
	store, _ := userstore.Open(context.Background(), ":memory:")
	defer store.Close()
	resolver := NewIdentityResolver(store)
	handler := newStaticHandler(resolver, fsys)

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
