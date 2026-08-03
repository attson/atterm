package relay

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/userstore"
)

type userCtxKey struct{}

// UserFromContext returns the user attached by requireSession middleware.
func UserFromContext(ctx context.Context) (*userstore.User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(*userstore.User)
	return u, ok && u != nil
}

// authScope classifies the level of access a caller has on attached sessions.
// In the post-share-secret world it is always authWrite for any authenticated
// principal; the type and constants survive because permissions.go and
// client_conn.go still pattern-match on them. A future cleanup may fold these
// uses into a per-session "remote permission" check.
type authScope uint8

const (
	_ authScope = iota
	authRead
	authWrite
)

// requireSession extracts a session token from the request, looks it up
// in the store, and injects the owning user into the request context.
// On miss, expired, or revoked: writes 401 and aborts. When the server was
// constructed without a userstore (Store == nil), every request is 401 —
// that surface is permanently un-authable until the store is wired up.
func (s *Server) requireSession(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Store == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok := tokenFromRequest(r)
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, user, err := s.cfg.Store.LookupSession(r.Context(), tok)
		if err != nil || user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, user)))
	}
}

// tokenFromRequest accepts a session token via either:
//  1. Authorization: Bearer <token>
//  2. Sec-WebSocket-Protocol: atterm-token.<token>
//  3. Sec-WebSocket-Protocol: atterm-token-b64.<base64url(token)>
//
// URL query tokens are intentionally rejected.
func tokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return tokenFromSubprotocol(r)
}

func tokenFromSubprotocol(r *http.Request) string {
	p := r.Header.Get("Sec-WebSocket-Protocol")
	if p == "" {
		return ""
	}
	for _, part := range strings.Split(p, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "atterm-token.") {
			return strings.TrimPrefix(part, "atterm-token.")
		}
		if strings.HasPrefix(part, "atterm-token-b64.") {
			decoded, err := base64.RawURLEncoding.DecodeString(
				strings.TrimPrefix(part, "atterm-token-b64."))
			if err == nil {
				return string(decoded)
			}
		}
	}
	return ""
}
