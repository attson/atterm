package relay

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// fakeWebFS returns an in-memory fs.FS that mimics what web-dist/
// holds: an index.html, a login.html, plus one /assets/ bundle so the
// "ungated bundle" test has a target.
func fakeWebFS(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"index.html":           {Data: []byte("<html>home</html>")},
		"assets/admin-fake.js": {Data: []byte("/* admin */")},
		"login.html":           {Data: []byte("<html>login</html>")},
	}
}

// Server-side auth gating on static paths was removed because browsers can
// not attach the localStorage-resident Bearer token to plain navigation
// requests. The SPA now reads localStorage on boot and apiFetch's 401
// interceptor handles unauthenticated states. The tests below assert
// that every page is served unconditionally; the only failure mode is
// "path not allowed".

func TestStaticHandler_RootServesUnconditionally(t *testing.T) {
	fsys := fakeWebFS(t)
	handler := newStaticHandler(nil, fsys)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d; want 200 (root must serve index.html so the SPA can read localStorage)", rec.Code)
	}
	if !contains(rec.Body.String(), "home") {
		t.Errorf("body did not contain home shell HTML; got %q", rec.Body.String())
	}
}

// TestStaticHandler_AdminPathNotWhitelisted guards against re-adding
// /admin/ to the static whitelist. The standalone admin.html MPA entry was
// dropped — admin is now an inline AdminPanel view served as part of the
// main index.html bundle, gated client-side on /api/me.is_admin — so the
// bare /admin/ path must 404 rather than serve a (nonexistent) shell.
func TestStaticHandler_AdminPathNotWhitelisted(t *testing.T) {
	fsys := fakeWebFS(t)
	handler := newStaticHandler(nil, fsys)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d; want 404 (admin.html was dropped; admin is inline in the main App.vue bundle now)", rec.Code)
	}
}

// TestStaticHandler_DisallowedPathReturns404 — the path allow-list is the
// last server-side guard; anything off the list 404s.
func TestStaticHandler_DisallowedPathReturns404(t *testing.T) {
	fsys := fakeWebFS(t)
	handler := newStaticHandler(nil, fsys)

	req := httptest.NewRequest(http.MethodGet, "/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d; want 404 for disallowed path", rec.Code)
	}
}

func TestStaticHandler_AssetsBundle_NotGated(t *testing.T) {
	fsys := fakeWebFS(t)
	handler := newStaticHandler(nil, fsys)

	// Post-Vue-rewrite, admin code is part of the shared /assets/<hash>.js
	// bundle, not under /admin/. Anonymous access is fine — API endpoints
	// are the real auth boundary.
	req := httptest.NewRequest(http.MethodGet, "/assets/admin-fake.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("assets bundle status=%d; want 200 (static skeleton is fine to serve to anyone; API endpoints are the real auth boundary)", rec.Code)
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
