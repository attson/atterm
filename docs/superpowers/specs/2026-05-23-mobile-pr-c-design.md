# PR-C: Mobile Setup + Session List + Terminal (Attach-Only Client) — Design

Date: 2026-05-23
Status: Draft (pending implementation plan)
Parent spec: `docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md` (PR-C row)

## Background

PR-A built the `platform/` adapter; PR-B added `platform/capacitor.ts`, the multi-target Vite build, and a `MobilePlaceholder.vue` boot. PR-C replaces the placeholder with the real mobile UX: configure a relay, list remote sessions, and attach to one to send/receive terminal I/O — an **attach-only** client (no local PTY, per the parent spec).

Mobile must use **desktop frontend code**, not the browser PWA (`web/`), per the project owner's directive ("iOS/Android share desktop code; browser stays independent"). The reusable desktop primitive is `desktop/frontend/src/lib/connection.ts` — a wails-free WebSocket client (`SessionConnection`) that takes an `Endpoint { url, token }` and handles ATTACH / IN / OUT / RESIZE / REPLAY / token-subprotocol auth. The 672-line `TerminalView.vue` is NOT reused: it depends on `lib/api` (Wails), the plugin host, WebGL preference, and desktop context-menu logic. Instead PR-C builds a lean `MobileTerminal.vue` atop `SessionConnection`.

## Goals

- Mobile boots to: **setup** (no config) → **session list** (configured) → **terminal** (session selected).
- Session list grouped by host, sourced from `GET {base}/api/sessions` (Bearer auth, the path PR #66 established).
- Lean `MobileTerminal.vue` (xterm + FitAddon + `SessionConnection`) — no plugin host, no WebGL pref, no desktop context menu.
- **Keepalive**: opened terminals stay attached when you navigate back; switching between open terminals is instant (no reconnect, no history replay). Soft LRU cap of 4 open terminals.
- Single-root SPA navigation: one `MobileApp.vue` with internal `view` state; no vue-router, no multi-HTML-entry.
- iOS smoke (cold start → setup → list → attach → send/receive → back → switch → token-invalid → disconnect) passes.
- Desktop unchanged. `web/` unchanged. PR-A/PR-B interfaces consumed unchanged.

## Non-Goals

- Reusing `web/`'s setup/terminal code (different stack: web uses naive-ui; desktop/mobile use plain Vue).
- Plugin host / file explorer / quick-input / context menu / WebGL renderer on mobile.
- Local PTY creation (`newSession`) — attach-only.
- PaneGrid / multi-pane layout on mobile.
- Android target + push notifications (PR-D).
- The WS `/client-sessions` push channel — mobile uses simpler `GET /api/sessions` polling + pull-to-refresh.
- Secure token storage beyond `localStorage` (PR-D may move to Capacitor Preferences).

## Constraints

- AGENTS.md red-line 4 (wire compat): no new frames; `SessionConnection` already speaks the existing protocol.
- AGENTS.md red-line 9: token via `Sec-WebSocket-Protocol` (SessionConnection already does this); HTTP via `Authorization: Bearer`; `Allow insecure` switch reused from PR-B for `ws://`/`http://` non-loopback.
- No new top-level runtime dependencies (xterm + addons already in `desktop/frontend`).
- Capacitor single `index.html` entry (PR-B); navigation is in-app state, not routes.

## Architecture

### Component tree (capacitor target)

```
main.capacitor.ts
  └── MobileApp.vue                      (root; replaces MobilePlaceholder)
        ├── view='setup'    → MobileSetup.vue        (relay URL + token + insecure)
        ├── view='list'     → MobileSessionList.vue  (host-grouped, pull-to-refresh)
        └── view='terminal' → MobileTerminalHost.vue (tab strip + N keepalive MobileTerminal)
                                  └── MobileTerminal.vue × N   (v-show toggled; xterm + SessionConnection)
```

`MobileApp.vue` owns the navigation + keepalive state. `MobileTerminalHost.vue` keeps all open `MobileTerminal` instances mounted (`v-show`, not `v-if`) so their xterm + WS persist across switches and back-navigation.

### State (in MobileApp.vue)

```ts
type View = 'setup' | 'list' | 'terminal'
const view = ref<View>('setup')
const setupReason = ref<'token_invalid' | null>(null)

// Keepalive registry. Insertion order = LRU recency (re-activating moves to end).
interface OpenTerminal { sessionId: string; info: RemoteSession }
const openTerminals = ref<OpenTerminal[]>([])   // capped at MAX_OPEN_TERMINALS (4)
const activeSessionId = ref<string | null>(null)
```

`MobileTerminalHost` renders one `MobileTerminal` per `openTerminals` entry, shows the one matching `activeSessionId`, hides the rest via `v-show`. Each `MobileTerminal` owns its own `SessionConnection` (created on mount, detached on unmount). Because they stay mounted, the connection stays attached.

### Keepalive lifecycle

| Action | Effect |
|---|---|
| Tap a session (not open) | push to `openTerminals`; if length would exceed 4, evict (unmount → detach) the least-recently-active entry first; set active; `view='terminal'` |
| Tap a session (already open) | set active (move to end of LRU order); `view='terminal'` — instant, no reconnect |
| Tab strip: tap another open tab | set active; instant switch |
| Tab strip: tap `×` | remove from `openTerminals` → MobileTerminal unmounts → `SessionConnection.detach()`; if it was active, activate the next/prev open one, or go to list if none left |
| `‹ back` | `view='list'`; openTerminals untouched (all stay attached) |
| Session ends (CLOSE frame) | MobileTerminal emits `ended`; MobileApp removes it from `openTerminals` (same as ×) |
| Token invalid (any 401 from fetch or a WS auth failure) | detach all; clear active; `view='setup'`, `setupReason='token_invalid'` |

`MAX_OPEN_TERMINALS = 4` (a module constant; soft cap with LRU eviction). Rationale: each open terminal holds one WS + one xterm; 4 is comfortable on a phone and matches typical "a couple of long tasks" usage.

## Components & Interfaces

### `MobileApp.vue` (new) — root, owns navigation + keepalive

Props: none (mounted by `main.capacitor.ts`). Uses `usePlatform()`.

On mount: `view = (await platform.relay.load()) ? 'list' : 'setup'`.

Exposes handlers down to children:
- `onConnected()` (from MobileSetup) → `view='list'`
- `onOpenSession(info: RemoteSession)` → keepalive logic above → `view='terminal'`
- `onBack()` → `view='list'`
- `onSwitch(sessionId)` / `onClose(sessionId)` (from MobileTerminalHost)
- `onTokenInvalid()` → detach all, `view='setup'`, reason set

### `MobileSetup.vue` (new) — relay config

Fields: Relay URL, API token (password), Allow-insecure switch, Connect button. Plain Vue + desktop CSS tokens (not naive-ui).

Validation: a small `validateRelayBase(url, allowInsecure)` helper — ported from `web/src/shared/api/relay-config.ts`'s validator into `desktop/frontend/src/mobile/relay-validate.ts` (http/https only; non-loopback http requires the insecure flag; reject path/query/fragment). Unit-tested.

Submit: `validateRelayBase` → if ok, `platform.relay.save({url, token, allow_insecure_relay, ...})` then `platform.relay.fetchMe()` to verify (GET `{base}/api/me`, Bearer). 200 → `onConnected()`. 401 → "API token is invalid". Network/403 → inline error mentioning `ATTERM_ORIGINS`/`capacitor://localhost`. (Mirrors PR #66's proven flow.)

`?reason=token_invalid` equivalent: `MobileApp` passes `reason` prop → banner.

### `MobileSessionList.vue` (new) — host-grouped list

Props: `openSessionIds: string[]` (to render the "已打开/open" badge).
Emits: `open(info)`, `refresh`, `editRelay` (gear).

On mount + on pull-to-refresh: `platform.sessions.listRemoteSessions()`. Group by `info.host`; render a group header (`host · user · count`) then each session row (title + `cols×rows`, live dot). Rows for currently-open sessions show an "open" badge. Empty state: "no remote sessions — start one from a desktop AT Term connected to this relay."

### `platform.sessions.listRemoteSessions()` — wire the PR-B stub

PR-B left `listRemoteSessions: async () => []`. PR-C implements it in `platform/capacitor.ts`:

```ts
listRemoteSessions: async (): Promise<RemoteSession[]> => {
  const cfg = loadCfg()
  if (!cfg) return []
  const base = cfg.url.replace(/\/$/, '')
  const res = await fetch(base + '/api/sessions', {
    headers: { Authorization: `Bearer ${cfg.token}` },
    credentials: 'omit',
  })
  if (res.status === 401) throw new Error('relay_unauthorized')   // MobileApp matches this → routes to setup
  if (!res.ok) throw new Error(`list sessions: HTTP ${res.status}`)
  const raw = await res.json()
  return raw.map(toRemoteSession)   // map relay SessionInfo → platform RemoteSession
}
```

`toRemoteSession` maps the relay's `SessionInfo` JSON (`session_id`, `host`, `user`, `title`/command, `cols`, `rows`, `host_id`) to the `RemoteSession` type defined in PR-A's `platform/types.ts`. Fields absent from the relay payload default sensibly (e.g. title falls back to command).

### `MobileTerminalHost.vue` (new) — tab strip + keepalive container

Props: `openTerminals: OpenTerminal[]`, `activeSessionId: string`.
Emits: `switch(sessionId)`, `close(sessionId)`, `back`, `ended(sessionId)`, `tokenInvalid`.

Renders the top tab strip (one tab per open terminal, active highlighted, `×` to close) and one `<MobileTerminal v-show="t.sessionId===activeSessionId">` per open terminal. Forwards `ended`/`tokenInvalid` up.

### `MobileTerminal.vue` (new) — lean xterm view

Props: `endpoint: Endpoint` (`{ url: wss-base, token }`), `sessionId`, `info: RemoteSession`, `active: boolean`.
Emits: `ended`, `tokenInvalid`.

On mount: build `Terminal` + `FitAddon`, open in container, create `SessionConnection(endpoint, sessionId, handlers)`, `attach()`. `onData` → `conn.sendInput`. `onResize` → `conn.sendResize`. Handlers: `onOutput` → `term.write`; `onClose` → emit `ended`; `onStatus` → status pill. A WS auth rejection (mapped from connection error/close code) → emit `tokenInvalid`.

On unmount (when removed from openTerminals): `conn.detach()`, `term.dispose()`.

Watches `active`: when becoming active, `fit()` + `focus()` (xterm was hidden via v-show so it couldn't size while hidden).

Bottom auxiliary key bar (esc / tab / ctrl / ⌥ / arrows / ⌃C) for keys the mobile soft keyboard lacks — sends the corresponding control sequence via `conn.sendInput`.

Endpoint construction: `MobileApp` derives `{ url, token }` from the saved relay config per `SessionConnection`'s `Endpoint` contract (`url` is the relay WS base — `wss://{host}` from an `https` relay, `ws://{host}` when `allow_insecure_relay`; `SessionConnection` forms the `/client` path + session id + auth subprotocol itself). `token = cfg.token`. The exact base/path split is verified against `lib/connection.ts` in the first implementation task so we don't double-append `/client`.

### Wiring `main.capacitor.ts`

Change the mount target from `MobilePlaceholder` to `MobileApp`. `MobilePlaceholder.vue` is deleted (its boot role is superseded). `MobilePlaceholder.test.ts` removed with it.

## Data Flow

### Cold start
```
main.capacitor.ts → initPlatform(createCapacitorPlatform) → mount MobileApp
MobileApp.onMounted: relay.load() == null → view='setup'
MobileSetup: user fills base+token+insecure → Connect
  validateRelayBase ok → relay.save(cfg) → relay.fetchMe() 200 → onConnected → view='list'
MobileSessionList.onMounted: listRemoteSessions() → group by host → render
user taps "claude" → MobileApp.onOpenSession(info):
  not open → push {sessionId, info}; active=sessionId; view='terminal'
MobileTerminalHost renders MobileTerminal(endpoint, sessionId) → SessionConnection.attach()
  → ATTACH → relay replays scrollback → OUT frames → term.write
user types → onData → conn.sendInput → IN frame
```

### Switch + keepalive
```
user taps ‹ back → view='list' (terminal stays mounted+attached, v-show=false)
user taps "codex" (a different host, not yet open) → push; active=codex; view='terminal'
  (claude's MobileTerminal still mounted+attached in background)
user taps tab "claude" → active=claude; instant (no reconnect, scrollback already in xterm)
user opens a 5th session → openTerminals.length would be 5 → evict LRU (oldest non-active) → detach → push new
```

### Token invalidation
```
any listRemoteSessions()/fetchMe() returns 401, OR a MobileTerminal WS auth-fails
  → MobileApp.onTokenInvalid(): every MobileTerminal unmounts (detach), openTerminals=[],
    view='setup', reason='token_invalid' → banner
```

## Error Handling Matrix

| Failure | Behavior |
|---|---|
| `relay.load()` null at boot | `view='setup'` |
| setup `validateRelayBase` fail | inline error under URL field; Connect disabled-equivalent (no save) |
| setup `fetchMe` 401 | inline "API token is invalid" |
| setup `fetchMe` 403 / CORS | inline error referencing `ATTERM_ORIGINS` + `capacitor://localhost` |
| setup `fetchMe` network error | inline "Cannot reach relay: <msg>" |
| `listRemoteSessions` 401 | `onTokenInvalid()` → setup with banner |
| `listRemoteSessions` network/5xx | list shows inline error + retry; keeps last-good list if any |
| `listRemoteSessions` empty | empty-state hint |
| MobileTerminal WS auth-fail | emit `tokenInvalid` → `onTokenInvalid()` |
| MobileTerminal WS drop (non-auth) | `SessionConnection`'s existing reconnect/backoff (status pill → reconnecting) |
| Session ends (CLOSE) | emit `ended` → remove from openTerminals; if active, fall back to next open or list |
| 5th terminal opened | LRU-evict oldest non-active before adding |

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `relay-validate.ts` | https ok / wss reject / http loopback ok / http non-loopback w/o insecure reject / w insecure ok / path-query-fragment reject / malformed | Vitest unit |
| `capacitor.listRemoteSessions` | URL+Bearer+credentials, 401 throws, SessionInfo→RemoteSession map, empty | Vitest mock fetch |
| `MobileSetup.vue` | renders fields; validate inline; 401/403/network errors; success → emit connected + relay.save called | @vue/test-utils + mock platform |
| `MobileSessionList.vue` | host grouping, open-badge, empty state, pull-to-refresh calls listRemoteSessions, emit open(info) | @vue/test-utils + mock platform |
| `MobileApp.vue` keepalive | open→back→reopen instant (no new connection); LRU eviction at 5th; close active falls back; tokenInvalid resets to setup; switch is instant | @vue/test-utils with a stubbed MobileTerminal (assert mount/unmount counts = attach/detach) |
| `MobileTerminal.vue` | builds Terminal + SessionConnection on mount, sendInput on data, detach+dispose on unmount, fit+focus on becoming active, emit ended on CLOSE, emit tokenInvalid on auth-fail | @vue/test-utils + mock SessionConnection |
| Caps gate (PR-B) | unaffected | existing |
| End-to-end | iOS simulator smoke checklist (below) | manual |

### iOS smoke checklist (PR-C)
1. Cold start, no config → setup screen.
2. Bad token → "API token is invalid"; not navigated away.
3. Valid token → session list, grouped by host.
4. Tap a session → terminal attaches; output shows; typing echoes.
5. `‹ back` → list; tap same session → **instant** (no reconnect/replay).
6. Open a second session (different host) → tab strip shows both; tap between tabs → instant switch.
7. Open 5 sessions → oldest auto-detaches (still ≤4 tabs).
8. `×` a tab → that terminal closes; others unaffected.
9. Revoke token on relay → next refresh/op → back to setup with "token invalid" banner.
10. Gear → setup → Disconnect → setup with empty fields.

## File Structure

**New (`desktop/frontend/src/mobile/`):**
- `MobileApp.vue` — root + navigation + keepalive registry
- `MobileSetup.vue`
- `MobileSessionList.vue`
- `MobileTerminalHost.vue`
- `MobileTerminal.vue`
- `relay-validate.ts` — `validateRelayBase`
- `__tests__/*.test.ts` for each unit above

**Modified:**
- `desktop/frontend/src/main.capacitor.ts` — mount `MobileApp` instead of `MobilePlaceholder`
- `desktop/frontend/src/platform/capacitor.ts` — implement `listRemoteSessions` (+ a typed 401 error for routing)
- `desktop/frontend/src/platform/types.ts` — if `RemoteSession` needs a field the relay provides (verify during impl)
- `mobile/README.md` — PR-C smoke checklist replaces the PR-B placeholder note

**Deleted:**
- `desktop/frontend/src/MobilePlaceholder.vue` + `src/__tests__/MobilePlaceholder.test.ts` (superseded)

**NOT touched:** `lib/api.ts`, `lib/connection.ts` (consumed as-is), `platform/{index,wails}.ts`, desktop components, `web/`.

## Risks & Open Questions

- **`GET /api/sessions` Bearer auth:** PR #66 established Bearer HTTP auth against the relay; PR-C assumes the same path authorizes `/api/sessions`. If the relay scopes `/api/sessions` to cookie-only, PR-C's plan adds the minimal relay change to accept the API token there — verify early in implementation.
- **WS auth-failure detection:** `SessionConnection` reports status/close; mapping a close code to "token invalid" vs "transient drop" needs care so we don't bounce to setup on a flaky network. Plan task will inspect the close codes the relay sends on auth rejection.
- **xterm sizing while hidden:** a `v-show`-hidden xterm can't measure; `MobileTerminal` must `fit()` on becoming active. Covered in the component contract + test.
- **Keepalive memory:** 4 × (WS + xterm) is fine; the LRU cap bounds it. If profiling shows pressure, lower the cap (single constant).
- **`MobilePlaceholder` deletion:** confirms PR-B's boot is fully superseded; the PR-B caps-gating groundwork in App.vue/SettingsDialog stays (used when PR mounts desktop-derived components later, if ever).
