package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// appConfig is what we persist to ~/.config/atterm/config.json.
// Empty fields mean "not configured" — RelayURL == "" disables uplink entirely.
type appConfig struct {
	RelayURL   string `json:"relay_url,omitempty"`
	RelayToken string `json:"relay_token,omitempty"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "atterm", "config.json")
}

// configStore is a thin lock-protected wrapper around appConfig with disk I/O.
type configStore struct {
	mu  sync.Mutex
	cfg appConfig
}

func loadConfig() *configStore {
	s := &configStore{}
	p := configPath()
	if p == "" {
		return s
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, &s.cfg)
	return s
}

// Get returns a snapshot of the current config.
func (s *configStore) Get() appConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Set replaces the config and persists it to disk atomically (write-temp-rename).
func (s *configStore) Set(c appConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
	p := configPath()
	if p == "" {
		return errors.New("user config dir unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
