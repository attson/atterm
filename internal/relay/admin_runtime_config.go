package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)


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

	// AllowedOrigins is the current HTTP/WS Origin allow-list. Editable via
	// PUT with allowed_origins in the body. An empty list means "any origin"
	// (dev mode). Mobile Capacitor clients need "capacitor://localhost" here.
	AllowedOrigins []string `json:"allowed_origins"`

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
		AllowedOrigins:              append([]string(nil), cfg.AllowedOrigins...),
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
