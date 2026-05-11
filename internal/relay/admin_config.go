package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
}

type AdminConfigStore struct {
	mu   sync.Mutex
	path string
	cfg  AdminConfig
}

func NewAdminConfigStore(path string, initial AdminConfig) *AdminConfigStore {
	return &AdminConfigStore{path: path, cfg: initial}
}

func LoadAdminConfig(path string) (AdminConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AdminConfig{}, nil
		}
		return AdminConfig{}, err
	}
	var cfg AdminConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AdminConfig{}, err
	}
	if err := cfg.validate(); err != nil {
		return AdminConfig{}, err
	}
	return cfg, nil
}

func (s *AdminConfigStore) Load() (AdminConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := LoadAdminConfig(s.path)
	if err != nil {
		return AdminConfig{}, err
	}
	s.cfg = cfg
	return cfg, nil
}

func (s *AdminConfigStore) Snapshot() AdminConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAdminConfig(s.cfg)
}

func (s *AdminConfigStore) Set(cfg AdminConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cloneAdminConfig(cfg)
	s.mu.Unlock()
	return s.Save()
}

func (s *AdminConfigStore) Save() error {
	s.mu.Lock()
	cfg := cloneAdminConfig(s.cfg)
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return errors.New("admin config path is empty")
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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
	return nil
}

func cloneAdminConfig(cfg AdminConfig) AdminConfig {
	cfg.ReadOnlyTokens = append([]StoredToken(nil), cfg.ReadOnlyTokens...)
	return cfg
}

func HashBearerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return tokenHashPrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

func tokenMatchesHash(token, hash string) bool {
	return token != "" && HashBearerToken(token) == hash
}
