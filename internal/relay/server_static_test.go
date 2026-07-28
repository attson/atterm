package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// staticHandlerFS is the exhaustive map of paths that vite + vite-plugin-pwa
// emit. Plus a handful of non-allowed neighbours that an operator might
// expose by accidentally pointing --web at the source tree.
func staticHandlerFS(t *testing.T) http.Handler {
	t.Helper()
	files := fstest.MapFS{
		// Allowed production assets.
		"index.html":                {Data: []byte("<root>")},
		"login.html":                {Data: []byte("<login>")},
		"signup.html":               {Data: []byte("<signup>")},
		"settings.html":             {Data: []byte("<settings>")},
		"sw.js":                     {Data: []byte("// sw")},
		"workbox-0bb07689.js":       {Data: []byte("// workbox")},
		"manifest.webmanifest":      {Data: []byte("{}")},
		"icon.svg":                  {Data: []byte("<svg/>")},
		"icon.png":                  {Data: []byte("\x89PNG")},
		"assets/login-Drb6zxei.js":  {Data: []byte("// login bundle")},
		"assets/shell-WYJzjxIi.css": {Data: []byte("/* shell */")},

		// Not allowed — these would be exposed if --web pointed at the
		// source tree without the whitelist.
		"package.json":                       {Data: []byte("{}")},
		"tsconfig.json":                      {Data: []byte("{}")},
		".npmrc":                             {Data: []byte("// secrets")},
		"vite.config.ts":                     {Data: []byte("// config")},
		"src/main/App.vue":                   {Data: []byte("<template/>")},
		"tests/contract/auth-pages.test.mjs": {Data: []byte("// test")},
		"legacy/manifest.webmanifest":        {Data: []byte("{}")},
		"assets/sub/dir/nested.js":           {Data: []byte("// nested")},

		// .gitkeep is in the embed root but is not a real asset.
		".gitkeep": {Data: []byte("")},
	}
	return newStaticHandler(nil, files)
}

func TestStaticHandler_AllowsProductionAssets(t *testing.T) {
	h := staticHandlerFS(t)
	allowed := []string{
		"/", "/index.html", "/login.html", "/signup.html", "/settings.html",
		"/sw.js", "/workbox-0bb07689.js",
		"/manifest.webmanifest",
		"/icon.svg", "/icon.png",
		"/assets/login-Drb6zxei.js", "/assets/shell-WYJzjxIi.css",
	}
	for _, p := range allowed {
		t.Run("GET "+p, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			h.ServeHTTP(rr, req)
			// http.FileServer redirects /foo/index.html → /foo/ on its own;
			// either 200 (direct serve) or 301 (canonical-form redirect) is
			// acceptable here — the security-relevant assertion is that the
			// whitelist did not 404 the path.
			if rr.Code != http.StatusOK && rr.Code != http.StatusMovedPermanently {
				t.Fatalf("expected 200 or 301 for %q, got %d", p, rr.Code)
			}
		})
	}
}

func TestStaticHandler_BlocksSourceFiles(t *testing.T) {
	h := staticHandlerFS(t)
	blocked := []string{
		"/package.json",
		"/tsconfig.json",
		"/.npmrc",
		"/vite.config.ts",
		"/src/",
		"/src/main/App.vue",
		"/tests/",
		"/tests/contract/auth-pages.test.mjs",
		"/legacy/",
		"/legacy/manifest.webmanifest",
		"/.gitkeep",
		// admin.html was dropped from the build (admin is now an inline
		// AdminPanel view in the main App.vue bundle) — the standalone
		// /admin/ path must no longer be whitelisted.
		"/admin/",
		"/admin/index.html",
	}
	for _, p := range blocked {
		t.Run("GET "+p, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for %q, got %d (body=%q)", p, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestStaticHandler_BlocksNestedAssets(t *testing.T) {
	h := staticHandlerFS(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/sub/dir/nested.js", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nested assets path, got %d", rr.Code)
	}
}

func TestStaticHandler_NoDirectoryListing(t *testing.T) {
	h := staticHandlerFS(t)
	// None of these directories are on the whitelist, so the handler must
	// 404 them outright rather than let FileServer emit a directory listing.
	for _, p := range []string{"/src/", "/tests/", "/legacy/", "/assets/"} {
		t.Run("GET "+p, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p, nil)
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				t.Fatalf("expected non-200 for directory %q, got 200 body=%q", p, rr.Body.String())
			}
		})
	}
}

func TestStaticHandler_PathTraversalNormalized(t *testing.T) {
	// Go's net/http normalizes /assets/../package.json to /package.json
	// before our handler sees it. Verify the normalized path is rejected.
	h := staticHandlerFS(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/../package.json", nil)
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 for path traversal, got 200 body=%q", rr.Body.String())
	}
}
