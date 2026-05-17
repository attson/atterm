package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// AdminServer holds the user-account admin handlers (Task 3.3).
// Handlers use requireAdmin middleware for Principal-based auth.
type AdminServer struct {
	Store    userstore.Store
	Resolver *IdentityResolver
}

// requireAdmin returns an http.Handler that allows only PrincipalAdmin requests.
// It uses IdentityResolver.Resolve so all token sources (Bearer header) work.
func (a *AdminServer) requireAdmin(inner http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := a.Resolver.Resolve(r)
		if p.Kind != PrincipalAdmin {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner(w, r)
	})
}

// AdminRoutes returns an http.Handler with all user-account admin endpoints.
func (a *AdminServer) AdminRoutes() http.Handler {
	mux := http.NewServeMux()
	a.RegisterInto(mux)
	return mux
}

// RegisterInto registers all user-account admin routes into the provided mux.
// Called by AdminRoutes() and by BuildMux so the production mux is assembled
// from the same set of routes.
func (a *AdminServer) RegisterInto(mux *http.ServeMux) {
	mux.Handle("POST /admin/api/invitations", a.requireAdmin(a.handleCreateInvite))
	mux.Handle("GET /admin/api/invitations", a.requireAdmin(a.handleListInvites))
	mux.Handle("GET /admin/api/users", a.requireAdmin(a.handleListUsers))
	mux.Handle("POST /admin/api/users/{id}/reset-password", a.requireAdmin(a.handleResetPassword))
	mux.Handle("POST /admin/api/users/{id}/disable", a.requireAdmin(a.handleDisableUser))
}

// defaultInviteExpiry is the lifetime applied to invitations whose request
// body omits expires_at. 7 days balances "convenient for share-with-a-colleague"
// against "stale invite codes pile up" — bind-and-forget is a security smell.
const defaultInviteExpiry = 7 * 24 * time.Hour

// handleCreateInvite implements POST /admin/api/invitations.
// Body (optional):
//
//	{
//	  "expires_at": <RFC3339 or null or "" → defaults to now + 7 days>,
//	  "note":       "<string>",
//	  "count":      <int, default 1, clamped to [1, 50]> — bulk-create N invites
//	                with the same note/expiry; each gets a distinct plaintext.
//	}
//
// Response 201:
//
//	count == 1: {"plaintext": "inv_…", "code_prefix": "...", "note": "...",
//	             "expires_at": "...", "created_at": "..."}
//	count >  1: {"invites": [<the same shape>, ...]}
func (a *AdminServer) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresAt *string `json:"expires_at"`
		Note      string  `json:"note"`
		Count     int     `json:"count"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck — body is optional
	}

	count := body.Count
	if count <= 0 {
		count = 1
	}
	if count > 50 {
		http.Error(w, "count exceeds maximum (50)", http.StatusBadRequest)
		return
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil && *body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at format, use RFC3339", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	} else {
		t := time.Now().Add(defaultInviteExpiry)
		expiresAt = &t
	}

	type invResp struct {
		Plaintext  string  `json:"plaintext"`
		CodePrefix string  `json:"code_prefix"`
		Note       string  `json:"note"`
		ExpiresAt  *string `json:"expires_at"`
		CreatedAt  string  `json:"created_at"`
	}

	invites := make([]invResp, 0, count)
	for i := 0; i < count; i++ {
		secret, inv, err := a.Store.CreateInvitation(r.Context(), expiresAt, body.Note)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		row := invResp{
			Plaintext:  secret.Expose(),
			CodePrefix: inv.CodePrefix,
			Note:       inv.Note,
			CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
		}
		if inv.ExpiresAt != nil {
			s := inv.ExpiresAt.UTC().Format(time.RFC3339)
			row.ExpiresAt = &s
		}
		invites = append(invites, row)
	}

	if count == 1 {
		writeJSONStatus(w, http.StatusCreated, invites[0])
		return
	}
	writeJSONStatus(w, http.StatusCreated, struct {
		Invites []invResp `json:"invites"`
	}{Invites: invites})
}

// handleListInvites implements GET /admin/api/invitations.
// Response 200: array of invitation rows (no plaintext).
func (a *AdminServer) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invs, err := a.Store.ListInvitations(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type invRow struct {
		CodePrefix string  `json:"code_prefix"`
		Note       string  `json:"note"`
		CreatedAt  string  `json:"created_at"`
		ExpiresAt  *string `json:"expires_at"`
		ConsumedAt *string `json:"consumed_at"`
		ConsumedBy string  `json:"consumed_by,omitempty"`
	}

	out := make([]invRow, 0, len(invs))
	for _, inv := range invs {
		row := invRow{
			CodePrefix: inv.CodePrefix,
			Note:       inv.Note,
			CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
			ConsumedBy: inv.ConsumedBy,
		}
		if inv.ExpiresAt != nil {
			s := inv.ExpiresAt.UTC().Format(time.RFC3339)
			row.ExpiresAt = &s
		}
		if inv.ConsumedAt != nil {
			s := inv.ConsumedAt.UTC().Format(time.RFC3339)
			row.ConsumedAt = &s
		}
		out = append(out, row)
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleListUsers implements GET /admin/api/users.
// Response 200: array of {id, email, created_at, disabled_at}.
func (a *AdminServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type userRow struct {
		ID         string  `json:"id"`
		Email      string  `json:"email"`
		CreatedAt  string  `json:"created_at"`
		DisabledAt *string `json:"disabled_at,omitempty"`
	}

	out := make([]userRow, 0, len(users))
	for _, u := range users {
		row := userRow{
			ID:        u.ID,
			Email:     u.Email,
			CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
		}
		if u.DisabledAt != nil {
			s := u.DisabledAt.UTC().Format(time.RFC3339)
			row.DisabledAt = &s
		}
		out = append(out, row)
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleResetPassword implements POST /admin/api/users/{id}/reset-password.
// Response 200: {"plaintext": "tmp_…"}
// Atomically: generates tmp password, updates hash + csrf_secret, deletes web_sessions.
func (a *AdminServer) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}

	secret, err := a.Store.ResetUserPassword(r.Context(), userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]string{
		"plaintext": secret.Expose(),
	})
}

// handleDisableUser implements POST /admin/api/users/{id}/disable.
// Response 200: {"status": "disabled"}
func (a *AdminServer) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}

	if err := a.Store.DisableUser(r.Context(), userID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]string{"status": "disabled"})
}

type adminConfigResponse struct {
	// Raw stored values. 0 means "use built-in default"; negative means
	// "disable the limit entirely". UI clients must interpret 0 via the
	// Default* fields below.
	RateLimitPerMinute   int `json:"rate_limit_per_minute"`
	MaxConnectionsPerKey int `json:"max_connections_per_key"`

	// Built-in defaults, exposed so the UI can show "0 = use default (<N>)"
	// instead of misleadingly displaying a literal 0.
	DefaultRateLimitPerMinute   int `json:"default_rate_limit_per_minute"`
	DefaultMaxConnectionsPerKey int `json:"default_max_connections_per_key"`

	Version string `json:"version"`
}

// handleAdminConfigHTTP serves GET/PUT /admin/api/config for the admin UI's
// runtime-limits panel.
//
// Auth: requires PrincipalAdmin (cookie session on a user with is_admin=true,
// or admin API token). When cfg.Resolver is nil (legacy/test setups with no
// userstore) the endpoint returns 401 — there is no fallback to a shared
// admin token. CSRF protection for PUT is layered on at the mux level via
// RequireCSRF; this handler does not re-check CSRF.
func (s *Server) handleAdminConfigHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Resolver == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	p := s.cfg.Resolver.Resolve(r)
	if p.Kind != PrincipalAdmin {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.adminConfigResponse())
	case http.MethodPut:
		var req struct {
			RateLimitPerMinute   int `json:"rate_limit_per_minute"`
			MaxConnectionsPerKey int `json:"max_connections_per_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := s.updateAdminConfig(func(cfg AdminConfig) AdminConfig {
			cfg.RateLimitPerMinute = req.RateLimitPerMinute
			cfg.MaxConnectionsPerKey = req.MaxConnectionsPerKey
			return cfg
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.applyRuntimeLimits(req.RateLimitPerMinute, req.MaxConnectionsPerKey)
		writeJSON(w, s.adminConfigResponse())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) adminConfigResponse() adminConfigResponse {
	cfg := AdminConfig{}
	if s.cfg.AdminConfigStore != nil {
		cfg = s.cfg.AdminConfigStore.Snapshot()
	}
	// Expose stored values as-is so the UI can distinguish "unset (= default)"
	// from a literal numeric override. Defaults travel alongside for display.
	return adminConfigResponse{
		RateLimitPerMinute:          cfg.RateLimitPerMinute,
		MaxConnectionsPerKey:        cfg.MaxConnectionsPerKey,
		DefaultRateLimitPerMinute:   defaultRateLimitPerMinute,
		DefaultMaxConnectionsPerKey: defaultMaxConnections,
		Version:                     s.cfg.Version,
	}
}

func (s *Server) updateAdminConfig(update func(AdminConfig) AdminConfig) error {
	if s.cfg.AdminConfigStore == nil {
		return errors.New("admin config path is not configured")
	}
	cfg := s.cfg.AdminConfigStore.Snapshot()
	cfg = update(cfg)
	return s.cfg.AdminConfigStore.Set(cfg)
}

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

