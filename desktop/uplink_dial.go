package main

import "strings"

// uplinkDialURL returns the ws(s) base URL to dial for /uplink. When the user
// has a home instance (multi-instance node selection), the stateful uplink
// routes there: the home is an http(s) public URL, converted to ws(s). When
// empty (single-instance / register / dead-home), it falls back to relayURL
// (already ws/wss). The trailing "/uplink" is appended by the uplink dialer.
func uplinkDialURL(homeInstanceURL, relayURL string) string {
	h := strings.TrimRight(strings.TrimSpace(homeInstanceURL), "/")
	if h == "" {
		return relayURL
	}
	switch {
	case strings.HasPrefix(h, "https://"):
		return "wss://" + strings.TrimPrefix(h, "https://")
	case strings.HasPrefix(h, "http://"):
		return "ws://" + strings.TrimPrefix(h, "http://")
	case strings.HasPrefix(h, "wss://"), strings.HasPrefix(h, "ws://"):
		return h
	default:
		return "wss://" + h
	}
}
