// Package relay — health endpoints. /healthz is public minimal; the
// /admin/health* endpoints are admin-only and emit a redaction-safe
// snapshot of operator-relevant runtime state.
package relay

import "strings"

// isMobileOriginCompatible reports whether the configured origin allow-list
// admits a mobile webview (Capacitor / Ionic / iOS WKWebView) origin. An
// empty list means "any origin is allowed" — technically compatible but
// also a security warning the caller surfaces separately.
func isMobileOriginCompatible(origins []string) bool {
	if len(origins) == 0 {
		return true
	}
	for _, o := range origins {
		switch {
		case strings.HasPrefix(o, "capacitor://"):
			return true
		case strings.HasPrefix(o, "ionic://"):
			return true
		case strings.HasPrefix(o, "https://localhost"):
			return true
		case o == "null":
			return true
		}
	}
	return false
}
