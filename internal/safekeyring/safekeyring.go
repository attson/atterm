// Package safekeyring wraps github.com/zalando/go-keyring with an automatic
// file fallback so secrets persist even when the OS keychain is unavailable.
//
// Motivation: on an ad-hoc-signed (unsigned / un-notarized) macOS build, the
// `security` helper go-keyring shells out to is distrusted by the OS and gets
// killed ("signal: killed"), so every Get/Set spams an error and spawns a
// doomed subprocess. After the first hard failure this package latches the
// keychain "degraded" and switches to a 0600 JSON file under the app config
// dir for the rest of the session: no more helper invocations, no spam, and
// secrets still persist across restarts.
//
// Security note: the keychain is OS-encrypted and access-controlled; the file
// fallback is plaintext-at-rest (0600, owner-only), the same posture as gh /
// aws / npm credential files. A properly signed + notarized build keeps using
// the keychain and never touches the file.
package safekeyring

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/logging"
	"github.com/zalando/go-keyring"
)

// ErrNotFound is re-exported so callers need not import go-keyring directly.
var ErrNotFound = keyring.ErrNotFound

var (
	mu       sync.Mutex
	degraded bool // latched after a hard keychain failure
	fileOnly bool // skip the keychain entirely (dev / tests / unsigned builds)

	fileMu  sync.Mutex // serializes the fallback file read-modify-write
	dirMu   sync.Mutex // guards fileDir
	fileDir string     // test override; "" = <appdir.ConfigDir>
)

// UseFileStore makes every operation use the 0600 file store directly, without
// ever invoking the OS keychain. Call this for builds where the keychain is
// known to be unavailable or undesirable — a `wails dev` build, or tests —
// so macOS never pops a keychain-access prompt and Linux/Windows never spawn
// the helper.
func UseFileStore() {
	mu.Lock()
	fileOnly = true
	mu.Unlock()
}

func markDegraded(op string, err error) {
	mu.Lock()
	first := !degraded
	degraded = true
	mu.Unlock()
	if first {
		logging.Warn("keychain", "keychain unavailable (%s: %v) — falling back to a 0600 file store for this session. "+
			"This is expected for an unsigned/ad-hoc build; sign + notarize the app to use the OS keychain.", op, err)
	}
}

// useFile reports whether operations should bypass the keychain — either
// latched degraded, or forced via UseFileStore.
func useFile() bool {
	mu.Lock()
	defer mu.Unlock()
	return degraded || fileOnly
}

// Reset clears the degraded latch and file-only flag. Intended for tests.
func Reset() {
	mu.Lock()
	degraded = false
	fileOnly = false
	mu.Unlock()
}

// SetFileDirForTest overrides the fallback file directory so tests don't touch
// the real user config dir. Pass "" to restore the default.
func SetFileDirForTest(dir string) {
	dirMu.Lock()
	fileDir = dir
	dirMu.Unlock()
}

// Get returns the stored secret, or ErrNotFound if absent. When the keychain
// is healthy it is the source of truth; once degraded, the file store is.
func Get(service, account string) (string, error) {
	if !useFile() {
		v, err := keyring.Get(service, account)
		if err == nil {
			return v, nil
		}
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		markDegraded("get", err)
	}
	return fileGet(service, account)
}

// Set stores secret. Keychain when healthy; on (or after) a keychain failure
// it writes the 0600 file instead. A file write error is returned so callers
// know persistence truly failed.
func Set(service, account, secret string) error {
	if !useFile() {
		if err := keyring.Set(service, account, secret); err == nil {
			return nil
		} else {
			markDegraded("set", err)
		}
	}
	return fileSet(service, account, secret)
}

// Delete removes the secret from whichever store is active. Absent secrets
// succeed.
func Delete(service, account string) error {
	if !useFile() {
		if err := keyring.Delete(service, account); err == nil || errors.Is(err, keyring.ErrNotFound) {
			return nil
		} else {
			markDegraded("delete", err)
		}
	}
	return fileDelete(service, account)
}

// --- 0600 file fallback ---------------------------------------------------

func fallbackPath() (string, error) {
	dirMu.Lock()
	dir := fileDir
	dirMu.Unlock()
	if dir == "" {
		// Default: the namespaced app config dir (appdir already MkdirAll's it),
		// so the fallback file sits next to config.json / recovery.json and is
		// isolated per dev/prod build.
		d, err := appdir.ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "keyring-fallback.json"), nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "keyring-fallback.json"), nil
}

func fileKey(service, account string) string { return service + "\x00" + account }

func readFallback() (map[string]string, string, error) {
	path, err := fallbackPath()
	if err != nil {
		return nil, "", err
	}
	out := map[string]string{}
	blob, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, path, nil
		}
		return nil, path, err
	}
	if len(blob) > 0 {
		if err := json.Unmarshal(blob, &out); err != nil {
			// Corrupt fallback: treat as empty rather than wedging the app.
			return map[string]string{}, path, nil
		}
	}
	return out, path, nil
}

func fileGet(service, account string) (string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	m, _, err := readFallback()
	if err != nil {
		return "", ErrNotFound
	}
	v, ok := m[fileKey(service, account)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func fileSet(service, account, secret string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	m, path, err := readFallback()
	if err != nil {
		return err
	}
	m[fileKey(service, account)] = secret
	return writeFallback(path, m)
}

func fileDelete(service, account string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	m, path, err := readFallback()
	if err != nil {
		return nil
	}
	delete(m, fileKey(service, account))
	return writeFallback(path, m)
}

func writeFallback(path string, m map[string]string) error {
	blob, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
