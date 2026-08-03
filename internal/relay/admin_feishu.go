package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

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
