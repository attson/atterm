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
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>AT Term Relay Admin</title>
<style>
  * { box-sizing: border-box; }
  body { font: 14px/1.4 system-ui, sans-serif; max-width: 960px; margin: 1rem auto; padding: 0 1rem; color: #1a1a1a; }
  h1 { margin: 0 0 0.5rem; font-size: 1.25rem; }
  h2 { font-size: 1rem; margin: 1.5rem 0 0.5rem; }
  .auth { display: flex; gap: 0.5rem; margin-bottom: 0.5rem; }
  .auth input { flex: 1; padding: 0.5rem; font: inherit; border: 1px solid #ccc; border-radius: 4px; }
  .muted { color: #888; font-size: 12px; margin: 0 0 1rem; }
  .tabs { display: flex; gap: 0.25rem; border-bottom: 1px solid #ccc; margin-bottom: 1rem; }
  .tabs button { padding: 0.5rem 1rem; border: none; background: none; border-bottom: 2px solid transparent; cursor: pointer; font: inherit; }
  .tabs button.active { border-bottom-color: #2563eb; font-weight: 600; color: #2563eb; }
  .panel { display: none; }
  .panel.active { display: block; }
  .row { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  input, button, select { font: inherit; padding: 0.4rem 0.6rem; border: 1px solid #ccc; border-radius: 4px; background: white; }
  button { cursor: pointer; }
  button.primary { background: #2563eb; color: white; border-color: #2563eb; }
  button.danger { background: #dc2626; color: white; border-color: #dc2626; }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  table { width: 100%; border-collapse: collapse; margin-top: 0.25rem; font-size: 13px; }
  th, td { text-align: left; padding: 0.45rem 0.5rem; border-bottom: 1px solid #eee; vertical-align: top; }
  th { background: #f5f5f5; font-weight: 600; }
  td code { font-size: 11px; color: #555; }
  .secret { background: #fef3c7; border: 1px solid #f59e0b; padding: 0.75rem; border-radius: 4px; margin: 0.75rem 0; }
  .secret code { display: block; background: white; padding: 0.4rem 0.5rem; border-radius: 3px; user-select: all; word-break: break-all; margin: 0.4rem 0; font-size: 13px; }
  .secret .warn { font-size: 12px; color: #92400e; }
  .err { color: #dc2626; margin: 0.5rem 0; min-height: 1.2em; }
  pre { background: #f5f5f5; padding: 0.5rem; border-radius: 4px; overflow-x: auto; }
</style>
</head>
<body>
<h1>AT Term Relay Admin</h1>
<div class="auth">
  <input id="token" type="password" placeholder="ATTERM_ADMIN_TOKEN" autocomplete="off">
</div>
<p class="muted">Token kept in tab memory only; never persisted. Refresh = re-enter.</p>

<div class="tabs">
  <button data-tab="invites" class="active">Invitations</button>
  <button data-tab="users">Users</button>
  <button data-tab="config">Config</button>
</div>

<div id="invites" class="panel active">
  <h2>Create invitation</h2>
  <div class="row">
    <input id="inv-note" placeholder="note (optional)" style="flex:1;min-width:180px">
    <input id="inv-expires" type="datetime-local" title="expires at (optional)">
    <button id="inv-create" class="primary">Create</button>
  </div>
  <div id="inv-secret"></div>
  <div id="inv-err" class="err"></div>
  <h2>All invitations <button id="inv-refresh">refresh</button></h2>
  <table id="inv-table"><thead><tr><th>Prefix</th><th>Note</th><th>Created</th><th>Expires</th><th>Consumed</th><th>By</th></tr></thead><tbody></tbody></table>
</div>

<div id="users" class="panel">
  <h2>All users <button id="users-refresh">refresh</button></h2>
  <div id="users-secret"></div>
  <table id="users-table"><thead><tr><th>Email</th><th>ID</th><th>Created</th><th>Status</th><th>Actions</th></tr></thead><tbody></tbody></table>
  <div id="users-err" class="err"></div>
</div>

<div id="config" class="panel">
  <h2>Relay config <button id="config-load" class="primary">load</button></h2>
  <pre id="config-out">(not loaded)</pre>
  <div id="config-err" class="err"></div>
</div>

<script>
const $ = function(id) { return document.getElementById(id); };
const fmt = function(s) { return s ? new Date(s).toLocaleString() : ""; };

async function api(path, opts) {
  opts = opts || {};
  const token = $("token").value.trim();
  if (!token) throw new Error("enter admin token first");
  const headers = Object.assign({"Authorization": "Bearer " + token}, opts.headers || {});
  if (opts.body) headers["Content-Type"] = "application/json";
  const res = await fetch(path, Object.assign({}, opts, {headers: headers}));
  const text = await res.text();
  if (!res.ok) throw new Error(res.status + " " + (text || res.statusText));
  return text ? JSON.parse(text) : null;
}

function showSecret(containerID, label, plaintext) {
  const el = $(containerID);
  el.innerHTML = "";
  const wrap = document.createElement("div");
  wrap.className = "secret";
  const head = document.createElement("strong");
  head.textContent = label;
  wrap.appendChild(head);
  const code = document.createElement("code");
  code.textContent = plaintext;
  wrap.appendChild(code);
  const warn = document.createElement("div");
  warn.className = "warn";
  warn.textContent = "Copy now — this plaintext is shown only once.";
  wrap.appendChild(warn);
  const copyBtn = document.createElement("button");
  copyBtn.textContent = "Copy";
  copyBtn.onclick = function() {
    navigator.clipboard.writeText(plaintext).then(function() { copyBtn.textContent = "Copied"; });
  };
  const dismissBtn = document.createElement("button");
  dismissBtn.textContent = "Dismiss";
  dismissBtn.onclick = function() { el.innerHTML = ""; };
  wrap.appendChild(copyBtn);
  wrap.appendChild(document.createTextNode(" "));
  wrap.appendChild(dismissBtn);
  el.appendChild(wrap);
}

function setErr(id, msg) { $(id).textContent = msg ? String(msg.message || msg) : ""; }

// Tabs
document.querySelectorAll(".tabs button").forEach(function(b) {
  b.onclick = function() {
    document.querySelectorAll(".tabs button").forEach(function(x) { x.classList.remove("active"); });
    document.querySelectorAll(".panel").forEach(function(x) { x.classList.remove("active"); });
    b.classList.add("active");
    $(b.dataset.tab).classList.add("active");
    if (b.dataset.tab === "invites") loadInvites();
    else if (b.dataset.tab === "users") loadUsers();
  };
});

// Invitations
async function createInvite() {
  setErr("inv-err", "");
  try {
    const body = {note: $("inv-note").value.trim()};
    const exp = $("inv-expires").value;
    if (exp) body.expires_at = new Date(exp).toISOString();
    const out = await api("/admin/api/invitations", {method: "POST", body: JSON.stringify(body)});
    showSecret("inv-secret", "Invitation code", out.plaintext);
    $("inv-note").value = "";
    $("inv-expires").value = "";
    loadInvites();
  } catch (e) { setErr("inv-err", e); }
}
async function loadInvites() {
  setErr("inv-err", "");
  try {
    const rows = (await api("/admin/api/invitations")) || [];
    const tbody = $("inv-table").querySelector("tbody");
    tbody.innerHTML = "";
    for (const r of rows) {
      const tr = document.createElement("tr");
      const cells = [r.code_prefix, r.note || "", fmt(r.created_at), fmt(r.expires_at) || "—", fmt(r.consumed_at) || "—", r.consumed_by || "—"];
      for (const c of cells) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      }
      tbody.appendChild(tr);
    }
    if (!rows.length) {
      const td = document.createElement("td");
      td.colSpan = 6;
      td.className = "muted";
      td.textContent = "no invitations yet";
      const tr = document.createElement("tr");
      tr.appendChild(td);
      tbody.appendChild(tr);
    }
  } catch (e) { setErr("inv-err", e); }
}
$("inv-create").onclick = createInvite;
$("inv-refresh").onclick = loadInvites;

// Users
async function loadUsers() {
  setErr("users-err", "");
  try {
    const rows = (await api("/admin/api/users")) || [];
    const tbody = $("users-table").querySelector("tbody");
    tbody.innerHTML = "";
    for (const r of rows) {
      const tr = document.createElement("tr");
      const status = r.disabled_at ? "disabled " + fmt(r.disabled_at) : "active";
      const text = [r.email, r.id, fmt(r.created_at), status];
      for (const c of text) {
        const td = document.createElement("td");
        td.textContent = c;
        tr.appendChild(td);
      }
      const action = document.createElement("td");
      const reset = document.createElement("button");
      reset.textContent = "Reset pw";
      reset.onclick = function() { resetUser(r.id, r.email); };
      action.appendChild(reset);
      action.appendChild(document.createTextNode(" "));
      const disable = document.createElement("button");
      disable.className = "danger";
      disable.textContent = r.disabled_at ? "disabled" : "Disable";
      disable.disabled = !!r.disabled_at;
      disable.onclick = function() { disableUser(r.id, r.email); };
      action.appendChild(disable);
      tr.appendChild(action);
      tbody.appendChild(tr);
    }
    if (!rows.length) {
      const td = document.createElement("td");
      td.colSpan = 5;
      td.className = "muted";
      td.textContent = "no users yet";
      const tr = document.createElement("tr");
      tr.appendChild(td);
      tbody.appendChild(tr);
    }
  } catch (e) { setErr("users-err", e); }
}
async function resetUser(id, email) {
  setErr("users-err", "");
  if (!confirm("Reset password for " + email + "?\nAll their web sessions will be revoked.")) return;
  try {
    const out = await api("/admin/api/users/" + id + "/reset-password", {method: "POST"});
    showSecret("users-secret", "Temporary password for " + email, out.plaintext);
  } catch (e) { setErr("users-err", e); }
}
async function disableUser(id, email) {
  setErr("users-err", "");
  if (!confirm("Disable " + email + "?\nThey can no longer log in or use API tokens.")) return;
  try {
    await api("/admin/api/users/" + id + "/disable", {method: "POST"});
    loadUsers();
  } catch (e) { setErr("users-err", e); }
}
$("users-refresh").onclick = loadUsers;

// Config
$("config-load").onclick = async function() {
  setErr("config-err", "");
  try {
    $("config-out").textContent = JSON.stringify(await api("/admin/api/config"), null, 2);
  } catch (e) { setErr("config-err", e); }
};
</script>
</body></html>`
