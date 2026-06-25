package relay

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
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

// AdminRoutes returns an http.Handler with all user-account admin endpoints.
// Used by tests that don't want session-token enforcement.
func (a *AdminServer) AdminRoutes() http.Handler {
	mux := http.NewServeMux()
	a.RegisterInto(mux, nil)
	return mux
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
// Response 200: array of {id, email, created_at, disabled_at, is_admin}.
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
		IsAdmin    bool    `json:"is_admin"`
	}

	out := make([]userRow, 0, len(users))
	for _, u := range users {
		row := userRow{
			ID:        u.ID,
			Email:     u.Email,
			CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
			IsAdmin:   u.IsAdmin,
		}
		if u.DisabledAt != nil {
			s := u.DisabledAt.UTC().Format(time.RFC3339)
			row.DisabledAt = &s
		}
		out = append(out, row)
	}
	writeJSONStatus(w, http.StatusOK, out)
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

// handlePromoteUser flips users.is_admin = true for {id}. Idempotent.
// Audit logged with actor (the requesting admin) and target.
func (a *AdminServer) handlePromoteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	var actorID string
	if u, ok := UserFromContext(r.Context()); ok {
		actorID = u.ID
	}
	if err := a.Store.SetUserAdmin(r.Context(), id, true); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("admin role change: actor=%s target=%s op=promote", actorID, id)
	w.WriteHeader(http.StatusNoContent)
}

// countAdmins returns how many users currently have is_admin=1. Used to
// prevent demoting / deleting the last admin and locking the deploy out.
func countAdmins(ctx context.Context, store userstore.Store) (int, error) {
	users, err := store.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u.IsAdmin {
			n++
		}
	}
	return n, nil
}

// handleDemoteUser flips users.is_admin = false for {id}, with two
// guardrails: self-demote (400 cannot_demote_self) and last-admin
// (409 last_admin).
func (a *AdminServer) handleDemoteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	var actorID string
	if u, ok := UserFromContext(r.Context()); ok {
		actorID = u.ID
	}
	if id == actorID {
		writeError(w, http.StatusBadRequest, "cannot_demote_self")
		return
	}
	target, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if target.IsAdmin {
		n, err := countAdmins(r.Context(), a.Store)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if n <= 1 {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
	}
	if err := a.Store.SetUserAdmin(r.Context(), id, false); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("admin role change: actor=%s target=%s op=demote", actorID, id)
	w.WriteHeader(http.StatusNoContent)
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

	// Hot-reloadable verbose logging switches.
	Debug        bool `json:"debug"`
	DebugPayload bool `json:"debug_payload"`

	Version string `json:"version"`
}

// handleAdminConfigHTTP serves GET/PUT /admin/api/config for the admin UI's
// runtime-limits panel.
//
// Auth: the route is wrapped in requireSession at mount time, so the
// request always carries a *userstore.User in its context. This handler
// only needs to enforce the admin flag.
func (s *Server) handleAdminConfigHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil || !u.IsAdmin {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.adminConfigResponse())
	case http.MethodPut:
		var req struct {
			RateLimitPerMinute   int       `json:"rate_limit_per_minute"`
			MaxConnectionsPerKey int       `json:"max_connections_per_key"`
			AllowedOrigins       *[]string `json:"allowed_origins,omitempty"`
			VAPIDSubject         *string   `json:"vapid_subject,omitempty"`
			Debug                *bool     `json:"debug,omitempty"`
			DebugPayload         *bool     `json:"debug_payload,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := s.updateAdminConfig(r.Context(), func(cfg AdminConfig) AdminConfig {
			cfg.RateLimitPerMinute = req.RateLimitPerMinute
			cfg.MaxConnectionsPerKey = req.MaxConnectionsPerKey
			if req.AllowedOrigins != nil {
				cfg.AllowedOrigins = *req.AllowedOrigins
			}
			if req.VAPIDSubject != nil {
				cfg.VAPIDSubject = *req.VAPIDSubject
			}
			if req.Debug != nil {
				cfg.Debug = *req.Debug
			}
			if req.DebugPayload != nil {
				cfg.DebugPayload = *req.DebugPayload
			}
			return cfg
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.applyRuntimeLimits(req.RateLimitPerMinute, req.MaxConnectionsPerKey)
		// Origins + debug hot-apply immediately; VAPID subject is persisted but
		// only takes effect on restart (webpush.Open consumes it once).
		if req.AllowedOrigins != nil {
			s.SetAllowedOrigins(OriginPatterns(*req.AllowedOrigins))
		}
		if req.Debug != nil || req.DebugPayload != nil {
			cur := s.cfg.AdminConfigStore.Snapshot()
			s.SetDebug(cur.Debug, cur.DebugPayload)
		}
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
		Debug:                       s.debugOn(),
		DebugPayload:                s.debugPayloadOn(),
		Version:                     s.cfg.Version,
	}
}

func (s *Server) updateAdminConfig(ctx context.Context, update func(AdminConfig) AdminConfig) error {
	if s.cfg.AdminConfigStore == nil {
		return errors.New("admin config path is not configured")
	}
	cfg := s.cfg.AdminConfigStore.Snapshot()
	cfg = update(cfg)
	return s.cfg.AdminConfigStore.Set(ctx, cfg)
}

// feishuAdminResponse is the masked Feishu integration view returned to the
// admin UI. The plaintext encrypt key is NEVER included — only whether one is
// set and its last 4 chars for recognition.
type feishuAdminResponse struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	BaseURL  string `json:"base_url"`
	KeySet   bool   `json:"key_set"`
	KeyLast4 string `json:"key_last4,omitempty"`
}

func (s *Server) feishuAdminResponse() feishuAdminResponse {
	cfg := AdminConfig{}
	if s.cfg.AdminConfigStore != nil {
		cfg = s.cfg.AdminConfigStore.Snapshot()
	}
	resp := feishuAdminResponse{
		Enabled: cfg.FeishuEnabled,
		Running: s.FeishuEnabled(),
		BaseURL: cfg.FeishuBaseURL,
		KeySet:  cfg.FeishuEncryptKey != "",
	}
	if n := len(cfg.FeishuEncryptKey); n >= 4 {
		resp.KeyLast4 = cfg.FeishuEncryptKey[n-4:]
	}
	return resp
}

// handleAdminFeishuHTTP serves GET/PUT /admin/api/feishu — the admin UI's
// Feishu integration panel. PUT persists to the DB (relay_config) and
// hot-applies the enable/disable transition (attach/detach the secret
// cipher + handler) with no restart.
func (s *Server) handleAdminFeishuHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil || !u.IsAdmin {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.feishuAdminResponse())
	case http.MethodPut:
		var req struct {
			Enabled    bool   `json:"enabled"`
			EncryptKey string `json:"encrypt_key"`
			BaseURL    string `json:"base_url"`
			Force      bool   `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if s.cfg.AdminConfigStore == nil {
			http.Error(w, "admin config path is not configured", http.StatusInternalServerError)
			return
		}
		cur := s.cfg.AdminConfigStore.Snapshot()
		baseURL := strings.TrimSpace(req.BaseURL)
		// Keep the existing key when the client doesn't resend one, so a plain
		// enable/disable toggle needn't carry the secret.
		newKey := strings.TrimSpace(req.EncryptKey)
		effectiveKey := cur.FeishuEncryptKey
		if newKey != "" {
			// Rotation guard: a different key orphans existing encrypted rows.
			if cur.FeishuEncryptKey != "" && cur.FeishuEncryptKey != newKey && !req.Force {
				http.Error(w, "changing the encrypt key makes existing Feishu bindings undecryptable; resend with force=true to proceed", http.StatusConflict)
				return
			}
			effectiveKey = newKey
		}
		if req.Enabled && effectiveKey == "" {
			http.Error(w, "encrypt_key required to enable Feishu", http.StatusBadRequest)
			return
		}
		var keyBytes []byte
		if effectiveKey != "" {
			b, err := AdminConfig{FeishuEncryptKey: effectiveKey}.DecodeFeishuKey()
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			keyBytes = b
		}
		if err := s.updateAdminConfig(r.Context(), func(cfg AdminConfig) AdminConfig {
			cfg.FeishuEnabled = req.Enabled
			cfg.FeishuEncryptKey = effectiveKey
			cfg.FeishuBaseURL = baseURL
			return cfg
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.ApplyFeishuConfig(req.Enabled, keyBytes, baseURL); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.feishuAdminResponse())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAdminFeishuGenerateKey returns a fresh base64-encoded 32-byte key for
// the UI's "generate" button. It does NOT persist — the client PUTs it back.
func (s *Server) handleAdminFeishuGenerateKey(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFromContext(r.Context())
	if !ok || u == nil || !u.IsAdmin {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		http.Error(w, "rng failure", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"encrypt_key": base64.StdEncoding.EncodeToString(buf)})
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
