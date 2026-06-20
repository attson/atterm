// Package appdir centralizes the per-application directory namespace so a
// development build (`wails dev`) is fully isolated from the installed
// production build: config, recovery snapshot, local relay db, cached files,
// credential fallback, and OS-keychain entries all live under a separate
// "atterm-dev" namespace instead of clobbering the real "atterm" data.
//
// The namespace is process-global and set once from main() via UseDev(). It is
// deliberately NOT switched in tests (which don't run main()), so tests keep
// the stable "atterm" namespace and existing path expectations hold.
package appdir

import (
	"os"
	"path/filepath"
	"sync"
)

const (
	prodName = "atterm"
	devName  = "atterm-dev"
)

var (
	mu   sync.RWMutex
	name = prodName
)

// UseDev switches every app path + keychain service name to the development
// namespace. Call once at the very top of main() (before any path is read)
// when the build is a dev build.
func UseDev() {
	mu.Lock()
	name = devName
	mu.Unlock()
}

// Name returns the current directory namespace ("atterm" or "atterm-dev").
func Name() string {
	mu.RLock()
	defer mu.RUnlock()
	return name
}

// IsDev reports whether the development namespace is active.
func IsDev() bool { return Name() == devName }

// KeychainSuffix is appended to OS-keychain service names so a dev build does
// not read or overwrite the production build's credentials. Empty in prod.
func KeychainSuffix() string {
	if IsDev() {
		return ".dev"
	}
	return ""
}

// ConfigDir returns <os.UserConfigDir>/<Name>, creating it 0700.
func ConfigDir() (string, error) { return ensure(os.UserConfigDir) }

// CacheDir returns <os.UserCacheDir>/<Name>, creating it 0700.
func CacheDir() (string, error) { return ensure(os.UserCacheDir) }

func ensure(base func() (string, error)) (string, error) {
	b, err := base()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(b, Name())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
