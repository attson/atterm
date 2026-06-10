package main

import (
	"encoding/json"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestCollectDiagnostics_NotConfigured(t *testing.T) {
	a := newRelayTestApp(t)
	got := collectDiagnostics(a, "")
	if got.RelayURL != "" {
		t.Errorf("RelayURL: got %q", got.RelayURL)
	}
	if got.RelayStatus != "not_configured" {
		t.Errorf("RelayStatus: got %q want not_configured", got.RelayStatus)
	}
	if got.RelayTokenRedacted != "" {
		t.Errorf("RelayTokenRedacted: got %q want empty", got.RelayTokenRedacted)
	}
	if got.OS != runtime.GOOS {
		t.Errorf("OS: got %q want %q", got.OS, runtime.GOOS)
	}
	if got.Arch != runtime.GOARCH {
		t.Errorf("Arch: got %q want %q", got.Arch, runtime.GOARCH)
	}
	if got.GeneratedAt == "" {
		t.Errorf("GeneratedAt empty")
	}
	if got.WebViewSummary != "" {
		t.Errorf("WebViewSummary: got %q want empty (no UA)", got.WebViewSummary)
	}
}

func TestCollectDiagnostics_ConfiguredRelay_RedactedFields(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{
		RelayURL:           "https://relay.example.com/path?t=secret",
		RelaySessionToken:         "atk_abcdefghijklmnopqrstuvwxyz0123",
		AllowInsecureRelay: false,
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.5 Safari/605.1.15"
	got := collectDiagnostics(a, ua)

	if got.RelayURL != "https://relay.example.com" {
		t.Errorf("RelayURL should be host-only, got %q", got.RelayURL)
	}
	if strings.Contains(got.RelayURL, "secret") {
		t.Errorf("RelayURL leaked query: %q", got.RelayURL)
	}
	if !strings.HasPrefix(got.RelayTokenRedacted, "atk_abcdefgh") {
		t.Errorf("token prefix wrong: %q", got.RelayTokenRedacted)
	}
	if !strings.HasSuffix(got.RelayTokenRedacted, "…") {
		t.Errorf("token should end with …, got %q", got.RelayTokenRedacted)
	}
	if strings.Contains(got.RelayTokenRedacted, "ijklmnop") {
		t.Errorf("token body should be redacted, got %q", got.RelayTokenRedacted)
	}
	if got.WebViewSummary != "WKWebView (Safari/17.5)" {
		t.Errorf("WebViewSummary: got %q", got.WebViewSummary)
	}
}

func TestDiagnosticsPayload_JSONFieldsStable(t *testing.T) {
	want := []string{
		"app_version", "arch", "config", "generated_at",
		"allow_insecure_relay",
		"os", "os_version",
		"recent_relay_errors",
		"relay_status", "relay_token_redacted", "relay_url",
		"remote_permission",
		"uplink_paused",
		"user_agent",
		"webview_summary",
	}
	sort.Strings(want)

	b, err := json.Marshal(DiagnosticsPayload{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys drift:\n got=%v\nwant=%v", got, want)
	}
}
