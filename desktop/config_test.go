package main

import (
	"encoding/json"
	"testing"
)

// TestAppConfig_DeserializeOldJSON_RelayPausedDefaultsFalse verifies that old
// config.json files without a "relay_paused" key deserialize with
// RelayPaused == false. This ensures zero-value semantics are preserved and no
// migration code is needed for existing installs.
func TestAppConfig_DeserializeOldJSON_RelayPausedDefaultsFalse(t *testing.T) {
	raw := `{"relay_url":"wss://x","relay_token":"atk_abc123"}`
	var cfg appConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if cfg.RelayURL != "wss://x" {
		t.Fatalf("RelayURL = %q; want %q", cfg.RelayURL, "wss://x")
	}
	if cfg.RelayToken != "atk_abc123" {
		t.Fatalf("RelayToken = %q; want %q", cfg.RelayToken, "atk_abc123")
	}
	if cfg.RelayPaused {
		t.Fatal("RelayPaused = true; want false for old config.json without the key")
	}
}
