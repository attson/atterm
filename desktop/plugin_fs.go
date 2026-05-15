package main

// PluginFS exposes a small, read-only filesystem API to the desktop webview
// for the File Explorer plugin. Every method runs every path argument through
// resolve() before any I/O.
//
// SECURITY (red-line #11): This binding group is local-only. It MUST NOT be
// reachable from uplink/relay code under any circumstance. The CI check at
// .github/scripts/check-plugin-fs-isolation.sh asserts the package graph
// stays clean.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type PluginFS struct {
	// allowRoots holds the set of directories the binding accepts as
	// containers for path arguments. Populated at construction time from
	// $HOME plus the live set of active local session cwds (see app.go
	// wiring).
	allowRoots []string
}

var (
	ErrPathRelative  = errors.New("plugin_fs: path must be absolute")
	ErrPathForbidden = errors.New("plugin_fs: path forbidden")
	ErrPathDenied    = errors.New("plugin_fs: path denied by policy")
)

// denyExact and denySuffix express paths that are never visible regardless
// of allowRoots. The check is run on the fully-resolved path.
var denyExact = []string{".ssh", ".gnupg", ".aws"}
var denySuffix = []string{".env"}

func isDenied(resolved string) bool {
	base := filepath.Base(resolved)
	for _, d := range denyExact {
		if base == d {
			return true
		}
	}
	for _, suf := range denySuffix {
		if base == suf || strings.HasPrefix(base, suf+".") {
			return true
		}
	}
	// Also walk segments for nested ~/.ssh inside an allowed root.
	parts := strings.Split(resolved, string(filepath.Separator))
	for _, p := range parts {
		for _, d := range denyExact {
			if p == d {
				return true
			}
		}
	}
	return false
}

// resolve normalizes path, follows symlinks, and checks against allowRoots
// and deny patterns. Returns the cleaned absolute path on success.
func (p *PluginFS) resolve(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrPathRelative
	}
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// If the path itself does not yet exist (e.g. for a Reveal call on
		// a freshly-created file that has not yet been observed by us), fall
		// back to the lexical clean. Allowlist still applies.
		resolved = clean
	}
	if isDenied(resolved) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, resolved)
	}
	for _, root := range p.allowRoots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolvedRoot = root
		}
		rel, err := filepath.Rel(resolvedRoot, resolved)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPathForbidden, resolved)
}
