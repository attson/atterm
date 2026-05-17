# Web Client Rewrite — Vue 3 + TypeScript + Naive UI

**Date:** 2026-05-17
**Status:** Approved (design)

## Goal

Replace the vanilla-JS / multi-file `web/` client with a Vue 3 + TypeScript + Naive UI codebase, keeping the existing page boundaries (`index`, `login`, `signup`, `settings`, `admin`) and the Go relay's static-file delivery story intact. End state: one TypeScript codebase, one bundle pipeline, no behavioural change to authentication, terminal streaming, or PWA install.

## Non-Goals

- Changing the page set or URL paths exposed by the relay
- Restyling the UI beyond what's needed to render with Naive UI (visual tokens stay the same)
- Touching `internal/proto` wire format, server-side cookie / CSRF logic, or the websocket endpoints `/agent`, `/uplink`, `/client`
- Sharing code between `desktop/frontend` and `web/` via a monorepo or workspace package
- Introducing a Playwright / browser-driver end-to-end suite (deferred to a later spec)
- Migrating `desktop/frontend` (already Vue 3 + TS) — out of scope; this spec is `web/` only

## Background

- `web/` today is hand-written ES module JS + multi-page HTML, served by Go via `http.FileServer(http.Dir(webDir))` (`internal/relay/server.go newStaticHandler`). The `--web` flag (`cmd/atterm-relay/main.go`) points at the source directory; there is no build step.
- AGENTS.md red-line 10 requires every asset be same-origin; xterm is vendored in `web/vendor/`, and a service worker pre-caches the bundle.
- A set of `node --test` files in `web/*.test.mjs` enforce contracts (no inline scripts, no raw colors, manifest fields, push flow, sw cache bump, etc.).
- `desktop/frontend/` already runs Vue 3 + Vite + TS, so the toolchain choice is consistent with the rest of the repo.

## Architecture

### Directory layout

```
web/                                # Vite project root (was: vanilla static site)
├── package.json
├── vite.config.ts                  # multi-entry build + vite-plugin-pwa
├── tsconfig.json / tsconfig.node.json
├── vitest.config.ts
├── index.html                      # entry 1: terminal home
├── login.html                      # entry 2
├── signup.html                     # entry 3
├── settings.html                   # entry 4
├── admin/index.html                # entry 5
├── public/                         # copied verbatim to dist root
│   ├── icon.png
│   └── icon.svg
├── src/
│   ├── main/                       # terminal home
│   │   ├── main.ts
│   │   ├── App.vue
│   │   └── components/
│   │       ├── SessionList.vue
│   │       ├── TerminalView.vue
│   │       ├── ShortcutBar.vue
│   │       ├── PasteFallback.vue
│   │       └── InstallHint.vue
│   ├── login/{main.ts, App.vue}
│   ├── signup/{main.ts, App.vue}
│   ├── settings/
│   │   ├── main.ts
│   │   ├── App.vue
│   │   └── tabs/
│   │       ├── ApiTokens.vue
│   │       ├── ChangePassword.vue
│   │       ├── Sessions.vue
│   │       └── DangerZone.vue
│   ├── admin/
│   │   ├── main.ts
│   │   ├── App.vue
│   │   └── tabs/{Users.vue, Invitations.vue}
│   ├── shared/
│   │   ├── api/
│   │   │   ├── client.ts           # apiFetch wrapper, 401 redirect, CSRF header
│   │   │   ├── auth.ts             # /auth/login, /auth/signup, /auth/logout
│   │   │   ├── me.ts               # /me, /me/sessions, password, delete
│   │   │   ├── admin.ts            # /admin/api/*
│   │   │   ├── push.ts             # webpush subscribe / unsubscribe
│   │   │   └── types.ts            # JSON DTOs, hand-mirrored from internal/relay/*_http.go
│   │   ├── ws/
│   │   │   ├── client-conn.ts      # /client WS, resize queue, replay progress
│   │   │   └── protocol.ts         # proto.Frame marshal/unmarshal (TS rewrite)
│   │   ├── components/{Topbar.vue, PageHeader.vue}
│   │   ├── theme/naive-theme.ts    # GlobalThemeOverrides binding to CSS tokens
│   │   ├── tokens.css              # :root --bg/--fg/--accent/... preserved
│   │   └── pwa.ts                  # registerSW from virtual:pwa-register
│   └── vendor/xterm/...            # moved from web/vendor/, served as static
├── tests/
│   ├── contract/                   # node --test, post-build assertions on dist/
│   │   ├── auth-pages.test.mjs
│   │   ├── no-inline-script.test.mjs
│   │   ├── no-raw-colors.test.mjs  # scans src/**/*.{css,vue}
│   │   ├── pwa-metadata.test.mjs
│   │   ├── sw-cache-bump.test.mjs
│   │   └── push-flow.test.mjs
│   └── unit/                       # vitest
│       ├── shared/api/client.test.ts
│       ├── shared/ws/protocol.test.ts
│       ├── shared/ws/client-conn.test.ts
│       ├── settings/tabs/*.test.ts
│       ├── admin/tabs/*.test.ts
│       └── main/components/TerminalView.test.ts
└── dist/                           # build output, gitignored
```

Go-side change: build pipeline syncs `web/dist/` to `internal/relay/web-dist/`, which the relay binary embeds:

```go
//go:embed all:web-dist
var embeddedWeb embed.FS

func EmbeddedWebFS() fs.FS {
    sub, _ := fs.Sub(embeddedWeb, "web-dist")
    return sub
}
```

`internal/relay/web-dist/` is committed to git (linguist-generated) so Docker builds need no Node. CI fails if the synced output drifts from source.

### Routing

Multi-page application: each entry HTML is its own Vue app. No `vue-router`. Each `<entry>.html` keeps `<meta name="page" content="...">` so `Topbar.vue` knows which page it's mounted on. The relay's path-based redirect (`/` and `/admin/` cookie gate in `newStaticHandler`) keeps working without modification.

### State

No Pinia. Each app has tightly-scoped state managed with `ref`/`reactive` + composables. Cross-component data inside one entry flows via composables under `src/<entry>/composables/` (added as needed).

### Auth & API

`apiFetch<T>(path, init)` in `src/shared/api/client.ts`:

- `credentials: 'include'`; JSON content-type by default
- Injects `X-Atterm-CSRF` header when the cookie has it
- 401 + current page is not `/login.html` or `/signup.html` → `window.location.assign('/login.html?next=' + encodeURIComponent(location.pathname + location.search))`
- 403 → throws `ApiError`; component decides UI (admin tabs use this to hide controls)
- 5xx / network → throws `ApiError`; component surfaces via Naive UI `useMessage()`
- Returns `{ data, status, headers }` so callers can read response metadata when needed

DTO types in `src/shared/api/types.ts` are hand-mirrored from `internal/relay/*_http.go`. No OpenAPI codegen — repo is small enough that drift is caught by integration usage and unit tests.

Cookie + CSRF flow is unchanged: relay still issues the cookie and CSRF token. Frontend never reads or stores tokens — it only forwards what's in the cookie jar.

### WebSocket / proto

`src/shared/ws/protocol.ts` reimplements `internal/proto.Frame` marshal/unmarshal in pure TypeScript. Wire compatibility is verified by binary fixtures (`web/tests/fixtures/proto/*.bin`), generated once by a small Go program from `internal/proto` and committed. `protocol.test.ts` round-trips every frame type against those fixtures.

`src/shared/ws/client-conn.ts` opens `/client?sid=...`:

- Cookie auth only — no `?token=`, no WS subprotocol (red-line 9)
- `sendResize(cols, rows)` queues while the socket is `CONNECTING`, flushes on `open`; if next outgoing matches the last sent dimensions, it skips (red-line 6 — predict-fork + queue-not-drop + skip-on-match)
- `sendInput`, `sendPasteImage`, `requestReplay` etc. correspond to existing frame types
- No reconnect loop; user refreshes (matches today's behaviour)

### UI theme

- `src/shared/tokens.css` keeps the existing `:root` CSS variables verbatim; visual palette is unchanged.
- `src/shared/theme/naive-theme.ts` returns a `GlobalThemeOverrides` that resolves the CSS variables via `getComputedStyle` and injects them into Naive UI's `common` / per-component overrides (`Button`, `Input`, `Card`, `Tabs`, `Tag`, `Modal`, `Form`).
- Each entry's `App.vue` wraps the page in `<n-config-provider :theme="darkTheme" :theme-overrides="overrides"> <n-message-provider> <n-dialog-provider> ... </n-config-provider>`.
- Business code never imports color constants from Naive UI; it references CSS variables or theme overrides. The updated `no-raw-colors` contract test scans `src/**/*.css` and the `<style>` blocks of `.vue` files; Naive UI's own bundled CSS is out of scope.

### PWA

- `vite-plugin-pwa` `strategies: 'generateSW'`, `registerType: 'autoUpdate'`
- `manifest` field of the plugin replaces the hand-written `manifest.webmanifest`
- Workbox `globPatterns: ['**/*.{js,css,html,svg,png,ico,woff2}']`; `navigateFallback: null` (MPA, no SPA fallback)
- xterm vendor and hashed assets go into precache; nothing fetches cross-origin

### Build & deploy

Vite multi-entry `rollupOptions.input` maps all five HTML files. `assetsInlineLimit: 0` so PWA can fully precache. `vue-tsc --noEmit` runs as part of `npm run build` (matches `desktop/frontend`).

`scripts/build-web.sh`:

```
cd web && npm ci && npm run build
rsync -a --delete web/dist/ internal/relay/web-dist/
```

CI runs the script before `go vet` / `go test` / docker build. A separate CI step verifies `git diff --exit-code internal/relay/web-dist` to catch drift.

`cmd/atterm-relay/main.go` flag semantics:

```
--web ""              # default: use embedded FS  (production)
--web web/dist        # override: serve from disk (dev parity)
--web some/other      # override: serve from disk (escape hatch)
```

`newStaticHandler(resolver, fs.FS)` — signature changes from `string` to `fs.FS`; callers in `cmd/atterm-relay` build the FS.

### Dev workflow

Two supported modes:

1. **Fast iterate** (frontend changes):
   - Terminal A: `ATTERM_BOOTSTRAP_ADMIN_EMAIL=... ATTERM_BOOTSTRAP_ADMIN_PASSWORD=... go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure`
   - Terminal B: `cd web && npm run dev` (Vite at 5173)
   - `vite.config.ts` `server.proxy` forwards `/auth`, `/me`, `/api`, `/admin/api`, `/agent`, `/uplink`, `/client`, `/sub` etc. to `http://127.0.0.1:8080`; WS endpoints use `ws: true`
2. **Closer to prod**:
   - `cd web && npm run build && go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --web web/dist --dev-insecure`
   - Or sync to `internal/relay/web-dist/` and run with `--web ""` (uses embed)

## Testing

### Contract tests (kept, migrated to `web/tests/contract/`)

| File | Guards | Notes |
|---|---|---|
| `auth-pages.test.mjs` | login/signup HTML structure, no `?token=` leakage | Asserts against `web/dist/login.html` / `dist/signup.html` |
| `push-flow.test.mjs` | Webpush frame sequence | Move logic to `src/shared/api/push.ts`; test the function |
| `sw-cache-bump.test.mjs` | SW precache changes between builds | Read generated SW; diff hash list against committed snapshot |
| `pwa-metadata.test.mjs` | Manifest required fields + icons | Assert against `dist/manifest.webmanifest` |
| `no-inline-script.test.mjs` | CSP compliance | Regex-scan `dist/**/*.html`; external/`type=module` scripts OK, inline `<script>...content...</script>` not OK |
| `no-raw-colors.test.mjs` | Business styles use CSS tokens | Scans `src/**/*.css` and `<style>` blocks in `.vue`; ignores Naive UI's own bundled CSS |

### Unit tests (new, vitest under `web/tests/unit/`)

- `shared/api/client.test.ts`: 401 redirect with `next=`, CSRF header injection, error branches (`ApiError` shape, 403 vs 5xx)
- `shared/ws/protocol.test.ts`: round-trips every frame type against `tests/fixtures/proto/*.bin`
- `shared/ws/client-conn.test.ts`: resize queue behavior (CONNECTING queue, open flush, skip-on-match) — the red-line 6 invariant
- `settings/tabs/{ApiTokens,Sessions,DangerZone,ChangePassword}.test.ts`: `@vue/test-utils` mount + mocked `apiFetch`; assert create / revoke / delete flows
- `admin/tabs/{Users,Invitations}.test.ts`: same pattern
- `main/components/TerminalView.test.ts`: mocked xterm + ws client; verifies predict-fork → initial resize flow (red-line 6)

### Test commands

```bash
cd web && npm test           # vitest run; also runs the .mjs contract files via a wrapper
cd web && npm run build      # includes vue-tsc --noEmit
go vet ./... && go test ./...
```

CI order: `npm ci → npm run build → npm test → rsync → git diff --exit-code internal/relay/web-dist → go vet → go test`.

### Deferred (out of scope this spec)

Playwright end-to-end across the five entries — adds a browser engine to CI; first verify that unit + contract coverage suffices.

## Phasing

### Phase A — Scaffolding (1 PR)

1. `web/` reshaped as Vite project root: `package.json`, `vite.config.ts`, `tsconfig*.json`, `vitest.config.ts`, `src/shared/`, empty `src/<entry>/main.ts` + `App.vue` for all five entries
2. `internal/relay/web_dist.go` + `EmbeddedWebFS()`; `server.go newStaticHandler` accepts `fs.FS`
3. `cmd/atterm-relay/main.go` builds `fs.FS` from `--web` (empty → embed, non-empty → `os.DirFS`)
4. `scripts/build-web.sh`; `internal/relay/web-dist/.gitkeep`
5. CI workflow: insert build step; docker stays as-is
6. During Phase A, the old `web/<page>.html` files stay alongside the Vite source so the relay can still serve them via `--web web` while we rewrite entries

Gate: `go test ./... && cd web && npm test && npm run build` all green; relay still serves the old UI.

### Phase B — Replace entries one at a time (4 PRs)

Order (simplest → riskiest):

1. **`login` + `signup`** (single PR — they share auth shape): form-only, no WS; validates the cookie/CSRF round-trip in the new stack
2. **`settings`**: 4 Naive UI tabs; densest forms but no streaming
3. **`admin`**: 2 tabs (Users, Invitations); behavior gated server-side, frontend just renders lists + actions
4. **`index`** (terminal home): xterm + WS + proto + paste/shortcut; highest risk, last to land

For each entry:

- Implement under `src/<entry>/`, write vitest tests, get green
- `npm run build`, sync `internal/relay/web-dist/`
- Boot relay with `--web ""`, manual smoke
- Remove the old `web/<entry>.html` + `<entry>.js` files; remove from any contract test glob
- Full gate: contract tests, vitest, `go vet`, `go test`, `vue-tsc`

Each Phase B PR is independently shippable and revertable.

### Phase C — Cutover (1 PR)

1. Switch `--web` default to empty (embed) — old `--web web` no longer serves anything useful
2. Delete remaining vanilla assets: `web/style.css` (already moved to tokens), `web/app.js`, `web/auth.js`, `web/layout.js`, `web/sw.js`, `web/manifest.webmanifest`, `web/*.test.mjs` files that did not migrate
3. README + AGENTS.md updates: mark `web/` as "Vue 3 + TypeScript + Naive UI"; preserve red-line 10's "no CDN" wording; update the "何时改哪里" row for web safety headers / static assets to reference `web/src/`
4. Final manual smoke across all five pages with `--web ""`

## Invariants (must hold across PRs)

- **proto wire compatibility**: any frame change updates both `internal/proto` and `web/src/shared/ws/protocol.ts`, plus the fixtures, plus `docs/spec/protocol.md` (red-line 4)
- **resize three-piece coupling**: predict-fork in `NewSession` (cols/rows from frontend probe) + CONNECTING-time queue in `client-conn.ts` + skip-on-match in `TerminalView.vue` are tested together; touching one without the others is a regression (red-line 6)
- **no CDN**: all bundled assets are same-origin; xterm stays under `src/vendor/`; Naive UI ships through npm into the Vite bundle, not a `<script>` tag (red-line 10)
- **no token in URL**: `?token=` is never used for auth endpoints; cookie + CSRF only on browser; agent WS uses `Sec-WebSocket-Protocol` (red-line 9)
- **`internal/relay/web-dist/` is generated**: never edit by hand; CI fails the build if it drifts from source

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| proto TS implementation drifts from Go wire | Binary fixtures generated by a Go program, committed; `protocol.test.ts` round-trips |
| Terminal resize regression (red-line 6) | `client-conn.test.ts` + `TerminalView.test.ts` both assert the queue / skip-on-match contract |
| PWA cache pins users to a stale build | `registerType: 'autoUpdate'` + content-hashed assets per build + `sw-cache-bump` contract test |
| `internal/relay/web-dist/` falls out of sync with `web/` source | CI step: build + rsync + `git diff --exit-code internal/relay/web-dist` |
| Bundle bloat from Naive UI + Vue per-entry | Vite's tree-shake + per-entry bundles; revisit only if `dist/` size measurably regresses (e.g. terminal entry > 500 KB gzip) |
| Dev mode WS proxying breaks against `/client` | `vite.config.ts` `server.proxy` with `ws: true`; alternative `--web web/dist` flow always works as fallback |

## Open questions

None blocking. Will resurface in the implementation plan if anything turns up.
