package main

import (
	"net/url"
	"regexp"
)

// redactToken returns the first 12 characters of an API token followed by "…".
// For atk_ tokens this yields "atk_AbCdEfGh…" — enough to recognise the token
// in a log line, not enough to authenticate. Empty input returns empty.
func redactToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 12 {
		return s + "…"
	}
	return s[:12] + "…"
}

// redactURL returns scheme://host[:port] only — drops path, query, and
// fragment so URLs that carry tokens in ?t=… are stripped before display.
func redactURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return "(invalid url)"
	}
	return parsed.Scheme + "://" + parsed.Host
}

var (
	tokenRE  = regexp.MustCompile(`atk_[A-Za-z0-9_-]{8,}`)
	authRE   = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
	cookieRE = regexp.MustCompile(`(?i)(cookie\s*:\s*)\S+`)
)

// redactErrorLine masks API tokens, Authorization headers, and Cookie headers
// in a free-form error message.
func redactErrorLine(s string) string {
	s = tokenRE.ReplaceAllStringFunc(s, redactToken)
	s = authRE.ReplaceAllString(s, "${1}[redacted]")
	s = cookieRE.ReplaceAllString(s, "${1}[redacted]")
	return s
}

var (
	safariRE    = regexp.MustCompile(`Version/(\S+)\s+Safari`)
	edgeRE      = regexp.MustCompile(`Edg/(\S+)`)
	webkitGTKRE = regexp.MustCompile(`(?:^|[^e])WebKit/(\S+)`)
)

// parseWebViewSummary extracts a WebView identifier + version from a user
// agent string. Returns the raw UA when no known pattern matches; returns
// empty string for empty input.
func parseWebViewSummary(ua string) string {
	if ua == "" {
		return ""
	}
	if m := safariRE.FindStringSubmatch(ua); len(m) > 1 {
		return "WKWebView (Safari/" + m[1] + ")"
	}
	if m := edgeRE.FindStringSubmatch(ua); len(m) > 1 {
		return "WebView2 (Edg/" + m[1] + ")"
	}
	if m := webkitGTKRE.FindStringSubmatch(ua); len(m) > 1 {
		return "WebKitGTK (WebKit/" + m[1] + ")"
	}
	return ua
}
