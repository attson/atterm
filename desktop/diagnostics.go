package main

import (
	"net/url"
	"regexp"
	"runtime"
	"time"
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

// RelayErrorEntry is a single relay-error history record. Timestamps are
// RFC3339 UTC; messages have already been passed through redactErrorLine.
type RelayErrorEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

const maxRelayErrors = 5

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

// ConfigSummary holds the redaction-safe slice of appConfig that's safe to
// share. No paths to secrets, no token values.
type ConfigSummary struct {
	DefaultShell                  string `json:"default_shell"`
	Locale                        string `json:"locale"`
	TerminalTheme                 string `json:"terminal_theme"`
	NotificationsEnabled          bool   `json:"notifications_enabled"`
	ShellIntegrationEnabled       bool   `json:"shell_integration_enabled"`
	WebGLRendererEnabled          bool   `json:"webgl_renderer_enabled"`
	LoggingEnabled                bool   `json:"logging_enabled"`
	LogFilePath                   string `json:"log_file_path"`
	AutoCheckUpdates              bool   `json:"auto_check_updates"`
	CommandNotifyThresholdSeconds int    `json:"command_notify_threshold_seconds"`
}

// DiagnosticsPayload is the JSON shape exposed by App.GetDiagnostics and
// consumed by formatDiagnostics in TypeScript.
type DiagnosticsPayload struct {
	GeneratedAt        string            `json:"generated_at"`
	AppVersion         string            `json:"app_version"`
	OS                 string            `json:"os"`
	Arch               string            `json:"arch"`
	OSVersion          string            `json:"os_version"`
	WebViewSummary     string            `json:"webview_summary"`
	UserAgent          string            `json:"user_agent"`
	RelayURL           string            `json:"relay_url"`
	RelayStatus        string            `json:"relay_status"`
	RelayTokenRedacted string            `json:"relay_token_redacted"`
	AllowInsecureRelay bool              `json:"allow_insecure_relay"`
	RemotePermission   string            `json:"remote_permission"`
	UplinkPaused       bool              `json:"uplink_paused"`
	RecentRelayErrors  []RelayErrorEntry `json:"recent_relay_errors"`
	Config             ConfigSummary     `json:"config"`
}

// collectDiagnostics gathers the runtime state of the desktop App into a
// DiagnosticsPayload. userAgent is the renderer's navigator.userAgent; it
// is recorded raw in UserAgent and also parsed into WebViewSummary.
func collectDiagnostics(a *App, userAgent string) DiagnosticsPayload {
	cfg := a.cfgStore.Get()

	a.mu.Lock()
	connected := a.uplink != nil
	a.mu.Unlock()
	paused := cfg.RelayPaused

	status := "not_configured"
	switch {
	case cfg.RelayURL == "":
		status = "not_configured"
	case paused:
		status = "paused"
	case connected:
		status = "connected"
	default:
		status = "disconnected"
	}

	errs := a.snapshotRelayErrors()
	// Nil-safe: tests that load a zero-init App might never have touched the
	// ring buffer. Marshal-friendly: always emit an array, not null.
	if errs == nil {
		errs = []RelayErrorEntry{}
	}

	return DiagnosticsPayload{
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		AppVersion:         Version,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		OSVersion:          systemVersion(),
		WebViewSummary:     parseWebViewSummary(userAgent),
		UserAgent:          userAgent,
		RelayURL:           redactURL(cfg.RelayURL),
		RelayStatus:        status,
		RelayTokenRedacted: redactToken(cfg.RelayToken),
		AllowInsecureRelay: cfg.AllowInsecureRelay,
		RemotePermission:   cfg.RemotePermissionOrDefault(),
		UplinkPaused:       paused,
		RecentRelayErrors:  errs,
		Config: ConfigSummary{
			DefaultShell:                  cfg.DefaultShellOrDefault(),
			Locale:                        cfg.LocalePreferenceOrDefault(),
			TerminalTheme:                 cfg.TerminalThemeOrDefault(),
			NotificationsEnabled:          cfg.NotificationsEnabledOrDefault(),
			ShellIntegrationEnabled:       cfg.ShellIntegrationEnabledOrDefault(),
			WebGLRendererEnabled:          cfg.WebglRendererEnabledOrDefault(),
			LoggingEnabled:                cfg.LogToFileEnabledOrDefault(),
			LogFilePath:                   cfg.LogFilePathOrDefault(),
			AutoCheckUpdates:              cfg.AutoCheckUpdatesOrDefault(),
			CommandNotifyThresholdSeconds: cfg.CommandNotifyThresholdSecondsOrDefault(),
		},
	}
}
