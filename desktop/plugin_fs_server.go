package main

// PluginFS HTTP handler — serves bytes for paths the File Explorer plugin has
// already validated through ListDir / FileMeta. Mounted on the Wails asset
// server so the webview can address local files via stable URLs.
//
// URL form:   /pluginfs/<base64.URLEncoding(absolute-path)>
// Allowed methods: GET, HEAD.
//
// SECURITY (red-line #11): every request runs through PluginFS.resolve()
// which enforces the same allowRoots + denylist as ReadFile. This file is part
// of the same package so the existing CI isolation check
// (.github/scripts/check-plugin-fs-isolation.sh) keeps it inside the desktop
// boundary.

import (
	"encoding/base64"
	"net/http"
	"os"
	"strings"
)

const pluginFSURLPrefix = "/pluginfs/"

// ServeHTTP implements http.Handler. Wails routes any asset request that
// doesn't match the embedded SPA to this handler.
func (p *PluginFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, pluginFSURLPrefix) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	encoded := strings.TrimPrefix(r.URL.Path, pluginFSURLPrefix)
	raw, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "bad path encoding", http.StatusBadRequest)
		return
	}

	resolved, err := p.resolve(string(raw))
	if err != nil {
		// resolve() only returns security errors (ErrPathRelative /
		// ErrPathForbidden / ErrPathDenied); all map to 403 so a probe
		// cannot distinguish "outside root" from "denied basename".
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory", http.StatusForbidden)
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "open failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Cache-Control", "no-store")
	// http.ServeContent emits Content-Type from ext, Accept-Ranges: bytes, and
	// handles If-Modified-Since / Range correctly.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
