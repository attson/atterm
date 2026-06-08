package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// CSRFToken derives the per-session CSRF token used by both the server
// and the client. Defined publicly so the /api/me handler can return it
// to the frontend.
func CSRFToken(cookieValue string, csrfSecret []byte) string {
	h := sha256.Sum256(append([]byte(cookieValue), csrfSecret...))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// RequireCSRF gates mutating routes. For GET/HEAD/OPTIONS it is a no-op. For
// all other methods, it requires a non-empty cookie session AND a matching
// X-CSRF-Token header.
func RequireCSRF(resolver *IdentityResolver, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			inner.ServeHTTP(w, r)
			return
		}
		// Bearer tokens skip CSRF on EVERY route this middleware guards
		// (including account-management: password change, token mint/revoke,
		// account delete, web-session sign-out, admin config). Rationale:
		// Bearer is an out-of-band credential — not auto-attached by
		// browsers, so CSRF adds no protection. A stolen API token has the
		// same authority as a cookie session by design; treat token-theft
		// posture accordingly. Auth is still enforced by inner handlers via
		// Resolver.Resolve / Principal.IsUser.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			inner.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("atterm_session")
		if err != nil || c.Value == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		p := resolver.Resolve(r)
		if !p.IsUser() || len(p.CSRFSecret) == 0 {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		want := CSRFToken(c.Value, p.CSRFSecret)
		got := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "csrf mismatch", http.StatusForbidden)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
