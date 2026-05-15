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
	"os"
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

const (
	maxReadBytesHard = 5 * 1024 * 1024 // server-side hard cap (5 MB)
	binaryProbeBytes = 4096            // bytes inspected for NUL → binary
)

type FileContent struct {
	Path        string `json:"path"`
	Data        []byte `json:"data"`
	IsBinary    bool   `json:"isBinary"`
	TruncatedAt int64  `json:"truncatedAt,omitempty"`
}

type FileMetaInfo struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`
	IsBinary bool   `json:"isBinary"`
}

var osReadDir = func(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

// DirEntry is a serialized representation of one directory entry.
type DirEntry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size,omitempty"`
	ModTime int64  `json:"modTime,omitempty"` // unix ms
}

// ListDir returns entries inside path. Path must be a directory and inside an
// allow-root. Hidden filtering is done frontend-side; ListDir is exhaustive.
func (p *PluginFS) ListDir(path string) ([]DirEntry, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := osReadDir(resolved)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, DirEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	return out, nil
}

// ReadFile returns up to maxBytes from path. If the file is larger than
// maxBytes, the returned Data is truncated and TruncatedAt reports the full
// file size. Binary detection samples the first 4 KB.
func (p *PluginFS) ReadFile(path string, maxBytes int64) (FileContent, error) {
	if maxBytes > maxReadBytesHard {
		return FileContent{}, fmt.Errorf("plugin_fs: maxBytes %d exceeds hard cap %d", maxBytes, maxReadBytesHard)
	}
	resolved, err := p.resolve(path)
	if err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileContent{}, err
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("plugin_fs: %s is a directory", resolved)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return FileContent{}, err
	}
	defer f.Close()

	size := info.Size()
	readLen := size
	truncated := int64(0)
	if size > maxBytes {
		readLen = maxBytes
		truncated = size
	}
	data := make([]byte, readLen)
	if _, err := f.Read(data); err != nil && err.Error() != "EOF" {
		return FileContent{}, err
	}
	probe := data
	if int64(len(probe)) > binaryProbeBytes {
		probe = probe[:binaryProbeBytes]
	}
	isBin := false
	for _, b := range probe {
		if b == 0 {
			isBin = true
			break
		}
	}
	return FileContent{Path: resolved, Data: data, IsBinary: isBin, TruncatedAt: truncated}, nil
}

// FileMeta returns size + modtime + binary-ness without reading the file body.
// Used by the frontend's "should I open this in the editor?" pre-check.
func (p *PluginFS) FileMeta(path string) (FileMetaInfo, error) {
	resolved, err := p.resolve(path)
	if err != nil {
		return FileMetaInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FileMetaInfo{}, err
	}
	isBin := false
	if !info.IsDir() {
		f, err := os.Open(resolved)
		if err == nil {
			probe := make([]byte, binaryProbeBytes)
			n, _ := f.Read(probe)
			f.Close()
			for _, b := range probe[:n] {
				if b == 0 {
					isBin = true
					break
				}
			}
		}
	}
	return FileMetaInfo{
		Path:     resolved,
		Size:     info.Size(),
		ModTime:  info.ModTime().UnixMilli(),
		IsBinary: isBin,
	}, nil
}
