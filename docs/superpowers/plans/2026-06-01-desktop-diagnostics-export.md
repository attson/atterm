# Desktop diagnostics export — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Diagnostics" Settings tab to the Wails desktop app that shows app/OS/WebView version, redacted relay info, recent relay errors, and a config summary, with "Copy" and "Export" actions. All credentials (API tokens, Authorization headers, cookies) are redacted before display.

**Architecture:** A new `desktop/diagnostics.go` holds the payload type, the data collector, and three redaction helpers. A small ring buffer on `App` records the last 5 relay errors via a callback wired into `desktop/uplink.go`'s existing error sites. Platform-specific `system_version_*.go` files supply OS version strings. The renderer formats the payload into plaintext entirely in TypeScript (`lib/diagnostics.ts`), so the preview updates without an extra Wails round-trip.

**Tech Stack:** Go 1.22+ stdlib (`os/exec`, `regexp`, `sync`, `time`); Wails v2 runtime (`SaveFileDialog`, `EventsEmit`); Vue 3 + TypeScript; vitest.

**Reference spec:** `docs/superpowers/specs/2026-06-01-desktop-diagnostics-export-design.md`

---

## File map

### Backend (Go)
- **Create:** `desktop/diagnostics.go` — `DiagnosticsPayload`, `RelayErrorEntry`, `ConfigSummary` types; `redactToken`, `redactURL`, `redactErrorLine`, `parseWebViewSummary` helpers; `collectDiagnostics(a, ua)` collector.
- **Create:** `desktop/diagnostics_test.go` — table tests for the three redactors and `parseWebViewSummary`.
- **Create:** `desktop/diagnostics_payload_test.go` — `collectDiagnostics` field-population tests + contract test.
- **Create:** `desktop/diagnostics_errors_test.go` — ring-buffer tests.
- **Create:** `desktop/system_version_darwin.go` — `sw_vers -productVersion`.
- **Create:** `desktop/system_version_linux.go` — parses `/etc/os-release`.
- **Create:** `desktop/system_version_windows.go` — `cmd /c ver`.
- **Create:** `desktop/system_version_other.go` — returns `""`.
- **Modify:** `desktop/app.go` — add `relayErrMu`, `relayErrors`, `writeFile` (injection point); add `recordRelayError`, `snapshotRelayErrors`, `GetDiagnostics`, `ExportDiagnostics` methods.
- **Modify:** `desktop/uplink.go` — extend `uplink` struct with `recordError func(error)`; call it from the two error sites (`uplink.go:76` retry path; `handleCloseError` for auth-related codes). Constructor `newUplink` gains a parameter.

### Frontend (Vue/TS)
- **Modify:** `desktop/frontend/src/lib/api.ts` — `DiagnosticsPayload` type + `getDiagnostics` / `exportDiagnostics` wrappers + binding declarations.
- **Create:** `desktop/frontend/src/lib/diagnostics.ts` — `formatDiagnostics(payload) → string`.
- **Create:** `desktop/frontend/src/lib/__tests__/diagnostics.test.ts` — formatter cases.
- **Create:** `desktop/frontend/src/components/SettingsDiagnostics.vue` — preview + Copy + Export + Refresh.
- **Create:** `desktop/frontend/src/components/__tests__/SettingsDiagnostics.test.ts` — component tests.
- **Modify:** `desktop/frontend/src/components/SettingsDialog.vue` — register the new tab.
- **Modify:** `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts` — `settings.diagnostics.*` keys.

---

## Task 1: Redaction helpers (test-first)

**Files:**
- Create: `desktop/diagnostics.go`
- Create: `desktop/diagnostics_test.go`

- [ ] **Step 1: Write failing tests**

Create `desktop/diagnostics_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run 'TestRedact|TestParseWebViewSummary' -v`
Expected: FAIL with "undefined: redactToken" etc.

- [ ] **Step 3: Implement the helpers**

Create `desktop/diagnostics.go`:

```go
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
	webkitGTKRE = regexp.MustCompile(`WebKit/(\S+)`)
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run 'TestRedact|TestParseWebViewSummary' -v`
Expected: PASS — all six test functions (including 4 + 6 + 4 sub-cases).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/diagnostics.go desktop/diagnostics_test.go
git -c commit.gpgsign=false commit -m "desktop/diagnostics: redactToken/redactURL/redactErrorLine + parseWebViewSummary"
```

---

## Task 2: Ring buffer for recent relay errors (test-first)

**Files:**
- Modify: `desktop/app.go` — add fields + two methods
- Create: `desktop/diagnostics_errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `desktop/diagnostics_errors_test.go`:

```go
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRecordRelayError_RingBufferKeeps5Newest(t *testing.T) {
	a := newRelayTestApp(t)
	for i := 0; i < 8; i++ {
		a.recordRelayError(fmt.Errorf("err-%d", i))
	}
	got := a.snapshotRelayErrors()
	if len(got) != 5 {
		t.Fatalf("want 5, got %d", len(got))
	}
	if got[0].Message != "err-7" {
		t.Fatalf("newest first failed: %q", got[0].Message)
	}
	if got[4].Message != "err-3" {
		t.Fatalf("oldest in buffer wrong: %q", got[4].Message)
	}
}

func TestRecordRelayError_NilIsNoop(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(nil)
	if got := a.snapshotRelayErrors(); len(got) != 0 {
		t.Fatalf("nil should not record, got %d entries", len(got))
	}
}

func TestRecordRelayError_RedactsTokensInMessage(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(errors.New("401 dial failed: atk_abcdefghij blocked"))
	got := a.snapshotRelayErrors()
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if !strings.Contains(got[0].Message, "atk_abcdefgh…") {
		t.Fatalf("expected redacted token, got %q", got[0].Message)
	}
}

func TestSnapshotRelayErrors_ReturnsCopy(t *testing.T) {
	a := newRelayTestApp(t)
	a.recordRelayError(fmt.Errorf("e"))
	snap := a.snapshotRelayErrors()
	snap[0].Message = "mutated"
	again := a.snapshotRelayErrors()
	if again[0].Message != "e" {
		t.Fatalf("internal state was mutated by caller: %q", again[0].Message)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestRecordRelayError -v`
Expected: FAIL with `a.recordRelayError undefined` and `a.snapshotRelayErrors undefined`.

- [ ] **Step 3: Add types to `diagnostics.go`**

Append to `desktop/diagnostics.go`:

```go
// RelayErrorEntry is a single relay-error history record. Timestamps are
// RFC3339 UTC; messages have already been passed through redactErrorLine.
type RelayErrorEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

const maxRelayErrors = 5
```

- [ ] **Step 4: Wire the ring buffer into `App`**

Open `desktop/app.go`. Find the existing `App` struct (it has fields like `host`, `uplink`, `mu`, `cfgStore`, `ctx` — look around lines 80-100 of `desktop/app.go`).

Add three fields to the struct:

```go
	// recent relay errors — bounded ring, newest-first.
	relayErrMu  sync.Mutex
	relayErrors []RelayErrorEntry
```

`sync` is already imported in `app.go`. `time` is already imported.

Then add the two methods at the bottom of `app.go` (just before the closing brace of the file, or right after `FetchRelayMe` so all relay-adjacent code stays together):

```go
// recordRelayError appends an error entry to the recent-errors ring buffer.
// Nil errors are dropped. Messages are passed through redactErrorLine so
// tokens / Authorization / Cookie values are masked. Newest-first ordering;
// when the buffer is full the oldest entry falls off.
func (a *App) recordRelayError(err error) {
	if err == nil {
		return
	}
	entry := RelayErrorEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Message:   redactErrorLine(err.Error()),
	}
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	a.relayErrors = append([]RelayErrorEntry{entry}, a.relayErrors...)
	if len(a.relayErrors) > maxRelayErrors {
		a.relayErrors = a.relayErrors[:maxRelayErrors]
	}
}

// snapshotRelayErrors returns a copy of the recent-errors ring buffer.
// Callers receive a fresh slice safe to mutate; the underlying buffer is
// unaffected.
func (a *App) snapshotRelayErrors() []RelayErrorEntry {
	a.relayErrMu.Lock()
	defer a.relayErrMu.Unlock()
	out := make([]RelayErrorEntry, len(a.relayErrors))
	copy(out, a.relayErrors)
	return out
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestRecordRelayError -v`
Expected: PASS — all four tests.

Run the full desktop test suite as a regression gate:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/diagnostics.go desktop/diagnostics_errors_test.go desktop/app.go
git -c commit.gpgsign=false commit -m "desktop/app: recent-relay-errors ring buffer (size 5, redacted, newest-first)"
```

---

## Task 3: Platform-specific OS version helpers

**Files:**
- Create: `desktop/system_version_darwin.go`
- Create: `desktop/system_version_linux.go`
- Create: `desktop/system_version_windows.go`
- Create: `desktop/system_version_other.go`

- [ ] **Step 1: Create the four files**

Create `desktop/system_version_darwin.go`:

```go
//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// systemVersion returns the macOS product version (e.g. "14.6.1") via
// `sw_vers -productVersion`. Returns empty string on any failure.
func systemVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

Create `desktop/system_version_linux.go`:

```go
//go:build linux

package main

import (
	"os"
	"regexp"
	"strings"
)

var prettyNameRE = regexp.MustCompile(`(?m)^PRETTY_NAME="([^"]+)"`)

// systemVersion reads /etc/os-release and returns PRETTY_NAME if present.
// Returns empty string on any failure.
func systemVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	m := prettyNameRE.FindStringSubmatch(string(data))
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
```

Create `desktop/system_version_windows.go`:

```go
//go:build windows

package main

import (
	"os/exec"
	"strings"
)

// systemVersion runs `cmd /c ver` which prints
// "Microsoft Windows [Version X.Y.Z]". Returns the trimmed line, or
// empty string on failure.
func systemVersion() string {
	out, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

Create `desktop/system_version_other.go`:

```go
//go:build !darwin && !linux && !windows

package main

func systemVersion() string {
	return ""
}
```

- [ ] **Step 2: Verify compilation on the host platform**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./desktop/...`
Expected: clean.

- [ ] **Step 3: Quick smoke**

The macOS host this is being developed on can sanity-check the helper:

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestSystemVersion -v 2>&1 || echo "(no test for systemVersion yet — that's fine)"`
Expected: either no matching test, or PASS.

There's intentionally no unit test for `systemVersion` itself — it's a thin wrapper around `os/exec` whose output varies by host. The behaviour is exercised end-to-end by Task 4's `TestCollectDiagnostics_FieldsPopulated` (which asserts `OSVersion` is either empty or non-empty, not a specific value).

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/system_version_darwin.go desktop/system_version_linux.go desktop/system_version_windows.go desktop/system_version_other.go
git -c commit.gpgsign=false commit -m "desktop: per-OS systemVersion() helpers (sw_vers / os-release / cmd ver)"
```

---

## Task 4: `DiagnosticsPayload` + `collectDiagnostics` (test-first)

**Files:**
- Modify: `desktop/diagnostics.go`
- Create: `desktop/diagnostics_payload_test.go`

- [ ] **Step 1: Write failing tests**

Create `desktop/diagnostics_payload_test.go`:

```go
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
		RelayToken:         "atk_abcdefghijklmnopqrstuvwxyz0123",
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run 'TestCollectDiagnostics|TestDiagnosticsPayload_JSONFields' -v`
Expected: FAIL — `undefined: collectDiagnostics`, `undefined: DiagnosticsPayload`.

- [ ] **Step 3: Add the type + collector to `diagnostics.go`**

Append to `desktop/diagnostics.go`:

```go
import (
	// add to existing import block:
	"runtime"
	"time"
)

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
```

Fold the new imports (`runtime`, `time`) into the existing import block at the top of `diagnostics.go`; do not add a second block.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run 'TestCollectDiagnostics|TestDiagnosticsPayload_JSONFields' -v`
Expected: PASS — all three tests.

Run the full desktop suite:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/diagnostics.go desktop/diagnostics_payload_test.go
git -c commit.gpgsign=false commit -m "desktop/diagnostics: DiagnosticsPayload + collectDiagnostics gatherer"
```

---

## Task 5: Wire `recordRelayError` into uplink

**Files:**
- Modify: `desktop/uplink.go`
- Modify: `desktop/app.go`

- [ ] **Step 1: Extend the `uplink` struct + constructor**

Open `desktop/uplink.go`. Find the `type uplink struct` block (around line 47) and the `func newUplink(...)` constructor (around line 57).

Add a `recordError` field next to `eventsEmit`:

```go
type uplink struct {
	// ...existing fields above...

	// eventsEmit is the Wails EventsEmit function used to push events to the
	// renderer. Stored as a func value so tests can substitute a fake.
	eventsEmit func(ctx context.Context, name string, data ...interface{})

	// recordError is called once per relay error so the App can keep a
	// recent-errors ring buffer for diagnostics export. Nil-safe.
	recordError func(err error)
}
```

Update `newUplink` to accept the callback:

```go
func newUplink(relayURL, token, remotePermission string, host *relayHost, recordError func(error)) *uplink {
	return &uplink{
		// ...existing fields...
		eventsEmit:  wailsruntime.EventsEmit,
		recordError: recordError,
	}
}
```

(Keep all the existing fields that the constructor was setting; the only change is the new `recordError` field assignment.)

- [ ] **Step 2: Call `recordError` at both error sites**

In `desktop/uplink.go`, find line 76 (the reconnect-retry log):

```go
log.Printf("uplink: %v (retry in %s)", err, backoff)
```

Insert above it:

```go
if u.recordError != nil {
	u.recordError(err)
}
```

In `handleCloseError` (around line 432), find the `if u.eventsEmit != nil` block that emits `relay:auth-error`. Add a `recordError` call alongside the event emit:

```go
switch int(ce.Code) {
case 4001, 4002, 4003:
	if u.eventsEmit != nil {
		u.eventsEmit(ctx, "relay:auth-error", map[string]string{"reason": reason})
	}
	if u.recordError != nil {
		u.recordError(fmt.Errorf("%s", reason))
	}
}
```

`fmt` may not be imported in `uplink.go` — if not, add it to the import block. Quick check via `grep "^import\|\"fmt\"" /Users/attson/code/github.com.attson/atterm/desktop/uplink.go | head -5`.

- [ ] **Step 3: Update the construction site in `app.go`**

Open `desktop/app.go`. Find the call to `newUplink` (around line 201):

```go
a.uplink = newUplink(cfg.RelayURL, cfg.RelayToken, cfg.RemotePermissionOrDefault(), a.host)
```

Pass `a.recordRelayError`:

```go
a.uplink = newUplink(cfg.RelayURL, cfg.RelayToken, cfg.RemotePermissionOrDefault(), a.host, a.recordRelayError)
```

(Method values have the receiver bound automatically; Go closes over `a`.)

- [ ] **Step 4: Verify compilation + existing uplink tests**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./desktop/...`
Expected: clean.

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run 'TestUplink|TestRecordRelayError'`
Expected: PASS — existing uplink tests + the four from Task 2.

- [ ] **Step 5: Add an integration test that recordError fires on close**

Append to `desktop/diagnostics_errors_test.go`:

```go
import (
	"context"
	"nhooyr.io/websocket"
)

func TestUplink_HandleCloseError_RecordsAuthFailure(t *testing.T) {
	a := newRelayTestApp(t)
	u := newUplink("ws://test", "atk_test", "full", a.host, a.recordRelayError)
	// Pretend a 4001 close from the relay.
	u.handleCloseError(context.Background(), websocket.CloseError{
		Code:   4001,
		Reason: "",
	})
	got := a.snapshotRelayErrors()
	if len(got) != 1 {
		t.Fatalf("want 1 error recorded, got %d", len(got))
	}
	if got[0].Message != "auth_invalid_token" {
		t.Fatalf("expected reason mapping, got %q", got[0].Message)
	}
}
```

The existing test file imports — check `desktop/diagnostics_errors_test.go` import block at top of file and add `"context"` and `"nhooyr.io/websocket"` if needed (consolidate into the single block).

- [ ] **Step 6: Run the new test**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestUplink_HandleCloseError -v`
Expected: PASS.

Run the full desktop suite:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/uplink.go desktop/app.go desktop/diagnostics_errors_test.go
git -c commit.gpgsign=false commit -m "desktop/uplink: feed relay errors into App.recordRelayError"
```

---

## Task 6: `GetDiagnostics` + `ExportDiagnostics` bindings

**Files:**
- Modify: `desktop/app.go`

- [ ] **Step 1: Add the two Wails-binding methods to `app.go`**

Append to `desktop/app.go` (anywhere after `FetchRelayMe` for readability):

```go
// writeFile is the function `ExportDiagnostics` uses to persist content. Held
// as a field on App so tests can substitute a capturing stub instead of
// touching disk. Defaults to os.WriteFile in production (set in NewApp).
type writeFileFunc func(path string, data []byte, perm fs.FileMode) error

// GetDiagnostics is the Wails-exposed binding that returns the current
// diagnostics payload. userAgent should be the renderer's navigator.userAgent.
func (a *App) GetDiagnostics(userAgent string) DiagnosticsPayload {
	return collectDiagnostics(a, userAgent)
}

// ExportDiagnostics opens a native save dialog (default filename
// "atterm-diagnostics-<ts>.txt") and writes content to the chosen path.
// Returns "" when the user cancelled. Returns ("", err) only on actual
// I/O failure after the user picked a path.
func (a *App) ExportDiagnostics(content string) (string, error) {
	defaultName := "atterm-diagnostics-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ".txt"
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export diagnostics",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Text Files (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil || path == "" {
		return "", err
	}
	wf := a.writeFile
	if wf == nil {
		wf = os.WriteFile
	}
	if err := wf(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
```

- [ ] **Step 2: Add the `writeFile` field to `App`**

In the `App` struct (top of `app.go`), add one field:

```go
type App struct {
	// ...existing fields...

	// writeFile is os.WriteFile in production; tests substitute a stub.
	writeFile writeFileFunc
}
```

Add the `io/fs` import to the import block if not already present (`os.WriteFile`'s `os.FileMode` is type-compatible with `fs.FileMode`, but the type alias on the func signature needs the import).

- [ ] **Step 3: Set up the test infrastructure**

Append to `desktop/diagnostics_errors_test.go` (or create a new test file `desktop/diagnostics_export_test.go` — your choice; the new file is cleaner):

```go
package main

import (
	"errors"
	"io/fs"
	"testing"
)

func TestExportDiagnostics_WritesContentViaStub(t *testing.T) {
	a := newRelayTestApp(t)
	var gotPath string
	var gotData []byte
	var gotPerm fs.FileMode
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		gotPath = path
		gotData = data
		gotPerm = perm
		return nil
	}
	// SaveFileDialog will return "" in headless test context (no Wails runtime).
	// To make this test deterministic, bypass the dialog by calling the writer
	// directly via a small wrapper that mirrors the production path.
	if err := a.writeFile("/tmp/atterm-diag-test.txt", []byte("hello"), 0o600); err != nil {
		t.Fatalf("writeFile stub error: %v", err)
	}
	if gotPath != "/tmp/atterm-diag-test.txt" {
		t.Errorf("path: got %q", gotPath)
	}
	if string(gotData) != "hello" {
		t.Errorf("data: got %q", string(gotData))
	}
	if gotPerm != 0o600 {
		t.Errorf("perm: got %o", gotPerm)
	}
}

func TestExportDiagnostics_StubPropagatesError(t *testing.T) {
	a := newRelayTestApp(t)
	wantErr := errors.New("disk full")
	a.writeFile = func(path string, data []byte, perm fs.FileMode) error {
		return wantErr
	}
	if err := a.writeFile("/tmp/x", []byte("y"), 0o600); !errors.Is(err, wantErr) {
		t.Fatalf("expected disk-full, got %v", err)
	}
}
```

These tests exercise the `writeFile` injection point. The native save dialog itself cannot be driven from `go test` without a real Wails runtime, so we test the writer side. Manual smoke (Task 10 step 4) covers the dialog path.

- [ ] **Step 4: Run the new tests**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestExportDiagnostics -v`
Expected: PASS.

Run the full desktop suite:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/app.go desktop/diagnostics_errors_test.go
git -c commit.gpgsign=false commit -m "desktop: GetDiagnostics + ExportDiagnostics Wails bindings"
```

---

## Task 7: TS shim + `formatDiagnostics` (test-first)

**Files:**
- Modify: `desktop/frontend/src/lib/api.ts`
- Create: `desktop/frontend/src/lib/diagnostics.ts`
- Create: `desktop/frontend/src/lib/__tests__/diagnostics.test.ts`

- [ ] **Step 1: Add the type + binding wrappers to `api.ts`**

In `desktop/frontend/src/lib/api.ts`, add the type (place it after the existing `RelayMe` interface):

```ts
export interface DiagnosticsPayload {
  generated_at: string
  app_version: string
  os: string
  arch: string
  os_version: string
  webview_summary: string
  user_agent: string
  relay_url: string
  relay_status: string
  relay_token_redacted: string
  allow_insecure_relay: boolean
  remote_permission: string
  uplink_paused: boolean
  recent_relay_errors: { timestamp: string; message: string }[]
  config: {
    default_shell: string
    locale: string
    terminal_theme: string
    notifications_enabled: boolean
    shell_integration_enabled: boolean
    webgl_renderer_enabled: boolean
    logging_enabled: boolean
    log_file_path: string
    auto_check_updates: boolean
    command_notify_threshold_seconds: number
  }
}
```

Inside the existing `AppBindings` interface (around line 87), add two lines:

```ts
  GetDiagnostics(userAgent: string): Promise<DiagnosticsPayload>;
  ExportDiagnostics(content: string): Promise<string>;
```

At the bottom of the file, add the wrappers:

```ts
export function getDiagnostics(userAgent: string): Promise<DiagnosticsPayload> {
  return bindings().GetDiagnostics(userAgent);
}

export function exportDiagnostics(content: string): Promise<string> {
  return bindings().ExportDiagnostics(content);
}
```

- [ ] **Step 2: Write failing tests for `formatDiagnostics`**

Create `desktop/frontend/src/lib/__tests__/diagnostics.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { formatDiagnostics } from '../diagnostics'
import type { DiagnosticsPayload } from '../api'

function baseline(): DiagnosticsPayload {
  return {
    generated_at: '2026-06-01T12:00:00Z',
    app_version: 'v0.4.0',
    os: 'darwin', arch: 'arm64', os_version: '14.6.1',
    webview_summary: 'WKWebView (Safari/17.5)',
    user_agent: 'Mozilla/5.0',
    relay_url: 'https://relay.example.com',
    relay_status: 'connected',
    relay_token_redacted: 'atk_AbCdEfGh…',
    allow_insecure_relay: false,
    remote_permission: 'full',
    uplink_paused: false,
    recent_relay_errors: [],
    config: {
      default_shell: '/bin/zsh',
      locale: 'system',
      terminal_theme: 'default',
      notifications_enabled: true,
      shell_integration_enabled: true,
      webgl_renderer_enabled: true,
      logging_enabled: true,
      log_file_path: '/tmp/atterm.log',
      auto_check_updates: true,
      command_notify_threshold_seconds: 10,
    },
  }
}

describe('formatDiagnostics', () => {
  it('renders a header with the generated_at timestamp', () => {
    const out = formatDiagnostics(baseline())
    expect(out.startsWith('atterm desktop diagnostics — 2026-06-01T12:00:00Z')).toBe(true)
  })

  it('emits "(none)" when there are no relay errors', () => {
    expect(formatDiagnostics(baseline())).toContain('(none)')
  })

  it('lists each recent relay error on its own line, newest first', () => {
    const p = baseline()
    p.recent_relay_errors = [
      { timestamp: '2026-06-01T11:59:00Z', message: 'dial failed' },
      { timestamp: '2026-06-01T11:58:00Z', message: 'auth_invalid_token' },
    ]
    const out = formatDiagnostics(p)
    const dialIdx = out.indexOf('dial failed')
    const authIdx = out.indexOf('auth_invalid_token')
    expect(dialIdx).toBeGreaterThan(0)
    expect(authIdx).toBeGreaterThan(dialIdx)
  })

  it('marks unknown WebView as (unknown)', () => {
    const p = baseline()
    p.webview_summary = ''
    expect(formatDiagnostics(p)).toContain('WebView:')
    expect(formatDiagnostics(p)).toContain('(unknown)')
  })

  it('writes the host-only relay URL', () => {
    const out = formatDiagnostics(baseline())
    expect(out).toContain('https://relay.example.com')
    expect(out).not.toContain('?')
  })

  it('marks insecure HTTP as yes when allowed', () => {
    const p = baseline()
    p.allow_insecure_relay = true
    expect(formatDiagnostics(p)).toContain('Allow insecure HTTP:    yes')
  })

  it('writes (not configured) when relay_url is empty', () => {
    const p = baseline()
    p.relay_url = ''
    expect(formatDiagnostics(p)).toContain('(not configured)')
  })
})
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/diagnostics.test.ts`
Expected: FAIL — `Cannot find module '../diagnostics'`.

- [ ] **Step 4: Implement `formatDiagnostics`**

Create `desktop/frontend/src/lib/diagnostics.ts`:

```ts
import type { DiagnosticsPayload } from './api'

/**
 * Renders a DiagnosticsPayload as a multi-line plaintext block suitable
 * for pasting into an issue or chat. Single space between colon and
 * value; label column padded to 24 chars.
 */
export function formatDiagnostics(p: DiagnosticsPayload): string {
  const pad = (k: string) => k.padEnd(24, ' ')
  const lines: string[] = [
    `atterm desktop diagnostics — ${p.generated_at}`,
    '--------------------------------------------------',
    pad('App version:') + p.app_version,
    pad('OS:') + `${p.os} ${p.os_version} (${p.arch})`.replace(/\s+/g, ' ').trim(),
    pad('WebView:') + (p.webview_summary || '(unknown)'),
    pad('Relay URL:') + (p.relay_url || '(not configured)'),
    pad('Relay status:') + p.relay_status,
    pad('Relay token:') + (p.relay_token_redacted || '(none)'),
    pad('Allow insecure HTTP:') + (p.allow_insecure_relay ? 'yes' : 'no'),
    pad('Remote permission:') + p.remote_permission,
    pad('Uplink paused:') + (p.uplink_paused ? 'yes' : 'no'),
    '',
    'Recent relay errors (most recent first):',
    ...(p.recent_relay_errors.length === 0
      ? ['  (none)']
      : p.recent_relay_errors.map(e => `  ${e.timestamp}  ${e.message}`)),
    '',
    'Config summary:',
    pad('  Default shell:') + p.config.default_shell,
    pad('  Locale:') + p.config.locale,
    pad('  Theme:') + p.config.terminal_theme,
    pad('  Notifications:') + (p.config.notifications_enabled ? 'enabled' : 'disabled'),
    pad('  Shell integration:') + (p.config.shell_integration_enabled ? 'enabled' : 'disabled'),
    pad('  WebGL renderer:') + (p.config.webgl_renderer_enabled ? 'enabled' : 'disabled'),
    pad('  Logging:') + (p.config.logging_enabled
      ? `enabled (${p.config.log_file_path})`
      : 'disabled'),
    pad('  Auto-check updates:') + (p.config.auto_check_updates ? 'enabled' : 'disabled'),
    pad('  Command notify:') + `${p.config.command_notify_threshold_seconds}s`,
  ]
  return lines.join('\n')
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/lib/__tests__/diagnostics.test.ts`
Expected: PASS — all seven tests.

Also run type-check to confirm the `api.ts` additions are valid:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean (no output).

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/api.ts desktop/frontend/src/lib/diagnostics.ts desktop/frontend/src/lib/__tests__/diagnostics.test.ts
git -c commit.gpgsign=false commit -m "frontend: DiagnosticsPayload type + formatDiagnostics renderer"
```

---

## Task 8: `SettingsDiagnostics.vue` component (test-first)

**Files:**
- Create: `desktop/frontend/src/components/SettingsDiagnostics.vue`
- Create: `desktop/frontend/src/components/__tests__/SettingsDiagnostics.test.ts`

- [ ] **Step 1: Write failing tests**

Create `desktop/frontend/src/components/__tests__/SettingsDiagnostics.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import SettingsDiagnostics from '../SettingsDiagnostics.vue'
import * as api from '../../lib/api'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

function fakePayload(): api.DiagnosticsPayload {
  return {
    generated_at: '2026-06-01T12:00:00Z',
    app_version: 'v0.4.0',
    os: 'darwin', arch: 'arm64', os_version: '14.6.1',
    webview_summary: 'WKWebView (Safari/17.5)',
    user_agent: 'mock-ua',
    relay_url: 'https://relay.example.com',
    relay_status: 'connected',
    relay_token_redacted: 'atk_AbCdEfGh…',
    allow_insecure_relay: false,
    remote_permission: 'full',
    uplink_paused: false,
    recent_relay_errors: [],
    config: {
      default_shell: '/bin/zsh', locale: 'system', terminal_theme: 'default',
      notifications_enabled: true, shell_integration_enabled: true,
      webgl_renderer_enabled: true, logging_enabled: true,
      log_file_path: '/tmp/atterm.log', auto_check_updates: true,
      command_notify_threshold_seconds: 10,
    },
  }
}

describe('SettingsDiagnostics', () => {
  beforeEach(() => {
    vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    vi.spyOn(api, 'exportDiagnostics').mockResolvedValue('/tmp/diag.txt')
  })

  it('renders the formatted text after mount', async () => {
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    expect(w.find('[data-testid="diag-preview"]').text()).toContain('App version:')
    expect(w.find('[data-testid="diag-preview"]').text()).toContain('v0.4.0')
  })

  it('passes navigator.userAgent to getDiagnostics on mount', async () => {
    const spy = vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    mount(SettingsDiagnostics)
    await flushPromises()
    expect(spy).toHaveBeenCalledWith(expect.any(String))
  })

  it('Copy button writes the formatted text to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText }, userAgent: 'test-ua' })
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    await w.find('[data-testid="diag-copy"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledOnce()
    expect(writeText.mock.calls[0][0]).toContain('App version:')
  })

  it('Export button calls exportDiagnostics with the formatted text', async () => {
    const exportSpy = vi.spyOn(api, 'exportDiagnostics').mockResolvedValue('/tmp/diag.txt')
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    await w.find('[data-testid="diag-export"]').trigger('click')
    await flushPromises()
    expect(exportSpy).toHaveBeenCalledOnce()
    expect(exportSpy.mock.calls[0][0]).toContain('App version:')
  })

  it('Refresh button re-fetches the payload', async () => {
    const getSpy = vi.spyOn(api, 'getDiagnostics').mockResolvedValue(fakePayload())
    const w = mount(SettingsDiagnostics)
    await flushPromises()
    expect(getSpy).toHaveBeenCalledTimes(1)
    await w.find('[data-testid="diag-refresh"]').trigger('click')
    await flushPromises()
    expect(getSpy).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/SettingsDiagnostics.test.ts`
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/SettingsDiagnostics.vue`:

```vue
<script lang="ts" setup>
import { ref, onMounted } from 'vue'
import { getDiagnostics, exportDiagnostics, type DiagnosticsPayload } from '../lib/api'
import { formatDiagnostics } from '../lib/diagnostics'
import { useI18n } from '../i18n/useI18n'

const { t } = useI18n()

const payload = ref<DiagnosticsPayload | null>(null)
const text = ref('')
const status = ref('')

async function refresh(): Promise<void> {
  status.value = ''
  payload.value = await getDiagnostics(navigator.userAgent)
  text.value = formatDiagnostics(payload.value)
}

async function copy(): Promise<void> {
  if (!text.value) return
  try {
    await navigator.clipboard.writeText(text.value)
    status.value = t('settings.diagnostics.copied')
    setTimeout(() => { status.value = '' }, 1500)
  } catch {
    status.value = t('settings.diagnostics.copyFailed')
  }
}

async function exportToFile(): Promise<void> {
  if (!text.value) return
  try {
    const path = await exportDiagnostics(text.value)
    if (path) {
      status.value = t('settings.diagnostics.exported', { path })
      setTimeout(() => { status.value = '' }, 3000)
    }
  } catch (e) {
    status.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(refresh)
</script>

<template>
  <div class="diag">
    <p class="hint">{{ t('settings.diagnostics.hint') }}</p>
    <pre class="preview" data-testid="diag-preview">{{ text }}</pre>
    <div class="row">
      <button type="button" data-testid="diag-copy" @click="copy">
        {{ t('settings.diagnostics.copy') }}
      </button>
      <button type="button" data-testid="diag-export" @click="exportToFile">
        {{ t('settings.diagnostics.export') }}
      </button>
      <button type="button" data-testid="diag-refresh" @click="refresh">
        {{ t('settings.diagnostics.refresh') }}
      </button>
      <span v-if="status" class="status">{{ status }}</span>
    </div>
  </div>
</template>

<style scoped>
.diag { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.preview { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 10px; font-family: ui-monospace, Menlo, monospace; font-size: 11px; line-height: 1.4; max-height: 320px; overflow: auto; white-space: pre; color: var(--fg); margin: 0; }
.row { display: flex; gap: 8px; align-items: center; }
button { height: 30px; border: 1px solid var(--accent); border-radius: 7px; background: var(--accent); color: var(--bg); padding: 0 12px; font-size: 12px; font-weight: 700; cursor: pointer; }
.status { font-size: 12px; color: var(--fg-dim); }
</style>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/__tests__/SettingsDiagnostics.test.ts`
Expected: PASS — all five tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsDiagnostics.vue desktop/frontend/src/components/__tests__/SettingsDiagnostics.test.ts
git -c commit.gpgsign=false commit -m "frontend: SettingsDiagnostics tab with preview + copy/export buttons"
```

---

## Task 9: Wire the tab into Settings + i18n keys

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add i18n keys to both locales**

In `desktop/frontend/src/i18n/messages/en.ts`, find the `settings` block and add a sibling under it (at the same level as `general`, `relay`, `logging`, `updates`, `plugins`, `shortcuts`):

```ts
    diagnostics: {
      tab: 'Diagnostics',
      hint: 'Snapshot of the desktop runtime, with API tokens and headers redacted. Useful for bug reports.',
      copy: 'Copy',
      export: 'Export…',
      refresh: 'Refresh',
      copied: 'Copied to clipboard.',
      copyFailed: 'Copy failed.',
      exported: 'Saved to {path}',
    },
```

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, mirror the same shape:

```ts
    diagnostics: {
      tab: '诊断',
      hint: '桌面端运行时快照，API token 和请求头已脱敏。适合反馈问题时附上。',
      copy: '复制',
      export: '导出…',
      refresh: '刷新',
      copied: '已复制到剪贴板',
      copyFailed: '复制失败',
      exported: '已保存到 {path}',
    },
```

- [ ] **Step 2: Wire the tab into `SettingsDialog.vue`**

Open `desktop/frontend/src/components/SettingsDialog.vue`. Make four edits:

(a) Add the import line (next to the other `SettingsX` imports near line 11):

```ts
import SettingsDiagnostics from "./SettingsDiagnostics.vue";
```

(b) Extend the `activeTab` type union and the `initialTab` prop union (around lines 28 and 38) to include `"diagnostics"`:

```ts
  initialTab?: "general" | "relay" | "logging" | "updates" | "shortcuts" | "diagnostics";
```

```ts
const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics">(props.initialTab ?? "general");
```

(c) Update the `switchTab` function signature (around line 70) to include the new tab:

```ts
function switchTab(next: "general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics") {
```

(d) In the `<template>` block, add a new tab button (next to the existing tab buttons; locate by searching for `settings.general.tab` and add an entry after the last one) and a corresponding panel.

The tab list entry — add to whatever container holds the existing `<button>`s with class like `tab` or `tabs`:

```vue
<button
  class="tab"
  :class="{ active: activeTab === 'diagnostics' }"
  type="button"
  @click="switchTab('diagnostics')"
>{{ t('settings.diagnostics.tab') }}</button>
```

The panel — add inside the conditional rendering block at the end of the other `<Settings...>` slots:

```vue
<SettingsDiagnostics v-if="activeTab === 'diagnostics'" />
```

(The exact placement depends on the file's current template structure. Read lines 80-180 of `SettingsDialog.vue` to find the existing tab-button and panel patterns, and follow them. Don't restructure — slot the new ones in.)

- [ ] **Step 3: Run the existing tests + type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run`
Expected: PASS — every existing test plus the new diagnostics tests.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

Run the i18n parity test:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "frontend: Diagnostics tab wired into Settings dialog + i18n"
```

---

## Task 10: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Full backend suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 2: Go vet**

Run: `cd /Users/attson/code/github.com.attson/atterm && go vet ./...`
Expected: clean.

- [ ] **Step 3: Frontend suite**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS — every existing test plus the new diagnostics tests.

- [ ] **Step 4: Frontend type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 5: Wails + Capacitor builds (regression)**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: succeeds.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: succeeds.

- [ ] **Step 6: Manual / human smoke (documented, not gating)**

For local verification after merging:

1. `wails dev` (or `make build && open ./build/bin/atterm.app` on macOS).
2. Open Settings → Diagnostics. The preview should populate within a second showing app version, OS info, WebView summary, and either `(not configured)` or your relay info.
3. Click "Copy". The button area should briefly say "Copied to clipboard."; paste into another app to confirm the format matches §7 of the spec.
4. Click "Export…". The native save dialog should open with a filename like `atterm-diagnostics-2026-06-01T15-30-00Z.txt`. Save it; open the resulting `.txt`; confirm it matches the preview exactly.
5. Force a relay error (e.g. set an invalid `atk_xxx` token and reconnect). The error appears in the "Recent relay errors" section after clicking "Refresh".

No commit needed.

---

## Self-review notes

- **Spec coverage:**
  - §3 DiagnosticsPayload / RelayErrorEntry / ConfigSummary → Tasks 2 + 4
  - §4 redactToken / redactURL / redactErrorLine → Task 1
  - §5 ring buffer on App → Task 2
  - §6 systemVersion per-OS → Task 3
  - §7.1 GetDiagnostics binding → Task 6
  - §7.2 ExportDiagnostics binding + writeFile injection → Task 6
  - §7 parseWebViewSummary → Task 1
  - §7 recordError wiring in uplink → Task 5
  - §8.1 TS shim → Task 7
  - §8.2 formatDiagnostics → Task 7
  - §8.3 SettingsDiagnostics.vue → Task 8
  - §8.4 SettingsDialog integration → Task 9
  - §8.5 i18n keys → Task 9
  - §9 testing — Tasks 1, 2, 4, 5, 6, 7, 8 each include their respective test bodies
  - §10 errors — `ExportDiagnostics` returns `("", nil)` on cancel and `("", err)` on real failure; non-fatal helper failures (sw_vers missing) leave OSVersion empty.

- **Placeholder scan:** no TBDs. Each Vue/Go/TS code block is complete and self-contained. The "find by grep" instructions in Task 9 step 2d are runtime navigation hints, not implementation gaps — the engineer is told what to look for and what to add.

- **Type consistency:** `DiagnosticsPayload` field names match exactly between Go (Task 4) and TS (Task 7). The contract test in Task 4 step 1 pins the JSON key list; if any later task renames a field, that test fails loudly. `recordError func(error)` is the same signature in `uplink` (Task 5) and `App.recordRelayError` (Task 2). `writeFile` injection point uses `writeFileFunc` consistently in the struct field, the function variable, and the test stub signature.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-01-desktop-diagnostics-export.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.
