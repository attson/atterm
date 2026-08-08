package relay

import (
	"context"
	"time"

	"github.com/attson/atterm/internal/logging"
)

// refreshConfigOnce pulls relay_config from the DB; if its version differs from
// the last-applied version, it applies the new values to the in-memory hot
// caches (origins, debug, limiters, Feishu cipher) and returns true.
func (s *Server) refreshConfigOnce(ctx context.Context) (bool, error) {
	store := s.cfg.AdminConfigStore
	prev := store.Version()
	applied, err := store.LoadFromDB(ctx)
	if err != nil {
		return false, err
	}
	if applied.version == prev {
		return false, nil
	}
	s.applyConfigToCaches(applied)
	return true, nil
}

// applyConfigToCaches pushes config values into the request-time atomic caches
// and limiters. Safe to call repeatedly.
//
// NOTE: VAPIDSubject is deliberately NOT hot-applied here — it is consumed once
// at webpush.Open and changing it requires restarting each instance; the
// cross-instance config refresher does not propagate it live.
func (s *Server) applyConfigToCaches(cfg AdminConfig) {
	s.SetAllowedOrigins(OriginPatterns(cfg.AllowedOrigins))
	s.SetDebug(cfg.Debug, cfg.DebugPayload)
	s.applyRuntimeLimits(cfg.RateLimitPerMinute, cfg.MaxConnectionsPerKey)
	if keyBytes, err := cfg.DecodeFeishuKey(); err == nil && cfg.FeishuEnabled {
		if err := s.ApplyFeishuConfig(true, keyBytes, cfg.FeishuBaseURL); err != nil {
			logging.Error("relay-config", "ApplyFeishuConfig: %v", err)
		}
	} else {
		if err := s.ApplyFeishuConfig(false, nil, cfg.FeishuBaseURL); err != nil {
			logging.Error("relay-config", "ApplyFeishuConfig(disable): %v", err)
		}
	}
}

// startConfigRefresher runs refreshConfigOnce every interval until ctx is done.
func (s *Server) startConfigRefresher(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := s.refreshConfigOnce(ctx); err != nil {
					logging.Warn("relay-config", "periodic refresh: %v", err)
				}
			}
		}
	}()
}

// StartConfigRefresher is the exported wrapper for startConfigRefresher, called
// from cmd/atterm-relay/main.go which is in a different package.
func (s *Server) StartConfigRefresher(ctx context.Context, interval time.Duration) {
	s.startConfigRefresher(ctx, interval)
}
