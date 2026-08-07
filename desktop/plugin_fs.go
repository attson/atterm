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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type PluginFS struct {
	access *fsAccess
}

func (p *PluginFS) resolve(path string) (string, error) {
	return p.access.resolve(path)
}

// ListDir returns entries inside path. Path must be a directory and inside an
// allow-root. Hidden filtering is done frontend-side; ListDir is exhaustive.
func (p *PluginFS) ListDir(path string) ([]DirEntry, error) {
	return p.access.listDir(path)
}

// ReadFile returns up to maxBytes from path. If the file is larger than
// maxBytes, the returned Data is truncated and TruncatedAt reports the full
// file size. Binary detection samples the first 4 KB.
func (p *PluginFS) ReadFile(path string, maxBytes int64) (FileContent, error) {
	return p.access.readFile(path, maxBytes)
}

// FileMeta returns size + modtime + binary-ness without reading the file body.
// Used by the frontend's "should I open this in the editor?" pre-check.
func (p *PluginFS) FileMeta(path string) (FileMetaInfo, error) {
	return p.access.fileMeta(path)
}

// RevealInOS asks the OS file manager to select the path. On macOS:
//
//	open -R <path>
//
// On Linux:
//
//	xdg-open <dir-of-path>      (no per-platform "reveal selecting" verb)
//
// On Windows:
//
//	explorer /select,<path>
func (p *PluginFS) RevealInOS(path string) error {
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	switch stdruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", resolved).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+resolved).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(resolved)).Start()
	}
}

// OpenExternal launches the OS default application for the path.
func (p *PluginFS) OpenExternal(path string) error {
	resolved, err := p.resolve(path)
	if err != nil {
		return err
	}
	return openExternalPath(resolved)
}

var openExternalPath = func(resolved string) error {
	switch stdruntime.GOOS {
	case "darwin":
		return exec.Command("open", resolved).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", resolved).Start()
	default:
		return exec.Command("xdg-open", resolved).Start()
	}
}

// NewPluginFS builds a PluginFS with allowRoots seeded from $HOME. The set is
// later expanded at call time to include cwds of active local sessions; that
// dynamic enrichment is plugged in below.
func NewPluginFS() *PluginFS {
	home, _ := os.UserHomeDir()
	// Local Wails path: never crosses the relay, so .env is readable.
	return &PluginFS{access: newFSAccess([]string{home}, false)}
}

func (p *PluginFS) setupWatcher(ctx context.Context) {
	p.access.setupWatcher(ctx, func(dir string) {
		wailsruntime.EventsEmit(ctx, "plugin-fs:dir-changed", dir)
	})
}

func (p *PluginFS) shutdownWatcher() {
	p.access.shutdownWatcher()
}

func (p *PluginFS) WatchDir(path string) (int64, error) {
	return p.access.watchDir(path)
}

func (p *PluginFS) UnwatchDir(handleID int64) error {
	return p.access.unwatchDir(handleID)
}
