package appdir

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// HostID resolves a stable, per-machine UUID that atterm uses to identify a
// host across renames, reboots, and container restarts. The id is generated
// once and persisted under ConfigDir()/host_id; subsequent calls return the
// same value.
//
// The ATTERM_HOST_ID environment variable, if set, overrides the file —
// useful for containerized deployments where the config dir is ephemeral.
//
// The dev/prod split is honored via ConfigDir(): a dev build that called
// UseDev() in main() will land here with ConfigDir() pointing at the
// project-local data root, so dev and prod end up with DISTINCT host ids on
// the same machine. Without this, two atterm processes (e.g. the installed
// app + `wails dev`) would share one host id and the relay would merge their
// session lists into a single host group, defeating multi-instance
// distinction in the session bar.
//
// Returns an empty string only if both the env override is unset and
// ConfigDir() fails AND writing the fresh id fails; callers should treat
// empty as "host id unknown".
func HostID() string {
	if v := strings.TrimSpace(os.Getenv(hostIDEnvOverride)); v != "" {
		return v
	}
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, hostIDFilename)
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if _, err := uuid.Parse(id); err == nil {
			return id
		}
	}
	id := uuid.New().String()
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return ""
	}
	return id
}

// HostIDPath returns the on-disk path the host id is persisted to (empty if
// the config dir is unavailable). Useful for diagnostics UIs.
func HostIDPath() string {
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, hostIDFilename)
}

const (
	hostIDEnvOverride = "ATTERM_HOST_ID"
	hostIDFilename    = "host_id"
)
