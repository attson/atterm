package quicktunnel

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const quickTunnelDomain = ".trycloudflare.com"

var publicURLCandidate = regexp.MustCompile(`https://[^\s"'<>]+`)

// ParsePublicURL accepts only the root of a single-label Cloudflare Quick
// Tunnel hostname. The narrow form prevents suffix confusion and keeps
// credentials, paths, and caller-controlled routing data out of route hints.
func ParsePublicURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid Quick Tunnel URL")
	}
	if parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Port() != "" {
		return "", errors.New("invalid Quick Tunnel URL authority")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", errors.New("Quick Tunnel URL must not contain query or fragment data")
	}
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return "", errors.New("Quick Tunnel URL must use the root path")
	}

	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, quickTunnelDomain) {
		return "", errors.New("unexpected Quick Tunnel hostname")
	}
	label := strings.TrimSuffix(host, quickTunnelDomain)
	if !validDNSLabel(label) {
		return "", errors.New("Quick Tunnel hostname must contain one valid DNS label")
	}
	return "https://" + host, nil
}

func validDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func extractPublicURLs(line string) []string {
	candidates := publicURLCandidate.FindAllString(line, -1)
	urls := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimRight(candidate, ").,;]}!")
		canonical, err := ParsePublicURL(candidate)
		if err == nil {
			urls = append(urls, canonical)
		}
	}
	return urls
}
