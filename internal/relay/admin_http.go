package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// AdminServer holds the user-account admin handlers. Routes are mounted via
// RegisterInto, which wraps each handler in requireSession (outer, validates
// the session token) and requireAdmin (inner, checks the is_admin flag).
type AdminServer struct {
	Store userstore.Store
}

// requireAdmin gates inner on the request's authenticated user having
// is_admin=true. The session-token check already ran in the outer
// requireSession wrapper, so this only enforces the admin flag.
func (a *AdminServer) requireAdmin(inner http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromContext(r.Context())
		if !ok || u == nil || !u.IsAdmin {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner(w, r)
	})
}

// RegisterInto registers all user-account admin routes into the provided mux.
// The requireSession argument wraps each admin route so the session-token is
// validated before requireAdmin checks the is_admin flag. Pass nil to skip
// the outer wrapper (tests or alternate hosts may apply it elsewhere).
func (a *AdminServer) RegisterInto(mux *http.ServeMux, requireSession func(http.HandlerFunc) http.HandlerFunc) {
	wrap := requireSession
	if wrap == nil {
		wrap = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	// gate composes requireSession + requireAdmin for a single admin handler.
	gate := func(h http.HandlerFunc) http.Handler {
		return wrap(a.requireAdmin(h).ServeHTTP)
	}
	mux.Handle("POST /admin/api/invitations", gate(a.handleCreateInvite))
	mux.Handle("GET /admin/api/invitations", gate(a.handleListInvites))
	mux.Handle("GET /admin/api/users", gate(a.handleListUsers))
	// /admin/api/users/{id}/reset-password was removed alongside the
	// legacy password store methods in M1a T12. Password resets for
	// OPAQUE accounts will be handled out-of-band (re-register via a
	// fresh claim token) and re-introduced under a different route
	// later in M1b.
	mux.Handle("POST /admin/api/users/{id}/disable", gate(a.handleDisableUser))
	mux.Handle("POST /admin/api/users/{id}/admin", gate(a.handlePromoteUser))
	mux.Handle("DELETE /admin/api/users/{id}/admin", gate(a.handleDemoteUser))
}

// defaultInviteExpiry is the lifetime applied to invitations whose request

func (s *Server) applyRuntimeLimits(rateLimit, connLimit int) {
	if rateLimit == 0 {
		rateLimit = defaultRateLimitPerMinute
	}
	if connLimit == 0 {
		connLimit = defaultMaxConnections
	}
	s.cfg.RateLimitPerMinute = rateLimit
	s.cfg.MaxConnectionsPerKey = connLimit
	if rateLimit < 0 {
		s.rate = nil
	} else if s.rate == nil {
		s.rate = newFixedWindowLimiter(rateLimit, time.Minute)
	} else {
		s.rate.setLimit(rateLimit)
	}
	if connLimit < 0 {
		s.conns = nil
	} else if s.conns == nil {
		s.conns = newConnectionLimiter(connLimit)
	} else {
		s.conns.setLimit(connLimit)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
