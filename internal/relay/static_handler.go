package relay

import (
	"io/fs"
	"net/http"
	"strings"
)

// allowedStaticPath reports whether p is a known production-asset path the
// static handler is willing to serve. The whitelist is the exhaustive set of
// files vite-plugin-pwa + the multi-entry MPA build emits at the embed root,
// plus the single-segment wildcards content-hashed assets use.
//
// Anything else (source files like package.json when --web mistakenly points
// at web/, dotfiles like .gitkeep / .npmrc, directory listings, nested
// /assets/ paths, traversals normalised down to root, …) is rejected with
// 404. The whitelist must be updated when build output gains a new
// top-level artifact type.
func allowedStaticPath(p string) bool {
	switch p {
	case "/", "/index.html",
		"/login.html", "/signup.html", "/settings.html", "/firstrun.html",
		"/sw.js", "/manifest.webmanifest",
		"/icon.svg", "/icon.png":
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/assets/"); ok {
		// Exactly one path segment past /assets/.
		return rest != "" && !strings.ContainsAny(rest, "/")
	}
	if strings.HasPrefix(p, "/workbox-") && strings.HasSuffix(p, ".js") {
		// /workbox-<hash>.js at root, no nested directories.
		return !strings.ContainsAny(strings.TrimPrefix(p, "/"), "/")
	}
	return false
}

// newStaticHandler wraps http.FileServer for the embedded web bundle.
//
// Before the session-token migration the relay relied on an HttpOnly cookie
// to know whether a browser was authenticated, and this handler 302'd
// unauthenticated `/` navigations to /login.html. Cookies are gone now and
// browsers cannot attach the localStorage-resident Bearer token to plain
// navigation requests, so the resolver always reports PrincipalNone for
// page loads — the old server-side gate would redirect every successful
// login back to /login.html (the production bug fixed by this change).
//
// Authorization is now a purely client-side concern: every page boots its
// SPA, which reads the session token from localStorage. If the token is
// missing or rejected, apiFetch's 401 interceptor sends the user to
// /login.html. Admin gating works the same way — the admin panel (inline in
// the main App.vue, not a standalone page) queries /api/me, checks is_admin,
// and hides the admin entry point for clients without privileges.
//
// resolver is retained as a parameter for compatibility but is unused; the
// resolver argument may be nil and is ignored at runtime.
func newStaticHandler(resolver *IdentityResolver, webFS fs.FS) http.Handler {
	_ = resolver
	fileSrv := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedStaticPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		fileSrv.ServeHTTP(w, r)
	})
}
