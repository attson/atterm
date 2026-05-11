package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func validateRelayEndpoint(raw string, allowInsecure bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid relay url: %w", err)
	}
	switch u.Scheme {
	case "wss":
		return nil
	case "ws":
		if allowInsecure || isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("insecure relay url %q uses ws://; enable insecure mode to allow cleartext remote relay connections", raw)
	default:
		return fmt.Errorf("relay url must start with ws:// or wss://")
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
