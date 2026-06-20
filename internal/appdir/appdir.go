// Package appdir centralizes the per-application data location so a development
// build (`wails dev`) is fully isolated from the installed production build.
//
// Production stores data under the OS application-support / cache / log dirs
// (UserConfigDir/atterm, UserCacheDir/atterm, the platform log dir). A dev
// build instead puts *everything* under a single project-local directory
// (<cwd>/.atterm-dev, or $ATTERM_DEV_DIR) so development never reads, clobbers,
// or pollutes the real app's data or the system directories.
//
// The mode is process-global and set once from main() via UseDev(). It is
// deliberately NOT switched in tests (which don't run main()), so tests keep
// the stable production layout and existing path expectations hold.
package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	prodName = "atterm"
	devName  = "atterm-dev"
)

var (
	mu      sync.RWMutex
	name    = prodName
	devRoot string
)

// UseDev switches to the development namespace and points all data at a
// project-local root (<cwd>/.atterm-dev, or $ATTERM_DEV_DIR). Call once at the
// very top of main() for a dev build; never from tests.
func UseDev() {
	mu.Lock()
	defer mu.Unlock()
	name = devName
	if devRoot != "" {
		return
	}
	if env := strings.TrimSpace(os.Getenv("ATTERM_DEV_DIR")); env != "" {
		devRoot = env
		return
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		devRoot = filepath.Join(wd, ".atterm-dev")
	}
	// If cwd is unavailable, devRoot stays "" and the namespaced system dirs
	// (UserConfigDir/atterm-dev, ...) are used as a fallback.
}

// Name returns the directory namespace ("atterm" or "atterm-dev") used for the
// system-dir layout (production, or the dev fallback when no project root).
func Name() string {
	mu.RLock()
	defer mu.RUnlock()
	return name
}

// IsDev reports whether the development namespace is active.
func IsDev() bool { return Name() == devName }

// DevRoot returns the project-local dev data root, or "" in production / when
// it could not be determined.
func DevRoot() string {
	mu.RLock()
	defer mu.RUnlock()
	if name != devName {
		return ""
	}
	return devRoot
}

// KeychainSuffix is appended to OS-keychain service names so a dev build does
// not read or overwrite the production build's credentials. Empty in prod.
func KeychainSuffix() string {
	if IsDev() {
		return ".dev"
	}
	return ""
}

// ConfigDir returns the directory for config-class files (config.json,
// recovery.json, users.db, hook-endpoint, keyring fallback). Dev: <DevRoot>;
// prod: <UserConfigDir>/atterm. Created 0700.
func ConfigDir() (string, error) {
	if root := DevRoot(); root != "" {
		return ensureDir(root)
	}
	return ensure(os.UserConfigDir)
}

// CacheDir returns the directory for cache-class files (paste-images, updates).
// Dev: <DevRoot>/cache; prod: <UserCacheDir>/atterm. Created 0700.
func CacheDir() (string, error) {
	if root := DevRoot(); root != "" {
		return ensureDir(filepath.Join(root, "cache"))
	}
	return ensure(os.UserCacheDir)
}

// DevLogFile returns the dev-mode log file path (<DevRoot>/logs/desktop.log),
// or ok=false in production so the caller keeps its platform-specific default.
func DevLogFile() (string, bool) {
	if root := DevRoot(); root != "" {
		return filepath.Join(root, "logs", "desktop.log"), true
	}
	return "", false
}

func ensure(base func() (string, error)) (string, error) {
	b, err := base()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(b, Name()))
}

func ensureDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
