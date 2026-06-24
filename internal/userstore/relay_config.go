package userstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RelayConfig is the singleton relay-wide config row (relay_config, id=1).
// It is the DB-backed replacement for relay.json. Version is a monotonic
// counter bumped on every SetRelayConfig, used by instances to detect and
// pull changes made by another instance's admin API.
type RelayConfig struct {
	RateLimitPerMinute   int
	MaxConnectionsPerKey int
	AllowedOrigins       []string
	VAPIDSubject         string
	Debug                bool
	DebugPayload         bool
	FeishuEnabled        bool
	FeishuEncryptKey     string
	FeishuBaseURL        string
	Version              int64
}

// GetRelayConfig reads the singleton row. If no row exists yet (fresh DB),
// it returns a zero RelayConfig with Version==0 and a nil error — callers
// treat Version==0 as "not configured yet".
func (s *DBStore) GetRelayConfig(ctx context.Context) (RelayConfig, error) {
	var (
		c          RelayConfig
		originsRaw string
		debug, debugPayload, feishuEnabled int64
	)
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT rate_limit_per_minute, max_connections_per_key, allowed_origins,
		        vapid_subject, debug, debug_payload, feishu_enabled,
		        feishu_encrypt_key, feishu_base_url, version
		 FROM relay_config WHERE id = 1`)).
		Scan(&c.RateLimitPerMinute, &c.MaxConnectionsPerKey, &originsRaw,
			&c.VAPIDSubject, &debug, &debugPayload, &feishuEnabled,
			&c.FeishuEncryptKey, &c.FeishuBaseURL, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return RelayConfig{Version: 0}, nil
	}
	if err != nil {
		return RelayConfig{}, fmt.Errorf("get relay_config: %w", err)
	}
	c.Debug = debug != 0
	c.DebugPayload = debugPayload != 0
	c.FeishuEnabled = feishuEnabled != 0
	if originsRaw != "" {
		if err := json.Unmarshal([]byte(originsRaw), &c.AllowedOrigins); err != nil {
			return RelayConfig{}, fmt.Errorf("decode allowed_origins: %w", err)
		}
	}
	return c, nil
}

// SetRelayConfig upserts the singleton row, bumping version by 1 and stamping
// updated_at. Returns the persisted config including the new version.
func (s *DBStore) SetRelayConfig(ctx context.Context, cfg RelayConfig) (RelayConfig, error) {
	origins := cfg.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	originsJSON, err := json.Marshal(origins)
	if err != nil {
		return RelayConfig{}, fmt.Errorf("encode allowed_origins: %w", err)
	}
	now := time.Now().Unix()
	var newVersion int64
	err = s.db.QueryRowContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_config(
		     id, rate_limit_per_minute, max_connections_per_key, allowed_origins,
		     vapid_subject, debug, debug_payload, feishu_enabled,
		     feishu_encrypt_key, feishu_base_url, version, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     rate_limit_per_minute   = excluded.rate_limit_per_minute,
		     max_connections_per_key = excluded.max_connections_per_key,
		     allowed_origins         = excluded.allowed_origins,
		     vapid_subject           = excluded.vapid_subject,
		     debug                   = excluded.debug,
		     debug_payload           = excluded.debug_payload,
		     feishu_enabled          = excluded.feishu_enabled,
		     feishu_encrypt_key      = excluded.feishu_encrypt_key,
		     feishu_base_url         = excluded.feishu_base_url,
		     version                 = relay_config.version + 1,
		     updated_at              = excluded.updated_at
		 RETURNING version`),
		cfg.RateLimitPerMinute, cfg.MaxConnectionsPerKey, string(originsJSON),
		cfg.VAPIDSubject, b2i(cfg.Debug), b2i(cfg.DebugPayload), b2i(cfg.FeishuEnabled),
		cfg.FeishuEncryptKey, cfg.FeishuBaseURL, now).Scan(&newVersion)
	if err != nil {
		return RelayConfig{}, fmt.Errorf("set relay_config: %w", err)
	}
	cfg.Version = newVersion
	return cfg, nil
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
