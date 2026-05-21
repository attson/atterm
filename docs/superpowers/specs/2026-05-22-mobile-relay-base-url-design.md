# Mobile Relay Base URL + Insecure HTTP/WS Toggle — Design

Date: 2026-05-22
Status: Draft (pending implementation plan)

## Background

The Capacitor-wrapped iOS app (`mobile/`) bundles `web/dist/` and loads it from `capacitor://localhost`. The Vue web client (`web/src/`) currently sends all HTTP and WS traffic to relative paths resolved against `location.host`, which in Capacitor becomes `capacitor://localhost` — no relay is reachable. The result today is a blank-once-logged-in experience: the login form renders, but `POST /api/auth/login` fails immediately.

`mobile/README.md` still references a legacy "relay URL field in the token panel" that was part of the pre-Vue vanilla web client. The Vue rewrite (PR-A through PR-F) did not port that field, so the bundled iOS app has no way to point at a relay.

This design adds first-class relay configuration to the web client, scoped to the Capacitor environment, with a paste-token onboarding flow and an explicit insecure-HTTP/WS toggle that mirrors the existing desktop pattern (`desktop/relay_security.go`, AGENTS.md red-line 9).

## Goals

- Capacitor-built iOS app can connect to an arbitrary relay over `https://`/`wss://` (default) or `http://`/`ws://` (with explicit insecure opt-in for non-loopback hosts).
- Authentication uses an API token (`atk_…`) — same token shape desktop already uses.
- Zero impact on the browser web client (`location`-based same-origin cookie flow stays).
- Zero relay protocol changes (AGENTS.md red-line 4 untouched).
- Zero relay code changes; only operational guidance to include `capacitor://localhost` in `ATTERM_ORIGINS`.

## Non-Goals

- Password-grant token endpoint on relay. Tokens are minted on the web/desktop via `POST /api/me/tokens` (cookie+CSRF) and pasted into the mobile setup screen. This is the same onboarding burden as desktop.
- iOS push notifications (APNs). Web Push doesn't apply in WKWebView; mobile push is a separate effort.
- App Store-grade ATS configuration. The current `NSAllowsArbitraryLoads = true` stays for the MVP; tightening ATS is a follow-up before public release.
- Android app. Out of scope; same architecture would apply if/when added.
- Secure storage of the token (Keychain). MVP stores in `localStorage`; Keychain via Capacitor plugin is a follow-up.

## Constraints

- AGENTS.md red-line 4 (wire compatibility): no new frames, no payload changes.
- AGENTS.md red-line 9 (relay security defaults): the mobile insecure toggle is the user-visible analogue of the desktop one. Plain `ws://`/`http://` to non-loopback hosts must require explicit opt-in.
- AGENTS.md red-line 10 (no CDN in web): unchanged; setup page consumes only bundled assets.
- Capacitor 8.3.3, Vue 3 + Vite 5, no new top-level dependencies.

## Architecture

### Runtime layout (Capacitor only)

```
localStorage  atterm.relay  =  { base, token, allowInsecure }
                              │
                              ▼
                shared/api/relay-config.ts
                (load / save / clear / validate / isMobileApp)
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
      shared/api/client.ts            shared/ws/client-conn.ts
      apiFetch — mobile branch:       wsUrl — mobile branch:
        url = base + path               proto = http→ws / https→wss
        Authorization: Bearer           host  = new URL(base).host
        credentials: 'omit'             new WebSocket(url, [token])
        no CSRF
                              │
                              ▼
                          atterm-relay  (unchanged)
```

The browser web client (`isMobileApp() === false`) takes the existing same-origin cookie path. Both branches live in the same module so production callsites don't need to know which mode they're in.

### Entry guard

Every `*/main.ts` invokes `applyMobileEntryGuard(currentPage)` before `createApp(...).mount(...)`:

```
isCapacitor()?          ── no  ──► render normally (browser path)
   │ yes
   ▼
hasValidConfig()?       ── no  ──► location.replace('/setup.html')
   │ yes
   ▼
currentPage is /login.html / /signup.html?
   │ yes ──► location.replace('/')        (mobile bypasses cookie login)
   │ no  ──► render normally (mobile path)
```

`hasValidConfig()` requires non-empty `base` and `token`. `validateRelayBase(base, allowInsecure)` is re-run defensively; mismatch (e.g., user tampered with localStorage) clears the config and redirects to setup.

### Setup page

New entry: `web/setup.html` + `web/src/setup/main.ts` + `web/src/setup/App.vue`.

Form fields:
- **Relay URL** — text input, placeholder `https://relay.example.com`
- **API token** — password-type input with show/hide toggle
- **Allow insecure HTTP/WS** — switch; only required to enable when base is `http://` non-loopback

Submit flow:
1. `validateRelayBase(base, allowInsecure)` → inline error if non-null.
2. Probe: `fetch(base + '/api/me', { headers: { Authorization: 'Bearer ' + token }, credentials: 'omit' })`. `/api/me` (not `/api/version`) is chosen because it requires authentication, so the probe simultaneously validates reachability AND that the token is valid.
   - Network error: surface "无法连接到 relay" with the underlying error.
   - 401: surface "API token 无效".
   - 403 with Origin-rejected hint or CORS preflight failure: surface "relay 未允许 capacitor://localhost origin"; include actionable copy referencing `ATTERM_ORIGINS`.
   - 200: `saveRelayConfig` + `location.replace('/')`.
3. Save persists `{base, token, allowInsecure}` to `localStorage` under key `atterm.relay`.

### Settings tab

Mobile mode shows an additional "Relay" tab in Settings exposing the same three fields, plus a "Disconnect / change relay" button that calls `clearRelayConfig()` and redirects to `/setup.html`. Browser mode does not show this tab.

## Components & Interfaces

### `web/src/shared/api/relay-config.ts` (new)

```ts
export interface RelayConfig {
  base: string          // 'https://relay.example.com'
  token: string         // 'atk_…'
  allowInsecure: boolean
}

export function isMobileApp(): boolean
export function loadRelayConfig(): RelayConfig | null
export function saveRelayConfig(cfg: RelayConfig): void
export function clearRelayConfig(): void

// Returns null on ok, error message string on failure.
// Rules mirror desktop/relay_security.go validateRelayEndpoint, adapted to http/https.
export function validateRelayBase(base: string, allowInsecure: boolean): string | null
```

`isMobileApp()` detection: `typeof (globalThis as any).Capacitor !== 'undefined' && (globalThis as any).Capacitor?.isNativePlatform?.() === true`. Cached after first call (Capacitor never disappears mid-session).

`validateRelayBase` accepts only `http://` and `https://` schemes. `http://localhost` and `http://127.0.0.1` are always allowed (loopback). Non-loopback `http://` requires `allowInsecure === true`. The parsed URL's `pathname` must be `/` or empty (no trailing path segments).

### `web/src/shared/api/client.ts` (modified)

`apiFetch` adds a mobile branch at the top:

```ts
if (isMobileApp()) {
  const cfg = loadRelayConfig()
  if (!cfg) throw new ApiError(0, 'relay_not_configured', null)
  url = cfg.base.replace(/\/$/, '') + path
  headers.set('Authorization', `Bearer ${cfg.token}`)
  init.credentials = 'omit'
  // skip CSRF: token auth doesn't need it
} else {
  url = path
  init.credentials = 'same-origin'
  if (method !== 'GET' && method !== 'HEAD' && cachedCsrf) {
    headers.set('X-CSRF-Token', cachedCsrf)
  }
}
```

401 handler:

```ts
if (res.status === 401) {
  if (isMobileApp()) {
    // mobile: token invalid, redirect to setup with the existing base prefilled
    location.replace('/setup.html?reason=token_invalid')
  } else {
    // existing behavior: redirect to /login.html?next=…
  }
}
```

### `web/src/shared/ws/client-conn.ts` (modified)

`wsUrl` adds a mobile branch:

```ts
function wsUrl(path: string): string {
  if (isMobileApp()) {
    const cfg = loadRelayConfig()
    if (!cfg) throw new Error('relay_not_configured')
    const u = new URL(cfg.base)
    const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${u.host}${path}`
  }
  // existing location-based logic
}
```

The `WebSocket` constructor call gains a subprotocol argument in mobile mode:

```ts
const ws = isMobileApp()
  ? new WebSocket(url, [loadRelayConfig()!.token])
  : new WebSocket(url)
```

`Sec-WebSocket-Protocol` token transport is the same mechanism desktop uses (AGENTS.md red-line 9).

### `web/src/shared/mobile-guard.ts` (new)

```ts
type EntryPage = 'home' | 'login' | 'signup' | 'settings' | 'admin' | 'setup'

// Returns true if a redirect was issued; caller must `return` and skip mount.
export function applyMobileEntryGuard(page: EntryPage): boolean
```

Decision table:

| isMobileApp | hasValidConfig | page    | action                          |
|-------------|----------------|---------|---------------------------------|
| false       | n/a            | any     | return false (browser path)     |
| true        | false          | setup   | return false (render setup)     |
| true        | false          | other   | replace('/setup.html')          |
| true        | true           | setup   | replace('/')                    |
| true        | true           | login   | replace('/')                    |
| true        | true           | signup  | replace('/')                    |
| true        | true           | other   | return false (render normally)  |

### Entry callsites

`web/src/main/main.ts`, `web/src/login/main.ts`, `web/src/signup/main.ts`, `web/src/settings/main.ts`, `web/src/admin/main.ts` each gain a leading:

```ts
import { applyMobileEntryGuard } from '@shared/mobile-guard'
if (applyMobileEntryGuard('<page>')) {/* redirect issued */} else { /* createApp(...).mount(...) */ }
```

### Vite config (modified)

`web/vite.config.ts` adds the new `setup.html` input to the `rollupOptions.input` map.

### Settings — Relay tab (new component)

`web/src/settings/tabs/Relay.vue`, rendered only when `isMobileApp() === true`. Fields mirror setup. Save uses the same probe-and-persist flow; disconnect calls `clearRelayConfig()` then `location.replace('/setup.html')`.

## Data Flow

### Cold start (no config)

```
WKWebView → capacitor://localhost/index.html
main/main.ts → applyMobileEntryGuard('home')
  isMobileApp=true, loadRelayConfig()=null
  location.replace('/setup.html')

setup/main.ts → applyMobileEntryGuard('setup') → renders setup page
user types base + token + (toggle) → submit
  validateRelayBase ok
  fetch GET base/api/me with Bearer
  200 → saveRelayConfig + location.replace('/')

main/main.ts → applyMobileEntryGuard('home')
  isMobileApp=true, loadRelayConfig()=cfg → render home

home composables call apiFetch('/api/sessions') → mobile branch → cfg.base+path with Bearer
session entry calls openWS('/client?…') → mobile branch → wss/ws to cfg.base.host with token subprotocol
```

### Token revoked mid-session

```
apiFetch returns 401
  → mobile branch redirect: /setup.html?reason=token_invalid
setup reads ?reason → shows banner; prefills base; clears token field
user re-pastes a fresh token → submit → continue as cold start
```

### User changes relay from Settings

```
Settings → Relay tab → Disconnect
  clearRelayConfig()
  location.replace('/setup.html')
```

## Error Handling Matrix

| Failure | Surface | Behavior |
|---|---|---|
| `apiFetch` called but no config (guard miss) | `ApiError(0, 'relay_not_configured', null)` | Caller layer redirects to setup |
| `apiFetch` 401 (mobile) | `ApiError(401, …)` | Side effect: `location.replace('/setup.html?reason=token_invalid')` |
| `apiFetch` network error | `ApiError(0, 'network_error', null)` | UI shows toast; setup page surfaces the underlying message |
| WS open fails | existing reconnect backoff | After 5 consecutive failures OR 30s elapsed since last successful open (whichever first), show banner "无法连接 relay。点此修改配置" linking to `/setup.html`. Banner dismisses automatically on next successful open. |
| `validateRelayBase` fail | string error | inline under the input, submit button disabled |
| Setup probe 403 / CORS preflight fail | thrown | inline error mentioning `ATTERM_ORIGINS` and `capacitor://localhost` |

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `validateRelayBase` | https ok, wss reject, http loopback ok, http non-loopback w/o insecure reject, http non-loopback w/ insecure ok, malformed URL reject | unit test (Vitest) under `web/src/shared/api/__tests__/relay-config.spec.ts` |
| `apiFetch` mobile path | URL prefixing, Bearer header, credentials omit, CSRF skip, 401 redirect target | Vitest with mocked `localStorage` + `globalThis.fetch` |
| `apiFetch` browser path | unchanged (regression guard) | extend existing tests if any; otherwise add a parallel spec |
| `wsUrl` | http→ws, https→wss, port preservation, browser fallback | unit test |
| `applyMobileEntryGuard` | 7-row decision table | table-driven Vitest |
| Setup flow | render, validate, probe success/failure | Vitest with `@vue/test-utils` (already a dep) |
| Manual / smoke | iOS simulator: setup → home → list sessions → open session → send/receive bytes | new "mobile smoke" section in `mobile/README.md` |

No relay-side test changes. CORS smoke is covered by the manual checklist (relay must be started with `ATTERM_ORIGINS=capacitor://localhost`).

## Operational / Documentation Changes

- `mobile/README.md`: remove the legacy "token panel" wording. Document the actual flow: launch app → setup screen → paste base + token (generated in desktop browser via Settings → API tokens) → connect. Keep the `ATTERM_ORIGINS=capacitor://localhost` example.
- `AGENTS.md`: extend the "何时改哪里" table with a row for "改移动 relay 配置" pointing at `web/src/setup/`, `web/src/shared/api/relay-config.ts`, `web/src/shared/mobile-guard.ts`, `web/src/settings/tabs/Relay.vue`.

## Risks & Open Questions

- **`localStorage` token exposure.** The token sits in WKWebView's local storage, accessible to any JS executed in the bundle. Acceptable for MVP because the bundle is signed and same-process; future hardening: move to Capacitor Preferences (encrypted on iOS) or Keychain plugin. Out of scope here, called out so the follow-up is tracked.
- **CORS preflight on `Authorization` header.** Browsers send a preflight for any custom auth header. Relay currently responds with `Access-Control-Allow-Origin: *` and lists `Authorization` in `Access-Control-Allow-Headers` (verified at `internal/relay/server.go:214-216`). No relay change required.
- **`credentials: 'omit'` is non-default.** Must be explicit; default `'same-origin'` would still attach cookies that don't exist anyway, but `omit` is the correct intent.
- **Mobile entry guard at every `main.ts`.** Easy to forget when adding a new page. Mitigation: add a lint-style check in the test suite that scans `web/src/**/main.ts` for the guard call.
- **iOS ATS.** Current `NSAllowsArbitraryLoads = true` is too permissive for App Store but fine for MVP. Explicit follow-up issue tracked separately.

## References

- `desktop/relay_security.go` — analogous URL validator for desktop, source of the `allowInsecure` semantics.
- AGENTS.md red-line 9 — relay security defaults and `Sec-WebSocket-Protocol` token transport.
- `mobile/README.md` — pre-existing iOS wrapper documentation (to be updated).
- `web/src/shared/api/client.ts:50-80` — current `apiFetch`.
- `web/src/shared/ws/client-conn.ts:197-201` — current `wsUrl`.
- `internal/relay/server.go:209-216` — relay CORS headers.
