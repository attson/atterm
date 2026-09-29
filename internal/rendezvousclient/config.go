// Package rendezvousclient defines transport-independent client configuration
// for official and self-hosted Rendezvous deployments.
package rendezvousclient

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	OfficialURL    = "https://rendezvous.atterm.dev"
	DefaultSTUNURL = "stun:stun.cloudflare.com:3478"
	maxSTUNURLs    = 8
	maxURLBytes    = 2048
)

type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeOfficial Mode = "official"
	ModeCustom   Mode = "custom"
)

type STUNMode string

const (
	STUNModeDefault  STUNMode = "default"
	STUNModeCustom   STUNMode = "custom"
	STUNModeDisabled STUNMode = "disabled"
)

// Config is the persisted local selection. Rendezvous and STUN choices are
// reachability preferences and must not enter Peer config replication.
type Config struct {
	Mode     Mode
	URL      string
	STUNMode STUNMode
	STUNURLs []string
}

// ResolvedConfig contains canonical endpoints ready for a client adapter.
// Empty endpoint fields mean Rendezvous is disabled.
type ResolvedConfig struct {
	Mode         Mode
	BaseURL      string
	WebSocketURL string
	HealthURL    string
	STUNMode     STUNMode
	STUNURLs     []string
}

// Endpoint is one canonical service origin and its fixed v1 HTTP/WS paths.
type Endpoint struct {
	BaseURL      string
	WebSocketURL string
	HealthURL    string
	MetricsURL   string
}

// ResolveConfig validates one local selection. Insecure service URLs are only
// accepted for explicit loopback development and never for persisted clients.
func ResolveConfig(cfg Config, allowInsecureLoopback bool) (ResolvedConfig, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeDisabled
	}
	resolved := ResolvedConfig{Mode: mode}
	var rawURL string
	switch mode {
	case ModeDisabled:
	case ModeOfficial:
		rawURL = OfficialURL
	case ModeCustom:
		rawURL = strings.TrimSpace(cfg.URL)
		if rawURL == "" {
			return ResolvedConfig{}, errors.New("rendezvous client: custom URL is required")
		}
	default:
		return ResolvedConfig{}, fmt.Errorf("rendezvous client: unsupported mode %q", mode)
	}
	if rawURL != "" {
		endpoint, err := ParseEndpoint(rawURL, allowInsecureLoopback)
		if err != nil {
			return ResolvedConfig{}, err
		}
		resolved.BaseURL = endpoint.BaseURL
		resolved.WebSocketURL = endpoint.WebSocketURL
		resolved.HealthURL = endpoint.HealthURL
	}

	stunMode := cfg.STUNMode
	if stunMode == "" {
		stunMode = STUNModeDefault
	}
	resolved.STUNMode = stunMode
	switch stunMode {
	case STUNModeDefault:
		resolved.STUNURLs = []string{DefaultSTUNURL}
	case STUNModeDisabled:
		resolved.STUNURLs = []string{}
	case STUNModeCustom:
		urls, err := normalizeSTUNURLs(cfg.STUNURLs)
		if err != nil {
			return ResolvedConfig{}, err
		}
		resolved.STUNURLs = urls
	default:
		return ResolvedConfig{}, fmt.Errorf("rendezvous client: unsupported STUN mode %q", stunMode)
	}
	return resolved, nil
}

// ParseEndpoint accepts a service origin, not an API path. This keeps route
// bundles stable while the versioned HTTP/WS paths remain protocol constants.
func ParseEndpoint(raw string, allowInsecureLoopback bool) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLBytes {
		return Endpoint{}, errors.New("rendezvous client: invalid service URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return Endpoint{}, errors.New("rendezvous client: URL must contain only scheme and host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	insecure := scheme == "http" || scheme == "ws"
	if scheme != "https" && scheme != "wss" && !insecure {
		return Endpoint{}, errors.New("rendezvous client: URL scheme must be HTTPS or WSS")
	}
	if insecure && (!allowInsecureLoopback || !isLoopbackHost(parsed.Hostname())) {
		return Endpoint{}, errors.New("rendezvous client: plaintext is allowed only for explicit loopback development")
	}
	host := strings.ToLower(parsed.Host)
	httpScheme, wsScheme := "https", "wss"
	if insecure {
		httpScheme, wsScheme = "http", "ws"
	}
	base := httpScheme + "://" + host
	return Endpoint{
		BaseURL:      base,
		WebSocketURL: wsScheme + "://" + host + "/v1/connect",
		HealthURL:    base + "/healthz",
		MetricsURL:   base + "/metrics",
	}, nil
}

func normalizeSTUNURLs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxSTUNURLs {
		return nil, fmt.Errorf("rendezvous client: custom STUN URL count must be between 1 and %d", maxSTUNURLs)
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		canonical, err := normalizeSTUNURL(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[canonical]; exists {
			return nil, fmt.Errorf("rendezvous client: duplicate STUN URL %q", canonical)
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

func normalizeSTUNURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLBytes {
		return "", errors.New("rendezvous client: invalid STUN URL")
	}
	parsed, err := url.Parse(raw)
	scheme := strings.ToLower(parsed.Scheme)
	if err != nil || (scheme != "stun" && scheme != "stuns") || parsed.Opaque == "" ||
		parsed.User != nil || parsed.Host != "" || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(parsed.Opaque, "/?#@") {
		return "", errors.New("rendezvous client: only STUN/STUNS URLs without credentials or query are supported")
	}
	probe, err := url.Parse("https://" + parsed.Opaque)
	if err != nil || probe.Hostname() == "" || probe.User != nil || probe.Path != "" {
		return "", errors.New("rendezvous client: invalid STUN host")
	}
	if port := probe.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", errors.New("rendezvous client: invalid STUN port")
		}
	}
	return scheme + ":" + strings.ToLower(probe.Host), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
