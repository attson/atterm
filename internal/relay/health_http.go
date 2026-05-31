// Package relay — health endpoints. /healthz is public minimal; the
// /admin/health* endpoints are admin-only and emit a redaction-safe
// snapshot of operator-relevant runtime state.
package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// HealthPayload is the contract returned by /admin/api/health and rendered
// into /admin/health. Every field is either an operator-configured value
// or an aggregate count — no PII, no secrets, no file paths.
type HealthPayload struct {
	Version                  string   `json:"version"`
	UptimeSeconds            int64    `json:"uptime_seconds"`
	HTTPS                    bool     `json:"https"`
	ConfiguredOrigins        []string `json:"configured_origins"`
	OriginsOpen              bool     `json:"origins_open"`
	BootstrapAdminConfigured bool     `json:"bootstrap_admin_configured"`
	RateLimitPerMinute       int      `json:"rate_limit_per_minute"`
	MaxConnectionsPerKey     int      `json:"max_connections_per_key"`
	ActiveUplinks            int64    `json:"active_uplinks"`
	MobileOriginCompatible   bool     `json:"mobile_origin_compatible"`
	GeneratedAt              string   `json:"generated_at"`
	Warnings                 []string `json:"health_check_warnings,omitempty"`
}

// isMobileOriginCompatible reports whether the configured origin allow-list
// admits a mobile webview (Capacitor / Ionic / iOS WKWebView) origin. An
// empty list means "any origin is allowed" — technically compatible but
// also a security warning the caller surfaces separately.
func isMobileOriginCompatible(origins []string) bool {
	if len(origins) == 0 {
		return true
	}
	for _, o := range origins {
		switch {
		case strings.HasPrefix(o, "capacitor://"):
			return true
		case strings.HasPrefix(o, "ionic://"):
			return true
		case strings.HasPrefix(o, "https://localhost"):
			return true
		case o == "null":
			return true
		}
	}
	return false
}

// httpsFromRequest mirrors publicBaseURL's TLS detection: true if the
// request arrived over TLS directly OR a forwarding proxy declared HTTPS
// via X-Forwarded-Proto.
func httpsFromRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// collectHealth gathers the runtime state of s into a HealthPayload. r
// is used only for HTTPS detection (per-request, since the same binary
// can be reached over both schemes via different listeners). On Store
// failures, BootstrapAdminConfigured falls back to false and a warning
// is appended to Warnings.
func collectHealth(ctx context.Context, s *Server, r *http.Request) HealthPayload {
	cfg := s.cfg

	rateLimit := cfg.RateLimitPerMinute
	if rateLimit == 0 {
		rateLimit = defaultRateLimitPerMinute
	}
	connLimit := cfg.MaxConnectionsPerKey
	if connLimit == 0 {
		connLimit = defaultMaxConnections
	}

	originsCopy := append([]string(nil), cfg.AllowedOrigins...)

	payload := HealthPayload{
		Version:                cfg.Version,
		UptimeSeconds:          int64(time.Since(s.startTime).Seconds()),
		HTTPS:                  httpsFromRequest(r),
		ConfiguredOrigins:      originsCopy,
		OriginsOpen:            len(originsCopy) == 0,
		RateLimitPerMinute:     rateLimit,
		MaxConnectionsPerKey:   connLimit,
		ActiveUplinks:          s.UplinkCount(),
		MobileOriginCompatible: isMobileOriginCompatible(originsCopy),
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339),
	}

	if cfg.Store != nil {
		users, err := cfg.Store.ListUsers(ctx)
		if err != nil {
			payload.Warnings = append(payload.Warnings, "bootstrap_admin_lookup_failed")
		} else {
			for _, u := range users {
				if u.IsAdmin {
					payload.BootstrapAdminConfigured = true
					break
				}
			}
		}
	}

	return payload
}

// handleHealthz returns a minimal liveness response for load balancers
// and probes. Public, no auth, no cache.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"version": s.cfg.Version,
	})
}
