package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
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
		c, err := r.Cookie("atterm_session")
		if err != nil || c.Value == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		p := resolver.Resolve(r)
		if (p.Kind != PrincipalUser && p.Kind != PrincipalAdmin) || len(p.CSRFSecret) == 0 {
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
