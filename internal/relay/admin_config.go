package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/attson/atterm/internal/userstore"
)

const tokenHashPrefix = "sha256:"

type StoredToken struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	CreatedAt int64  `json:"created_at"`
}

type AdminConfig struct {
	RateLimitPerMinute   int           `json:"rate_limit_per_minute"`
	MaxConnectionsPerKey int           `json:"max_connections_per_key"`
	ReadOnlyTokens       []StoredToken `json:"read_only_tokens,omitempty"`

	// Feishu integration — moved out of env so an admin can toggle it at
	// runtime. FeishuEncryptKey is the relay-wide field-encryption key
	// (base64, 32 bytes) that protects per-user Feishu credentials at rest
	// in users.db. It lives in this 0600 file, never logged or returned in
	// plaintext by the admin API.
	FeishuEnabled    bool   `json:"feishu_enabled,omitempty"`
	FeishuEncryptKey string `json:"feishu_encrypt_key,omitempty"`
	FeishuBaseURL    string `json:"feishu_base_url,omitempty"`

	// AllowedOrigins is the HTTP/WS Origin allow-list (hot-reloadable).
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
	// VAPIDSubject is persisted but applied only at startup (webpush.Open
	// consumes it once); changing it needs a relay restart.
	VAPIDSubject string `json:"vapid_subject,omitempty"`

	// Debug / DebugPayload are hot-reloadable verbose-logging switches.
	// DebugPayload also logs PTY byte contents (terminal in/out) — sensitive.
	Debug        bool `json:"debug,omitempty"`
	DebugPayload bool `json:"debug_payload,omitempty"`

	// version is the DB relay_config.version; 0 = unconfigured. Unexported
	// so it is never marshalled to JSON or surfaced directly; consumers use
	// AdminConfigStore.Version().
	version int64 `json:"-"` //nolint:unused
}

// DecodeFeishuKey decodes FeishuEncryptKey to its 32 raw bytes, or returns
// an error if it is empty or malformed.
func (c AdminConfig) DecodeFeishuKey() ([]byte, error) {
	if c.FeishuEncryptKey == "" {
		return nil, errors.New("feishu encrypt key is empty")
	}
	raw, err := base64.StdEncoding.DecodeString(c.FeishuEncryptKey)
	if err != nil {
		return nil, fmt.Errorf("feishu encrypt key: not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("feishu encrypt key: want 32 bytes, got %d", len(raw))
	}
	return raw, nil
}

type AdminConfigStore struct {
	mu    sync.Mutex
	store userstore.Store
	cfg   AdminConfig
}

func NewAdminConfigStore(store userstore.Store, initial AdminConfig) *AdminConfigStore {
	return &AdminConfigStore{store: store, cfg: initial}
}

// LoadFromDB reads relay_config and replaces the in-memory cfg. If the DB has
// no config yet (Version==0), the in-memory cfg is left as the seeded initial.
func (s *AdminConfigStore) LoadFromDB(ctx context.Context) (AdminConfig, error) {
	rc, err := s.store.GetRelayConfig(ctx)
	if err != nil {
		return AdminConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rc.Version > 0 {
		s.cfg = relayConfigToAdmin(rc)
	}
	return cloneAdminConfig(s.cfg), nil
}

func (s *AdminConfigStore) Snapshot() AdminConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAdminConfig(s.cfg)
}

func (s *AdminConfigStore) Version() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.version
}

// Set validates, writes to the DB (bumping version), and updates the cache.
func (s *AdminConfigStore) Set(ctx context.Context, cfg AdminConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	written, err := s.store.SetRelayConfig(ctx, adminToRelayConfig(cfg))
	if err != nil {
		return err
	}
	applied := relayConfigToAdmin(written)
	// Preserve ReadOnlyTokens since they are not stored in the DB.
	applied.ReadOnlyTokens = append([]StoredToken(nil), cfg.ReadOnlyTokens...)
	s.mu.Lock()
	s.cfg = applied
	s.mu.Unlock()
	return nil
}

func relayConfigToAdmin(rc userstore.RelayConfig) AdminConfig {
	return AdminConfig{
		RateLimitPerMinute:   rc.RateLimitPerMinute,
		MaxConnectionsPerKey: rc.MaxConnectionsPerKey,
		AllowedOrigins:       append([]string(nil), rc.AllowedOrigins...),
		VAPIDSubject:         rc.VAPIDSubject,
		Debug:                rc.Debug,
		DebugPayload:         rc.DebugPayload,
		FeishuEnabled:        rc.FeishuEnabled,
		FeishuEncryptKey:     rc.FeishuEncryptKey,
		FeishuBaseURL:        rc.FeishuBaseURL,
		version:              rc.Version,
	}
}

func adminToRelayConfig(c AdminConfig) userstore.RelayConfig {
	return userstore.RelayConfig{
		RateLimitPerMinute:   c.RateLimitPerMinute,
		MaxConnectionsPerKey: c.MaxConnectionsPerKey,
		AllowedOrigins:       append([]string(nil), c.AllowedOrigins...),
		VAPIDSubject:         c.VAPIDSubject,
		Debug:                c.Debug,
		DebugPayload:         c.DebugPayload,
		FeishuEnabled:        c.FeishuEnabled,
		FeishuEncryptKey:     c.FeishuEncryptKey,
		FeishuBaseURL:        c.FeishuBaseURL,
	}
}

func (c AdminConfig) validate() error {
	seen := make(map[string]struct{}, len(c.ReadOnlyTokens))
	for _, tok := range c.ReadOnlyTokens {
		id := strings.TrimSpace(tok.ID)
		if id == "" {
			return errors.New("read-only token id is empty")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate read-only token id %q", id)
		}
		seen[id] = struct{}{}
		if !strings.HasPrefix(tok.Hash, tokenHashPrefix) {
			return fmt.Errorf("read-only token %q has unsupported hash", id)
		}
	}
	// A Feishu-enabled config must carry a usable 32-byte key.
	if c.FeishuEnabled {
		if _, err := c.DecodeFeishuKey(); err != nil {
			return fmt.Errorf("feishu enabled but %w", err)
		}
	}
	return nil
}

func cloneAdminConfig(cfg AdminConfig) AdminConfig {
	cfg.ReadOnlyTokens = append([]StoredToken(nil), cfg.ReadOnlyTokens...)
	cfg.AllowedOrigins = append([]string(nil), cfg.AllowedOrigins...)
	return cfg
}

func HashBearerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return tokenHashPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func tokenMatchesHash(token, hash string) bool {
	return token != "" && HashBearerToken(token) == hash
}
