package hostid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/appdir"
	"github.com/google/uuid"
)

// withIsolatedConfigHome routes appdir.ConfigDir() (and therefore hostid) at a
// per-test temp directory by hijacking the platform's UserConfigDir lookup:
//
//	macOS  : HOME             → <HOME>/Library/Application Support
//	linux  : XDG_CONFIG_HOME  → <XDG_CONFIG_HOME>
//	windows: APPDATA          → <APPDATA>
//
// All three are set so the test passes regardless of which platform we're on.
// The env vars are restored via t.Setenv's cleanup hook.
func withIsolatedConfigHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
}

func TestGet_envOverrideWins(t *testing.T) {
	withIsolatedConfigHome(t)
	t.Setenv("ATTERM_HOST_ID", "fixed-from-env")
	if got := Get(); got != "fixed-from-env" {
		t.Errorf("Get() with env override = %q, want %q", got, "fixed-from-env")
	}
}

func TestGet_generatesAndPersistsUnderAppdir(t *testing.T) {
	withIsolatedConfigHome(t)
	t.Setenv("ATTERM_HOST_ID", "")

	first := Get()
	if first == "" {
		t.Fatal("Get() returned empty on first call")
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("Get() = %q, not a valid UUID: %v", first, err)
	}

	// The persisted file must live under appdir.ConfigDir() — NOT under a
	// hardcoded "atterm" subdir of UserConfigDir. Regression for the bug
	// where dev (.atterm-dev) and prod ($CFG/atterm) shared one host_id.
	cfgDir, err := appdir.ConfigDir()
	if err != nil {
		t.Fatalf("appdir.ConfigDir(): %v", err)
	}
	wantPath := filepath.Join(cfgDir, "host_id")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected host_id file at %s: %v", wantPath, err)
	}
	if got := strings.TrimSpace(string(data)); got != first {
		t.Errorf("persisted host_id = %q, want %q", got, first)
	}

	// Second call must return the same id (reads the persisted file).
	if second := Get(); second != first {
		t.Errorf("Get() second call = %q, want %q (stable across calls)", second, first)
	}
}

func TestPath_matchesAppdir(t *testing.T) {
	withIsolatedConfigHome(t)
	cfgDir, err := appdir.ConfigDir()
	if err != nil {
		t.Fatalf("appdir.ConfigDir(): %v", err)
	}
	if got, want := Path(), filepath.Join(cfgDir, "host_id"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
