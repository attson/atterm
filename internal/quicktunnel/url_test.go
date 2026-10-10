package quicktunnel

import (
	"reflect"
	"testing"
)

func TestParsePublicURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "generated", raw: "https://quiet-field.trycloudflare.com", want: "https://quiet-field.trycloudflare.com"},
		{name: "root slash canonicalized", raw: "https://quiet-field.trycloudflare.com/", want: "https://quiet-field.trycloudflare.com"},
		{name: "uppercase canonicalized", raw: "https://QUIET-FIELD.trycloudflare.com", want: "https://quiet-field.trycloudflare.com"},
		{name: "http", raw: "http://quiet-field.trycloudflare.com"},
		{name: "userinfo", raw: "https://user@quiet-field.trycloudflare.com"},
		{name: "port", raw: "https://quiet-field.trycloudflare.com:443"},
		{name: "query", raw: "https://quiet-field.trycloudflare.com?token=x"},
		{name: "fragment", raw: "https://quiet-field.trycloudflare.com#x"},
		{name: "path", raw: "https://quiet-field.trycloudflare.com/connect"},
		{name: "bare domain", raw: "https://trycloudflare.com"},
		{name: "nested label", raw: "https://nested.quiet-field.trycloudflare.com"},
		{name: "suffix confusion", raw: "https://quiet-field.trycloudflare.com.evil.example"},
		{name: "leading hyphen", raw: "https://-quiet.trycloudflare.com"},
		{name: "trailing hyphen", raw: "https://quiet-.trycloudflare.com"},
		{name: "wildcard", raw: "https://*.trycloudflare.com"},
		{name: "underscore", raw: "https://quiet_field.trycloudflare.com"},
		{name: "unicode", raw: "https://quiet-场.trycloudflare.com"},
		{name: "empty", raw: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePublicURL(tt.raw)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("ParsePublicURL(%q) unexpectedly succeeded with %q", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePublicURL(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePublicURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractPublicURLs(t *testing.T) {
	t.Parallel()

	line := `2026-09-28T08:00:00Z INF Your quick Tunnel has been created! Visit it at (https://quiet-field.trycloudflare.com), ignore https://nested.bad.trycloudflare.com and https://good.trycloudflare.com/path`
	got := extractPublicURLs(line)
	want := []string{"https://quiet-field.trycloudflare.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractPublicURLs() = %#v, want %#v", got, want)
	}
}
