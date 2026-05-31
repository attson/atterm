# Desktop diagnostics export (design)

Date: 2026-06-01
Status: Draft (design phase); pending implementation plan
Roadmap item: P1.10

## 1. Goal

Give users a one-click way to dump their atterm desktop's runtime
state into a copyable, paste-into-an-issue plaintext file. Bundle
the bits a maintainer normally has to extract piecemeal: version,
OS, WebView runtime, relay status, recent relay errors, and a
summary of the user's settings — with API tokens and other
credentials redacted automatically.

After this lands:

- Settings gains a new "Diagnostics" tab with a live preview of the
  current diagnostics text and two actions: "Copy" and "Export".
- "Copy" writes the text to the system clipboard.
- "Export" opens a native Save File dialog defaulting to
  `atterm-diagnostics-YYYY-MM-DDTHH-MM-SSZ.txt` and writes the same
  text to disk.
- The text never includes the full API token, never includes URL
  query strings (which sometimes carry tokens), and never includes
  terminal PTY output.

Out of scope:

- A "view recent log" panel in Diagnostics — already exists under
  Logging tab.
- Uploading diagnostics to a backend collector.
- Including the relay's own `/admin/api/health` payload (P1.7).
  Desktop diagnostics is desktop-only.
- Anonymising hostnames in the relay URL (the host is operationally
  necessary information).
- A periodic "ship anonymous stats" telemetry mechanism.

## 2. Architecture

```
┌── desktop (Wails / Vue) ─────────────────────────────────────────┐
│                                                                   │
│  desktop/app.go (modified)                                        │
│    • recentRelayErrors      ringbuf (size 5)                      │
│    • recordRelayError(err)  appends with timestamp                │
│    • GetDiagnostics(userAgent string) DiagnosticsPayload          │
│    • ExportDiagnostics(content string) (path string, error)       │
│                                                                   │
│  desktop/diagnostics.go (new)                                     │
│    • DiagnosticsPayload struct                                    │
│    • collectDiagnostics(a, userAgent) DiagnosticsPayload          │
│    • redactToken(s) string                                        │
│    • redactURL(u) string                                          │
│    • redactErrorLine(s) string                                    │
│    • formatDiagnostics(p) string                                  │
│                                                                   │
│  desktop/system_version_darwin.go (new)                           │
│  desktop/system_version_linux.go (new)                            │
│  desktop/system_version_windows.go (new)                          │
│  desktop/system_version_other.go (new)                            │
│    • systemVersion() string  (build-tagged per OS)                │
│                                                                   │
│  desktop/uplink.go (modified)                                     │
│    • on retry path: a.recordRelayError(err) alongside log.Printf  │
│    • on auth-error: a.recordRelayError(reason)                    │
│                                                                   │
│  desktop/frontend/src/lib/api.ts (modified)                       │
│    • DiagnosticsPayload type                                      │
│    • getDiagnostics(userAgent) wrapper                            │
│    • exportDiagnostics(content) wrapper                           │
│                                                                   │
│  desktop/frontend/src/components/SettingsDiagnostics.vue (new)    │
│    • preview block                                                │
│    • [Copy] / [Export] buttons                                    │
│                                                                   │
│  desktop/frontend/src/components/SettingsDialog.vue (modified)    │
│    • register "diagnostics" tab + label                           │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

Three small modules with one responsibility each: data collection
(`diagnostics.go`), platform-specific OS version helper
(`system_version_*.go`), and ring-buffer error history (in
`app.go` alongside the existing fields it touches).

## 3. Data model

```go
type DiagnosticsPayload struct {
    GeneratedAt          string             `json:"generated_at"`            // RFC3339 UTC
    AppVersion           string             `json:"app_version"`             // main.Version
    OS                   string             `json:"os"`                      // "darwin", "windows", "linux"
    Arch                 string             `json:"arch"`                    // runtime.GOARCH
    OSVersion            string             `json:"os_version"`              // "14.6.1" etc, or "" on failure
    WebViewSummary       string             `json:"webview_summary"`         // "WKWebView (Safari/17.5)" etc
    UserAgent            string             `json:"user_agent"`              // raw UA passed in from JS
    RelayURL             string             `json:"relay_url"`               // host-only, no query
    RelayStatus          string             `json:"relay_status"`            // "connected" | "disconnected" | "paused" | "not_configured"
    RelayTokenRedacted   string             `json:"relay_token_redacted"`    // "atk_AbCdEfGh…" or ""
    AllowInsecureRelay   bool               `json:"allow_insecure_relay"`
    RemotePermission     string             `json:"remote_permission"`
    UplinkPaused         bool               `json:"uplink_paused"`
    RecentRelayErrors    []RelayErrorEntry  `json:"recent_relay_errors"`     // newest first, ≤ 5
    Config               ConfigSummary      `json:"config"`
}

type RelayErrorEntry struct {
    Timestamp string `json:"timestamp"` // RFC3339 UTC
    Message   string `json:"message"`   // redacted (see §4)
}

type ConfigSummary struct {
    DefaultShell                   string `json:"default_shell"`
    Locale                         string `json:"locale"`
    TerminalTheme                  string `json:"terminal_theme"`
    NotificationsEnabled           bool   `json:"notifications_enabled"`
    ShellIntegrationEnabled        bool   `json:"shell_integration_enabled"`
    WebGLRendererEnabled           bool   `json:"webgl_renderer_enabled"`
    LoggingEnabled                 bool   `json:"logging_enabled"`
    LogFilePath                    string `json:"log_file_path"`  // path only, no contents
    AutoCheckUpdates               bool   `json:"auto_check_updates"`
    CommandNotifyThresholdSeconds  int    `json:"command_notify_threshold_seconds"`
}
```

The exported text format is rendered from this payload by
`formatDiagnostics(p)`; the JSON shape is exposed so future tooling
(or this same file converted to JSON later) can consume the same
fields.

## 4. Redaction

Three helpers, each with focused tests in §8.

### 4.1 `redactToken(s string) string`

```go
// redactToken returns the first 12 characters of an API token followed by
// "…". For atk_ tokens this yields "atk_AbCdEfGh…" — enough to recognise
// the token in a log line, not enough to authenticate.
// Empty string returns empty string.
func redactToken(s string) string {
    if s == "" {
        return ""
    }
    if len(s) <= 12 {
        return s + "…"
    }
    return s[:12] + "…"
}
```

### 4.2 `redactURL(u string) string`

```go
// redactURL returns scheme://host[:port] only — drops path, query, and
// fragment. Defends against URLs that carry tokens in ?t=… or fragments.
// Unparseable input returns the literal string "(invalid url)".
func redactURL(u string) string {
    if u == "" {
        return ""
    }
    parsed, err := url.Parse(u)
    if err != nil || parsed.Host == "" {
        return "(invalid url)"
    }
    return parsed.Scheme + "://" + parsed.Host
}
```

### 4.3 `redactErrorLine(s string) string`

Applies token and Authorization-header masking to a single line of
free-form text:

```go
var (
    tokenRE = regexp.MustCompile(`atk_[A-Za-z0-9_-]{8,}`)
    authRE  = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)\S+`)
    cookieRE = regexp.MustCompile(`(?i)(cookie\s*:\s*)\S+`)
)

func redactErrorLine(s string) string {
    s = tokenRE.ReplaceAllStringFunc(s, redactToken)
    s = authRE.ReplaceAllString(s, "${1}[redacted]")
    s = cookieRE.ReplaceAllString(s, "${1}[redacted]")
    return s
}
```

The token regex matches the `atk_` prefix + at least 8 base64url
characters; `redactToken` then strips it to the prefix. The
Authorization / Cookie patterns are conservative (case-insensitive,
space-tolerant) and replace the secret portion with `[redacted]`.

## 5. Ring buffer for recent relay errors

A small fixed-size list on `App`:

```go
type App struct {
    // ...existing fields...
    relayErrMu   sync.Mutex
    relayErrors  []RelayErrorEntry // newest-first, ≤ 5
}

const maxRelayErrors = 5

func (a *App) recordRelayError(err error) {
    if err == nil {
        return
    }
    a.relayErrMu.Lock()
    defer a.relayErrMu.Unlock()
    entry := RelayErrorEntry{
        Timestamp: time.Now().UTC().Format(time.RFC3339),
        Message:   redactErrorLine(err.Error()),
    }
    a.relayErrors = append([]RelayErrorEntry{entry}, a.relayErrors...)
    if len(a.relayErrors) > maxRelayErrors {
        a.relayErrors = a.relayErrors[:maxRelayErrors]
    }
}

func (a *App) snapshotRelayErrors() []RelayErrorEntry {
    a.relayErrMu.Lock()
    defer a.relayErrMu.Unlock()
    out := make([]RelayErrorEntry, len(a.relayErrors))
    copy(out, a.relayErrors)
    return out
}
```

`desktop/uplink.go` already has two error sites that should feed
the buffer:

- `uplink.go:76` (reconnect retry) — `a.recordRelayError(err)`
  alongside the existing `log.Printf`.
- `uplink.go:416-435` (close-error reason mapping) —
  `a.recordRelayError(fmt.Errorf("%s", reason))` after building the
  reason string.

The buffer survives process lifetime; it's cleared on restart. No
persistence, no admin export concerns.

## 6. OS-version helper

Four files, one per build constraint, all exporting `systemVersion() string`:

### `desktop/system_version_darwin.go`

```go
//go:build darwin

package main

import (
    "os/exec"
    "strings"
)

func systemVersion() string {
    out, err := exec.Command("sw_vers", "-productVersion").Output()
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(out))
}
```

### `desktop/system_version_linux.go`

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
// Falls back to empty string on any error.
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

### `desktop/system_version_windows.go`

```go
//go:build windows

package main

import (
    "os/exec"
    "strings"
)

// systemVersion runs `cmd /c ver` which prints "Microsoft Windows [Version X.Y.Z]".
func systemVersion() string {
    out, err := exec.Command("cmd", "/c", "ver").Output()
    if err != nil {
        return ""
    }
    return strings.TrimSpace(string(out))
}
```

### `desktop/system_version_other.go`

```go
//go:build !darwin && !linux && !windows

package main

func systemVersion() string {
    return ""
}
```

The helper is best-effort: empty string is treated as "unknown" by
the diagnostics renderer.

## 7. Wails bindings

### 7.1 `App.GetDiagnostics`

```go
// GetDiagnostics builds the current desktop diagnostics payload. userAgent
// should be the renderer's navigator.userAgent so the WebView summary
// can include a parseable version string. The returned struct is JSON-
// marshalable and is consumed by the SettingsDiagnostics view.
func (a *App) GetDiagnostics(userAgent string) DiagnosticsPayload {
    return collectDiagnostics(a, userAgent)
}
```

`collectDiagnostics` lives in `desktop/diagnostics.go`:

```go
func collectDiagnostics(a *App, userAgent string) DiagnosticsPayload {
    cfg := a.cfgStore.Get()
    a.mu.Lock()
    paused := cfg.RelayPaused
    connected := a.uplink != nil
    a.mu.Unlock()

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
        RecentRelayErrors:  a.snapshotRelayErrors(),
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

`parseWebViewSummary(ua string)` uses regex to extract WebView
identifier + version:

```go
var safariRE = regexp.MustCompile(`Version/(\S+)\s+Safari`)
var edgeRE = regexp.MustCompile(`Edg/(\S+)`)
var webkitGTKRE = regexp.MustCompile(`WebKit/(\S+)`)

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
    return ua // unknown; surface the raw UA so the user can paste it
}
```

### 7.2 `App.ExportDiagnostics`

```go
// ExportDiagnostics opens a native save dialog (default filename
// "atterm-diagnostics-<ts>.txt") and writes content to the chosen path.
// Returns "" path if the user cancelled. Returns an error only on actual
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
    if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
        return "", err
    }
    return path, nil
}
```

The renderer formats the payload into text (via the same logic
`formatDiagnostics` provides; the Vue file imports nothing about
formatting — it gets the formatted string from a TS helper, see
§8.3) and passes it to `ExportDiagnostics`.

## 8. Frontend

### 8.1 TS shim (`lib/api.ts`)

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

export function getDiagnostics(userAgent: string): Promise<DiagnosticsPayload> {
  return bindings().GetDiagnostics(userAgent)
}

export function exportDiagnostics(content: string): Promise<string> {
  return bindings().ExportDiagnostics(content)
}
```

### 8.2 `formatDiagnostics` (in `desktop/frontend/src/lib/diagnostics.ts`, new)

A pure TS function that takes the payload and returns the
plaintext block. Placing this in TS rather than Go means the
preview updates instantly when the user toggles tabs (no Wails
round-trip just to re-format), and the test suite can pin the
exact output via vitest snapshots if desired.

```ts
export function formatDiagnostics(p: DiagnosticsPayload): string {
  const pad = (k: string) => k.padEnd(24, ' ')
  const lines = [
    `atterm desktop diagnostics — ${p.generated_at}`,
    '--------------------------------------------------',
    pad('App version:') + p.app_version,
    pad('OS:') + `${p.os} ${p.os_version} (${p.arch})`.trim(),
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
    pad('  Command notify threshold:') + `${p.config.command_notify_threshold_seconds}s`,
  ]
  return lines.join('\n')
}
```

### 8.3 `SettingsDiagnostics.vue` (new)

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

async function refresh() {
  status.value = ''
  payload.value = await getDiagnostics(navigator.userAgent)
  text.value = formatDiagnostics(payload.value)
}

async function copy() {
  if (!text.value) return
  try {
    await navigator.clipboard.writeText(text.value)
    status.value = t('settings.diagnostics.copied')
    setTimeout(() => { status.value = '' }, 1500)
  } catch {
    status.value = t('settings.diagnostics.copyFailed')
  }
}

async function exportToFile() {
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
      <button type="button" data-testid="diag-copy" @click="copy">{{ t('settings.diagnostics.copy') }}</button>
      <button type="button" data-testid="diag-export" @click="exportToFile">{{ t('settings.diagnostics.export') }}</button>
      <button type="button" data-testid="diag-refresh" @click="refresh">{{ t('settings.diagnostics.refresh') }}</button>
      <span v-if="status" class="status">{{ status }}</span>
    </div>
  </div>
</template>
```

Styling follows the existing settings-tab look (subdued background,
monospace pre, button row).

### 8.4 `SettingsDialog.vue` integration

Add `"diagnostics"` to the activeTab type union and register the
tab beside the existing six. No conditional-hide logic — the
Diagnostics tab is available in every desktop build (Wails-only;
Capacitor/web users don't have a Settings dialog).

### 8.5 i18n keys

Six new keys under `settings.diagnostics`:

- `tab` ("Diagnostics" / "诊断")
- `hint` (1-line description)
- `copy` ("Copy")
- `export` ("Export…")
- `refresh` ("Refresh")
- `copied` ("Copied")
- `copyFailed` ("Copy failed")
- `exported` ("Saved to {path}")

## 9. Testing

### 9.1 `desktop/diagnostics_test.go`

```go
func TestRedactToken_Cases(t *testing.T) { /* empty / short / long */ }
func TestRedactURL_Cases(t *testing.T)   { /* https with query / http-only / empty / malformed */ }
func TestRedactErrorLine_StripsTokens(t *testing.T) {
    // input "dial failed: atk_abcdefghij sent" → "dial failed: atk_abcdefgh… sent"
}
func TestRedactErrorLine_StripsAuthHeader(t *testing.T) {
    // input "401 Authorization: Bearer atk_xyz" → "401 Authorization: Bearer [redacted]"
}
func TestRedactErrorLine_StripsCookieHeader(t *testing.T) {
    // input "Cookie: atterm_session=abc123def" → "Cookie: [redacted]"
}
```

### 9.2 `desktop/diagnostics_payload_test.go`

```go
func TestCollectDiagnostics_FieldsPopulated(t *testing.T) {
    // Build App with a configured relay URL+token; call collectDiagnostics
    // with a fake Safari UA; assert all key fields are non-empty and
    // RelayURL has no query string + RelayTokenRedacted ends with "…".
}

func TestCollectDiagnostics_NotConfigured(t *testing.T) {
    // Empty relay URL → RelayStatus = "not_configured", token redacted = "".
}

func TestParseWebViewSummary_Cases(t *testing.T) {
    // Safari UA → "WKWebView (Safari/X)"
    // Edge UA   → "WebView2 (Edg/X)"
    // WebKitGTK UA → "WebKitGTK (WebKit/X)"
    // empty → ""
    // unknown → echoes the input
}
```

### 9.3 `desktop/app_relay_errors_test.go`

```go
func TestRecordRelayError_RingBufferKeeps5Newest(t *testing.T) {
    a := newRelayTestApp(t)
    for i := 0; i < 8; i++ {
        a.recordRelayError(fmt.Errorf("err-%d", i))
    }
    got := a.snapshotRelayErrors()
    if len(got) != 5 { t.Fatalf("want 5, got %d", len(got)) }
    if got[0].Message != "err-7" || got[4].Message != "err-3" {
        t.Fatalf("ordering wrong: %v", got)
    }
}

func TestRecordRelayError_NilIsNoop(t *testing.T) {
    a := newRelayTestApp(t)
    a.recordRelayError(nil)
    if len(a.snapshotRelayErrors()) != 0 { t.Fatal("nil should not record") }
}

func TestRecordRelayError_RedactsTokensInMessage(t *testing.T) {
    a := newRelayTestApp(t)
    a.recordRelayError(fmt.Errorf("401 with token atk_abcdefghij failed"))
    got := a.snapshotRelayErrors()[0].Message
    if !strings.Contains(got, "atk_abcdefgh…") {
        t.Fatalf("expected redacted token, got %q", got)
    }
}
```

### 9.4 `desktop/diagnostics_export_test.go`

```go
func TestExportDiagnostics_WritesContentWhenPathReturned(t *testing.T) {
    // We can't drive the native save dialog from tests. Instead, this
    // test asserts the *writer* side: extract the io.WriteFile call into
    // a swappable function variable (var writeFile = os.WriteFile) so
    // tests can substitute a stub; then verify content+perm.
}
```

(This requires a small refactor in `ExportDiagnostics` to take a
`writeFile func(string, []byte, fs.FileMode) error` field on App,
defaulting to `os.WriteFile` in production. The test sets it to a
capturing stub.)

### 9.5 Frontend tests

`desktop/frontend/src/lib/__tests__/diagnostics.test.ts`:

- `formatDiagnostics` with a fully-populated payload → fixed string
  (use a snapshot or explicit `.toBe(...)` against the expected
  multi-line block).
- `formatDiagnostics` with empty `recent_relay_errors` → contains
  `"  (none)"`.

`desktop/frontend/src/components/__tests__/SettingsDiagnostics.test.ts`:

- Renders the preview after onMount; Copy button calls
  `navigator.clipboard.writeText` with the formatted text; Export
  button calls the binding.

### 9.6 Contract test

`TestDiagnosticsPayload_JSONFieldsStable` — same idea as the relay
health contract test: marshal a zero value, compare top-level keys
against a hardcoded list.

## 10. Errors and observability

- Save-dialog cancelled → returns `("", nil)` (no error). UI shows
  no message.
- Save-dialog write fails → returns `("", err)`. UI shows the
  error message.
- `getDiagnostics` itself never errors (all inputs are local
  reads); if any platform helper fails (e.g., `sw_vers` missing)
  the field stays empty and the rest of the payload is still
  emitted. No partial-failure surfacing in the UI.

## 11. Rollout

- New tab is unconditionally present in the Wails build. No flag.
- The ring buffer fills as soon as uplink errors occur after
  upgrade; on first launch after install it starts empty.
- No README change required; the feature is self-explanatory.
