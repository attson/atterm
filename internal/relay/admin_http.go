package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

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
