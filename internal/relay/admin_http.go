package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// AdminServer holds the new user-account admin handlers (Task 3.3).
// It is separate from the legacy *Server admin handlers so that neither
// struct has to change in breaking ways. The old handlers stay on *Server
// and use s.authorizeAdmin; the new handlers use requireAdmin (Principal-based).
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

// handleCreateInvite implements POST /admin/api/invitations.
// Body (optional): {"expires_at": <RFC3339 or null>, "note": "<string>"}
// Response 201: {"plaintext": "inv_…", "code_prefix": "inv_xxxx", "note": "…", "expires_at": null}
func (a *AdminServer) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresAt *string `json:"expires_at"`
		Note      string  `json:"note"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck — body is optional
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil && *body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at format, use RFC3339", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	}

	secret, inv, err := a.Store.CreateInvitation(r.Context(), expiresAt, body.Note)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type invResp struct {
		Plaintext  string  `json:"plaintext"`
		CodePrefix string  `json:"code_prefix"`
		Note       string  `json:"note"`
		ExpiresAt  *string `json:"expires_at"`
		CreatedAt  string  `json:"created_at"`
	}
	resp := invResp{
		Plaintext:  secret.Expose(),
		CodePrefix: inv.CodePrefix,
		Note:       inv.Note,
		CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
	}
	if inv.ExpiresAt != nil {
		s := inv.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	writeJSONStatus(w, http.StatusCreated, resp)
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
	RateLimitPerMinute   int                    `json:"rate_limit_per_minute"`
	MaxConnectionsPerKey int                    `json:"max_connections_per_key"`
	ReadOnlyTokens       []adminStoredTokenView `json:"read_only_tokens"`
	Version              string                 `json:"version"`
}

type adminStoredTokenView struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; connect-src 'self'; style-src 'unsafe-inline'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminPageHTML))
}

func (s *Server) handleAdminConfigHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAdmin(r) {
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

func (s *Server) handleAdminReadOnlyTokensHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAdmin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	token, err := generateBearerToken()
	if err != nil {
		http.Error(w, "generate token", http.StatusInternalServerError)
		return
	}
	hash := HashBearerToken(token)
	if err := s.updateAdminConfig(func(cfg AdminConfig) AdminConfig {
		cfg.ReadOnlyTokens = append(cfg.ReadOnlyTokens, StoredToken{
			ID:        id,
			Hash:      hash,
			CreatedAt: time.Now().Unix(),
		})
		return cfg
	}); err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.ReadOnlyTokenHashes = append(s.cfg.ReadOnlyTokenHashes, hash)
	writeJSON(w, struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}{ID: id, Token: token})
}

func (s *Server) handleAdminReadOnlyTokenHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAdmin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/api/read-only-tokens/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "bad token id", http.StatusBadRequest)
		return
	}
	var nextHashes []string
	if err := s.updateAdminConfig(func(cfg AdminConfig) AdminConfig {
		out := cfg.ReadOnlyTokens[:0]
		for _, tok := range cfg.ReadOnlyTokens {
			if tok.ID == id {
				continue
			}
			out = append(out, tok)
			nextHashes = append(nextHashes, tok.Hash)
		}
		cfg.ReadOnlyTokens = out
		return cfg
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.ReadOnlyTokenHashes = nextHashes
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizeAdmin(r *http.Request) bool {
	if s.cfg.AdminToken == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	return tokenEqual(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")), s.cfg.AdminToken)
}

func (s *Server) adminConfigResponse() adminConfigResponse {
	cfg := AdminConfig{}
	if s.cfg.AdminConfigStore != nil {
		cfg = s.cfg.AdminConfigStore.Snapshot()
	}
	if cfg.RateLimitPerMinute == 0 {
		cfg.RateLimitPerMinute = s.cfg.RateLimitPerMinute
	}
	if cfg.MaxConnectionsPerKey == 0 {
		cfg.MaxConnectionsPerKey = s.cfg.MaxConnectionsPerKey
	}
	out := adminConfigResponse{
		RateLimitPerMinute:   cfg.RateLimitPerMinute,
		MaxConnectionsPerKey: cfg.MaxConnectionsPerKey,
		ReadOnlyTokens:       make([]adminStoredTokenView, 0, len(cfg.ReadOnlyTokens)),
		Version:              s.cfg.Version,
	}
	for _, tok := range cfg.ReadOnlyTokens {
		out.ReadOnlyTokens = append(out.ReadOnlyTokens, adminStoredTokenView{ID: tok.ID, CreatedAt: tok.CreatedAt})
	}
	return out
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

func generateBearerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const adminPageHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AT Term Relay Admin</title></head>
<body>
<h1>AT Term Relay Admin</h1>
<p>This page keeps the admin token only in this tab memory.</p>
<input id="token" type="password" placeholder="admin token">
<button id="load">load config</button>
<pre id="out"></pre>
<script>
let adminToken = "";
async function api(path, options = {}) {
  adminToken = document.getElementById("token").value;
  const res = await fetch(path, {
    ...options,
    headers: { "Authorization": "Bearer " + adminToken, "Content-Type": "application/json", ...(options.headers || {}) },
  });
  const text = await res.text();
  if (!res.ok) throw new Error(res.status + " " + text);
  return text ? JSON.parse(text) : null;
}
document.getElementById("load").onclick = async () => {
  try { document.getElementById("out").textContent = JSON.stringify(await api("/admin/api/config"), null, 2); }
  catch (e) { document.getElementById("out").textContent = String(e); }
};
</script>
</body></html>`
