// Package hostid resolves a stable, per-machine UUID that atterm uses to
// identify a host across renames, reboots, and container restarts. The id
// is generated once and persisted to the user config dir; subsequent calls
// return the same value.
//
// The ATTERM_HOST_ID environment variable, if set, overrides the file —
// useful for containerized deployments where the config dir is ephemeral.
package hostid

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const envOverride = "ATTERM_HOST_ID"

// Get returns the stable host id, generating and persisting it on first call.
// Returns an empty string only if both the env override is unset and the
// config dir is unavailable AND writing it failed; callers should treat empty
// as "host id unknown".
func Get() string {
	if v := strings.TrimSpace(os.Getenv(envOverride)); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, "atterm", "host_id")
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if _, err := uuid.Parse(id); err == nil {
			return id
		}
	}
	id := uuid.New().String()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return ""
	}
	return id
}

// Path returns the on-disk path the host id is persisted to (empty if the
// config dir is unavailable). Useful for diagnostics and the Phase 1 desktop
// settings UI.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "atterm", "host_id")
}
