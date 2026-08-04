package relay

import (
	"net/url"
	"strings"
)

// currentAllowedOrigins returns a snapshot of the hot-reloadable Origin
// allow-list. Never returns nil (empty slice = allow any origin / dev mode).
func (s *Server) currentAllowedOrigins() []string {
	if p := s.allowedOrigins.Load(); p != nil {
		return *p
	}
	return nil
}

// SetAllowedOrigins hot-swaps the WS/HTTP Origin allow-list. An empty slice
// reverts to "allow any origin". Safe to call at runtime. The stored value is
// taken verbatim — callers that need desktop-webview hosts appended should
// pass the result of OriginPatterns.
func (s *Server) SetAllowedOrigins(origins []string) {
	cp := append([]string(nil), origins...)
	s.allowedOrigins.Store(&cp)
}

// relayDesktopWebviewOriginHosts mirror the packaged-Wails asset hosts so a
// desktop client keeps matching after an admin edits origins at runtime.
var relayDesktopWebviewOriginHosts = []string{"wails", "wails.localhost", "wails.localhost:*"}

// OriginPatterns normalizes user-supplied origins to host patterns and appends
// the desktop webview hosts. Empty input → nil (allow any origin / dev mode).
// Mirrors the bootstrap's allowedOriginHosts so admin edits stay consistent.
func OriginPatterns(origins []string) []string {
	clean := make([]string, 0, len(origins))
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			clean = append(clean, o)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	out := make([]string, 0, len(clean)+len(relayDesktopWebviewOriginHosts))
	for _, o := range clean {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			o = u.Host
		}
		out = appendUniqueOrigin(out, o)
	}
	for _, h := range relayDesktopWebviewOriginHosts {
		out = appendUniqueOrigin(out, h)
	}
	return out
}
