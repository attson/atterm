package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/relay"
)

func TestRelaySecurityGeneratesTokenWhenUnset(t *testing.T) {
	var log bytes.Buffer
	cfg, token, err := buildRelayConfig(relayOptions{
		addr:    "127.0.0.1:8080",
		webDir:  "web",
		version: "test",
		log:     &log,
	})
	if err != nil {
		t.Fatalf("buildRelayConfig err: %v", err)
	}
	if token == "" {
		t.Fatal("generated token is empty")
	}
	if cfg.Token != token {
		t.Fatalf("cfg.Token = %q; want generated token", cfg.Token)
	}
	if !strings.Contains(log.String(), token) {
		t.Fatalf("startup log %q does not include generated token", log.String())
	}
}

func TestRelaySecurityRejectsWeakTokenOnPublicListen(t *testing.T) {
	_, _, err := buildRelayConfig(relayOptions{
		addr:    ":8080",
		token:   "dev",
		origins: "https://relay.example.com",
		version: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "weak token") {
		t.Fatalf("err = %v; want weak token rejection", err)
	}
}

func TestRelaySecurityWarnsButAllowsPublicListenWithoutOriginsWithStrongToken(t *testing.T) {
	var log bytes.Buffer
	_, _, err := buildRelayConfig(relayOptions{
		addr:    ":8080",
		token:   "strong-random-token",
		version: "test",
		log:     &log,
	})
	if err != nil {
		t.Fatalf("buildRelayConfig err: %v", err)
	}
	if !strings.Contains(log.String(), "origins") {
		t.Fatalf("startup log %q does not warn about origins", log.String())
	}
}

func TestRelaySecurityDevInsecureAllowsWeakPublicConfig(t *testing.T) {
	var log bytes.Buffer
	cfg, token, err := buildRelayConfig(relayOptions{
		addr:        ":8080",
		token:       "dev",
		webDir:      "web",
		version:     "test",
		devInsecure: true,
		log:         &log,
	})
	if err != nil {
		t.Fatalf("buildRelayConfig err: %v", err)
	}
	if token != "dev" || cfg.Token != "dev" {
		t.Fatalf("token=%q cfg.Token=%q; want dev", token, cfg.Token)
	}
	if !strings.Contains(log.String(), "INSECURE") {
		t.Fatalf("startup log %q does not warn about insecure mode", log.String())
	}
}

func TestRelaySecurityConfiguresReadOnlyTokensAndLimits(t *testing.T) {
	cfg, _, err := buildRelayConfig(relayOptions{
		addr:      "127.0.0.1:8080",
		token:     "strong-random-token",
		readOnly:  "reader-a, reader-b",
		rateLimit: 42,
		maxConns:  7,
		version:   "test",
	})
	if err != nil {
		t.Fatalf("buildRelayConfig err: %v", err)
	}
	if len(cfg.ReadOnlyTokens) != 2 || cfg.ReadOnlyTokens[0] != "reader-a" || cfg.ReadOnlyTokens[1] != "reader-b" {
		t.Fatalf("ReadOnlyTokens = %#v; want reader-a/reader-b", cfg.ReadOnlyTokens)
	}
	if cfg.RateLimitPerMinute != 42 {
		t.Fatalf("RateLimitPerMinute = %d; want 42", cfg.RateLimitPerMinute)
	}
	if cfg.MaxConnectionsPerKey != 7 {
		t.Fatalf("MaxConnectionsPerKey = %d; want 7", cfg.MaxConnectionsPerKey)
	}
}

func TestRelaySecurityLoadsPersistentAdminConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "relay.json")
	if err := os.WriteFile(configPath, []byte(`{
  "rate_limit_per_minute": 22,
  "max_connections_per_key": 5,
  "read_only_tokens": [{"id":"viewer","hash":"`+relay.HashBearerToken("secret")+`","created_at":123}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := buildRelayConfig(relayOptions{
		addr:       "127.0.0.1:8080",
		token:      "main-write-token",
		configPath: configPath,
		version:    "test",
	})
	if err != nil {
		t.Fatalf("buildRelayConfig err: %v", err)
	}
	if cfg.Token != "main-write-token" {
		t.Fatalf("Token = %q; want env/flag main token", cfg.Token)
	}
	if cfg.RateLimitPerMinute != 22 || cfg.MaxConnectionsPerKey != 5 {
		t.Fatalf("limits = %d/%d; want 22/5", cfg.RateLimitPerMinute, cfg.MaxConnectionsPerKey)
	}
	if len(cfg.ReadOnlyTokenHashes) != 1 {
		t.Fatalf("ReadOnlyTokenHashes = %#v; want one hash", cfg.ReadOnlyTokenHashes)
	}
}
