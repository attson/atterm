# Relay health check page (design)

Date: 2026-05-31
Status: Draft (design phase); pending implementation plan
Roadmap item: P1.7

## 1. Goal

Give relay operators a single URL they can open to see whether the
relay is running and configured correctly, and copy a redaction-safe
diagnostics blob to paste into an issue or chat.

After this lands:

- `GET /healthz` returns minimal JSON (`{ok, version}`) without auth.
  Load balancers, K8s liveness probes, and ops scripts hit this.
- `GET /admin/health` renders an HTML page (admin login required)
  listing version, uptime, HTTPS status, configured origins, bootstrap
  admin status, rate-limit settings, active uplink count, and a mobile
  origin compatibility flag. The page has a "Copy diagnostics" button.
- `GET /admin/api/health` returns the same data as JSON (admin login
  required) so the copy button has a stable contract and external
  scripts can poll it.

Out of scope:

- Prometheus / OpenMetrics export (separate roadmap item if needed).
- Liveness vs readiness distinction (one `/healthz` suffices for this
  feature set).
- Per-user / per-session diagnostics (those belong in audit logging,
  P3 territory).
- Auto-refresh on the HTML page (the user can reload; no SSE/WS).
- Localisation (admin-facing, English only — matches the existing
  `/admin/*` pages).

## 2. Architecture

```
┌── relay (Go) ────────────────────────────────────────────────┐
│                                                               │
│  internal/relay/                                              │
│    health_http.go (new)                                       │
│      • handleHealthzHTTP    GET /healthz (public)             │
│      • handleAdminHealth    GET /admin/health (admin, HTML)   │
│      • handleAdminHealthAPI GET /admin/api/health (admin)     │
│      • collectHealth(s)     gathers HealthPayload             │
│      • isMobileOriginCompatible(origins) bool                 │
│                                                               │
│    server.go (modified)                                       │
│      • uplinkCount int64 (atomic)                             │
│      • UplinkCount() int64                                    │
│      • startTime time.Time                                    │
│      • routes register the three new endpoints                │
│                                                               │
│    uplink_conn.go (modified)                                  │
│      • handleUplinkHTTP increments/decrements uplinkCount     │
│                                                               │
│    templates/health.gohtml (new, embedded)                    │
│      • html/template; renders the diagnostics grid + Copy btn │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

The page is server-rendered Go `html/template`. No Vue, no
dependency on the web frontend build — when the operator is reaching
for a diagnostics page, the relay binary alone has to suffice.

## 3. Data model

```go
type HealthPayload struct {
    Version                  string   `json:"version"`
    UptimeSeconds            int64    `json:"uptime_seconds"`
    HTTPS                    bool     `json:"https"`
    ConfiguredOrigins        []string `json:"configured_origins"`
    OriginsOpen              bool     `json:"origins_open"`
    BootstrapAdminConfigured bool     `json:"bootstrap_admin_configured"`
    RateLimitPerMinute       int      `json:"rate_limit_per_minute"`
    MaxConnectionsPerKey     int      `json:"max_connections_per_key"`
    ActiveUplinks            int64    `json:"active_uplinks"`
    MobileOriginCompatible   bool     `json:"mobile_origin_compatible"`
    GeneratedAt              string   `json:"generated_at"` // RFC3339 UTC
}
```

Every field is either an aggregate count or an operator-controlled
configuration value. No user emails, no token prefixes, no IP
addresses, no file paths. The page and the copy-diagnostics text are
the same data — there is no separate "redacted" view, because the
data is already redaction-safe by construction.

### Field derivations

- `Version`: `cfg.Version` (build-time ldflag, already in `relay.Config`).
- `UptimeSeconds`: `time.Since(s.startTime)` where `s.startTime` is set
  in `NewServer`.
- `HTTPS`: `r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")`.
  Reuses the same logic as `publicBaseURL` in `pair_http.go:15`.
- `ConfiguredOrigins`: a copy of `cfg.AllowedOrigins`.
- `OriginsOpen`: `len(cfg.AllowedOrigins) == 0`.
- `BootstrapAdminConfigured`: `Store.ListUsers(ctx)` returns at least
  one row with `is_admin=true`. Only the admin-gated endpoints carry
  this field, so it's queried at most once per human page-load — no
  cache needed.
- `RateLimitPerMinute`, `MaxConnectionsPerKey`: read from `cfg` after
  defaults are applied (the values already passed through
  `newFixedWindowLimiter`'s zero-handling in `NewServer`).
- `ActiveUplinks`: `atomic.LoadInt64(&s.uplinkCount)`.
- `MobileOriginCompatible`: see §4.
- `GeneratedAt`: `time.Now().UTC().Format(time.RFC3339)`.

## 4. Endpoints

### 4.1 `GET /healthz`

Public, no auth.

- **200 OK**:
  ```json
  {"ok": true, "version": "v0.3.6"}
  ```
- Response headers: `Content-Type: application/json`, `Cache-Control: no-store`.
- Always 200 if the binary can serve HTTP at all; deeper "ready"
  signals are intentionally out of scope.

### 4.2 `GET /admin/api/health`

Admin-gated (`requireAdmin` middleware, same one that protects
`/admin/api/config`).

- **200 OK**: returns the full `HealthPayload` JSON above.
- **401**: not authenticated.
- **403**: authenticated but not admin.

### 4.3 `GET /admin/health`

Admin-gated. Renders `templates/health.gohtml` with the same
`HealthPayload` data, and injects it as `window.__HEALTH__` for the
copy-diagnostics button.

- **200 OK**: HTML response, `Content-Type: text/html; charset=utf-8`.
- **401 / 403**: as above.

## 5. `isMobileOriginCompatible`

```go
func isMobileOriginCompatible(origins []string) bool {
    if len(origins) == 0 {
        return true // wildcard origin pool — mobile reaches it too
    }
    for _, o := range origins {
        switch {
        case strings.HasPrefix(o, "capacitor://"):
            return true
        case strings.HasPrefix(o, "ionic://"):
            return true
        case strings.HasPrefix(o, "https://localhost"):
            return true
        case o == "null":
            return true
        }
    }
    return false
}
```

When this returns `false`, the HTML page shows a yellow callout under
the field. The diagnostics blob simply records `mobile_compat: no`,
giving the operator a one-glance signal to add a Capacitor origin.

The `origins_open == true` case is technically "compatible" but is
itself a security warning; the HTML page surfaces both:
`Mobile compat: yes (origins are open — narrow this in production)`.

## 6. Uplink counting

`internal/relay/server.go` gains:

```go
type Server struct {
    // ...existing fields...
    startTime    time.Time
    uplinkCount  int64 // atomic; incremented in handleUplinkHTTP, decremented on defer
}

func (s *Server) UplinkCount() int64 {
    return atomic.LoadInt64(&s.uplinkCount)
}
```

`NewServer` sets `startTime = time.Now()`.

`internal/relay/uplink_conn.go`, inside `handleUplinkHTTP`, right after
the WebSocket upgrade succeeds:

```go
atomic.AddInt64(&s.uplinkCount, 1)
defer atomic.AddInt64(&s.uplinkCount, -1)
```

One increment per successful upgrade; one decrement on connection
exit. A connection that fails the auth check before upgrade is never
counted (correct — it isn't an "active uplink").

## 7. HTML page

`internal/relay/templates/health.gohtml` is loaded with `//go:embed`
into a `*template.Template`. The page is one self-contained file:
inline CSS, inline JavaScript, no external resources. Total size
target ≤ 6 KB. Layout:

```
┌──────────────────────────────────────────┐
│  atterm-relay — health                   │
│                                          │
│  ● Version             v0.3.6            │
│  ● Uptime              1h 23m            │
│  ● HTTPS               yes               │
│  ● Configured origins  wails.localhost,  │
│                         capacitor://…    │
│  ● Origins open        no                │
│  ● Bootstrap admin     configured        │
│  ● Rate limit / min    600               │
│  ● Max conn / key      64                │
│  ● Active uplinks      3                 │
│  ● Mobile compat       yes               │
│                                          │
│  [ Copy diagnostics ]   generated 14:23  │
└──────────────────────────────────────────┘
```

Each row is `<dl class="row"><dt>Label</dt><dd>Value</dd></dl>`. Rows
with warning states (`origins_open=true`, `mobile_compat=false`)
get a `data-state="warn"` attribute and a yellow left border via CSS.

The copy button reads `window.__HEALTH__` (JSON injected into a
`<script>` block via Go's `template.JS` escape), formats it into a
fixed-width text block, and calls `navigator.clipboard.writeText`. On
success the button label briefly changes to "Copied". On failure
(`clipboard` permission denied), it falls back to selecting a hidden
`<textarea>` so the operator can copy manually.

Text-block format (one space between colon and value, columns aligned
to 23 chars, no trailing spaces):

```
atterm-relay diagnostics — 2026-05-31T14:23:00Z
-----------------------------------------------
Version:                v0.3.6
Uptime:                 1h 23m
HTTPS:                  yes
Origins:                wails.localhost, capacitor://localhost
Origins open:           no
Bootstrap admin:        configured
Rate limit / min:       600
Max conn / key:         64
Active uplinks:         3
Mobile compat:          yes
```

## 8. Errors and observability

- All three endpoints are stateless reads. No mutating operations, no
  rate limits (admin auth already gates `/admin/*`).
- On `Store.ListUsers` failure (transient SQLite error),
  `BootstrapAdminConfigured` falls back to `false` and the page shows
  a small "(check failed)" suffix; the JSON includes a sibling
  `health_check_warnings: ["bootstrap_admin_lookup_failed"]` field.

## 9. Testing

### 9.1 `internal/relay/health_http_test.go`

- `TestHealthz_Public_ReturnsOKAndVersion` — public GET → 200 with
  `ok:true` and `version` field, no `Cache-Control: max-age`.
- `TestAdminHealth_NoAuth_401` — unauthenticated GET → 401.
- `TestAdminHealth_NonAdmin_403` — Bearer-authed regular user → 403.
- `TestAdminHealth_Admin_200_AllFieldsPresent` — admin Bearer → 200,
  HTML body contains every field label from §7.
- `TestAdminHealthAPI_Admin_JSONShape` — admin Bearer → 200, JSON
  body matches `HealthPayload` fields verbatim (string-compare
  expected field names against actual top-level keys).
- `TestUplinkCount_IncrementsOnConnectDecrementsOnClose` — drives a
  fake uplink WebSocket through `handleUplinkHTTP`, asserts
  `UplinkCount()` reads 1 mid-stream, 0 after close.
- `TestIsMobileOriginCompatible_Cases` — table-driven; covers
  empty/`capacitor://`/`ionic://`/`https://localhost`/`null`/no-match.

### 9.2 Contract test

`TestHealthPayload_JSONFieldsStable` — asserts the JSON tag names by
marshaling a zero value and comparing the produced key list to a
hardcoded slice. Catches accidental renames that would break the
copy-diagnostics consumers.

## 10. Rollout

- New code only; no migrations, no config flags. The endpoints exist
  as soon as the binary ships.
- Operators who deploy behind a load balancer can point its liveness
  probe at `/healthz` to start getting uplink health monitoring.
- No documentation in `README.md` yet (separate doc PR if anyone
  asks); the page is self-explanatory.
