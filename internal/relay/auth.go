package relay

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/userstore"
)

type userCtxKey struct{}
type sessionCtxKey struct{}

const (
	sessionActivityWriteInterval = 15 * time.Second
	sessionHeartbeatInterval     = 30 * time.Second
)

// UserFromContext returns the user attached by requireSession middleware.
func UserFromContext(ctx context.Context) (*userstore.User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(*userstore.User)
	return u, ok && u != nil
}

// SessionFromContext returns the authenticated login session attached by
// requireSession. The session only contains the already-hashed credential ID;
// plaintext tokens never enter request context beyond authentication.
func SessionFromContext(ctx context.Context) (*userstore.Session, bool) {
	sess, ok := ctx.Value(sessionCtxKey{}).(*userstore.Session)
	return sess, ok && sess != nil
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
// in the store, and injects the owning user and login session into the request
// context.
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
		sess, user, err := s.cfg.Store.LookupSession(r.Context(), tok)
		if err != nil || user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Avoid turning every authenticated HTTP request into a DB write while
		// still making first activity visible immediately. Long-lived WebSocket
		// requests get a periodic touch below so an idle but connected device
		// remains online in Settings across relay instances.
		now := time.Now()
		if now.Sub(sess.LastSeenAt) >= sessionActivityWriteInterval {
			if _, err := s.cfg.Store.TouchSession(r.Context(), sess.IDHash, now); err != nil {
				logging.Warn("relay-auth", "touch session on request: %v", err)
			}
		}

		ctx := context.WithValue(r.Context(), userCtxKey{}, user)
		ctx = context.WithValue(ctx, sessionCtxKey{}, sess)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if isWebSocketUpgrade(r) {
			go s.keepSessionActive(ctx, cancel, sess.IDHash)
		}
		h(w, r.WithContext(ctx))
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

// keepSessionActive persists a coarse presence heartbeat. A missing row means
// the token was revoked (or expired), so cancel the request context to tear
// down the authenticated WebSocket instead of letting it outlive revocation.
func (s *Server) keepSessionActive(ctx context.Context, cancel context.CancelFunc, idHash string) {
	ticker := time.NewTicker(sessionHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			active, err := s.cfg.Store.TouchSession(ctx, idHash, at)
			if err != nil {
				logging.Warn("relay-auth", "touch session heartbeat: %v", err)
				continue
			}
			if !active {
				cancel()
				return
			}
		}
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
