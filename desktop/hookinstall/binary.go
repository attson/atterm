package hookinstall

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ensureBinary materializes the embedded atterm-hook binary as
// ~/.atterm/bin/atterm-hook-<sha8> if not already there, updates
// ~/.atterm/bin/atterm-hook symlink to point at it, and GCs siblings
// older than 7 days. Returns (symlinkPath, sha8, nil) on success.
func ensureBinary(home string) (symlinkPath string, version string, err error) {
	base := attermBinDir(home)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", err
	}

	target := filepath.Join(base, "atterm-hook-"+embeddedHash)
	if _, err := os.Stat(target); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		suffix := make([]byte, 6)
		if _, err := rand.Read(suffix); err != nil {
			return "", "", err
		}
		tmp := target + ".tmp-" + hex.EncodeToString(suffix)
		if err := os.WriteFile(tmp, embeddedHook, 0o755); err != nil {
			return "", "", err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return "", "", err
		}
	}

	link := attermHookSymlink(home)
	newLink := link + ".new"
	_ = os.Remove(newLink)
	if err := os.Symlink(target, newLink); err != nil {
		return "", "", err
	}
	if err := os.Rename(newLink, link); err != nil {
		_ = os.Remove(newLink)
		return "", "", err
	}

	gcOldVersions(base, target, 7*24*time.Hour)
	return link, embeddedHash, nil
}

// gcOldVersions removes files in base whose name begins with
// "atterm-hook-" AND whose path is not keep AND whose mtime is older
// than maxAge. Best-effort; errors are ignored.
func gcOldVersions(base, keep string, maxAge time.Duration) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "atterm-hook-") {
			continue
		}
		full := filepath.Join(base, name)
		if full == keep {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(full)
		}
	}
}
