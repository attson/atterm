package relay

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authorize accepts either an Authorization: Bearer <token> header or a
// ?token=<token> query parameter. The query form exists because browser
// WebSocket clients cannot set custom headers cross-origin.
func authorize(r *http.Request, expected string) bool {
	if expected == "" {
		return true // dev mode: no token configured
	}
	got := tokenFromRequest(r)
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q
	}
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		// allow browsers that pass token via the subprotocol header
		for _, part := range strings.Split(p, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "atterm-token.") {
				return strings.TrimPrefix(part, "atterm-token.")
			}
		}
	}
	return ""
}
