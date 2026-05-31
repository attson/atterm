package main

import (
	"strings"
	"testing"
)

func TestRedactToken_Cases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"short", "atk_a", "atk_a…"},
		{"exactly_12", "atk_12345678", "atk_12345678…"},
		{"long", "atk_abcdefghijklmnopqrstuvwxyz", "atk_abcdefgh…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactToken(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRedactURL_Cases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"https_with_query", "https://relay.example.com/x?t=abc", "https://relay.example.com"},
		{"http_with_port", "http://localhost:8080/api", "http://localhost:8080"},
		{"wss", "wss://relay.example.com/uplink", "wss://relay.example.com"},
		{"malformed", "not-a-url", "(invalid url)"},
		{"empty_host", "https://", "(invalid url)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURL(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRedactErrorLine_StripsTokens(t *testing.T) {
	in := "dial failed: atk_abcdefghijkl sent in header"
	got := redactErrorLine(in)
	if !strings.Contains(got, "atk_abcdefgh…") {
		t.Fatalf("expected redacted token, got %q", got)
	}
	if strings.Contains(got, "ijkl") {
		t.Fatalf("token body should be redacted, got %q", got)
	}
}

func TestRedactErrorLine_StripsAuthHeader(t *testing.T) {
	in := `HTTP 401 Authorization: Bearer atk_secret_value`
	got := redactErrorLine(in)
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected [redacted], got %q", got)
	}
	if strings.Contains(got, "secret_value") {
		t.Fatalf("authorization body should be redacted, got %q", got)
	}
}

func TestRedactErrorLine_StripsCookieHeader(t *testing.T) {
	in := `failed Cookie: atterm_session=abc123def`
	got := redactErrorLine(in)
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected [redacted], got %q", got)
	}
	if strings.Contains(got, "abc123def") {
		t.Fatalf("cookie value should be redacted, got %q", got)
	}
}

func TestParseWebViewSummary_Cases(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want string
	}{
		{"safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "WKWebView (Safari/17.5)"},
		{"edge", "Mozilla/5.0 Edg/120.0.2210.91", "WebView2 (Edg/120.0.2210.91)"},
		{"webkitgtk", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 WebKit/2.42.5", "WebKitGTK (WebKit/2.42.5)"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseWebViewSummary(tc.ua); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
