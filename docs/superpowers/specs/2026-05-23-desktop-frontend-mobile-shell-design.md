# Desktop Frontend as Mobile Shell — Design

Date: 2026-05-23
Status: Draft (pending implementation plan)

## Background

`desktop/frontend/` is the Vue 3 + TypeScript + xterm + Naive UI codebase that ships inside the Wails desktop app. `web/` is a separate (much smaller) Vue 3 PWA that browsers use to attach to remote sessions. PR #66 added a thin Capacitor wrapper at `mobile/` that bundles `web/dist/` for an iOS WebView app.

We want iOS and Android to ship the **desktop** UX (full terminal, session list, settings) — not the lean browser UX — while keeping the browser PWA as a separate, lighter codebase. Wails v2 has no mobile target, and Wails v3's mobile story is not production-ready (as of 2026-05). The pragmatic path is to make `desktop/frontend/` build for **two** targets: native (Wails desktop) and Capacitor (iOS/Android WebView).

This spec defines the abstraction layer that lets the same Vue source compile for both — a `platform/` adapter with two implementations (`wails`, `capacitor`) and a `capabilities` flag set that the UI consults for conditional rendering.

## Goals

- Same `desktop/frontend/` source builds for desktop (Wails) and mobile (Capacitor) targets, selected by `VITE_TARGET` env var.
- All current Wails IPC and runtime calls (22 import sites across 12+ files) route through a typed `Platform` interface; the existing `wailsjs/` generated layer is referenced only inside `platform/wails.ts`.
- Mobile gets a usable subset: relay configuration, remote session list, attach to a remote session (send keys, receive output, replay history). No local PTY, no auto-update, no plugin host, no window controls.
- Browser PWA at `web/` continues unchanged; it is not part of this architecture.
- Existing desktop behavior is byte-for-byte preserved (single chokepoint refactor, not a rewrite).
- AGENTS.md red-lines 4 / 5 / 9 / 10 stay clean.

## Non-Goals

- Wails v3 migration.
- Tauri migration.
- Browser PWA changes (other than a follow-up to clean up the now-dead Capacitor branch in `web/src/shared/api/client.ts` — separate spec).
- Reverse RPC (Capacitor JS → native plugin → relay) — mobile talks to the relay directly via fetch + WebSocket like the browser PWA does today.
- Encrypted local storage of API tokens beyond what Capacitor Preferences gives you natively (already encrypted at rest on iOS).
- Mobile-specific UX redesign of the desktop pages (e.g., PaneGrid stays as-is for now; if it doesn't fit small screens it becomes a follow-up).

## Constraints

- AGENTS.md red-line 4 (wire compatibility): no new frames, no payload changes.
- AGENTS.md red-line 5 (`internal/` ⇏ `desktop/`): the new `platform/` layer is frontend-only; no Go changes required.
- AGENTS.md red-line 9 (relay security defaults): mobile reuses the `Allow insecure HTTP/WS` switch from PR #66; Android target picks up the same logic.
- AGENTS.md red-line 10 (no CDN in web): `desktop/frontend/` already only uses local deps; the Capacitor bundle inherits that.
- Wails v2 stays the desktop runtime.
- Capacitor stays at v8.x; no version bump.

## Architecture

### Three-target layout

```
                       desktop/frontend/src/  (single Vue codebase)
                                  │
       ┌──────────────────────────┼──────────────────────────┐
       │ VITE_TARGET=wails        │ VITE_TARGET=capacitor    │
       ▼                          ▼
 desktop (Wails v2)         iOS + Android (Capacitor)
       │                          │
       │ Go backend +             │ no native code (yet),
       │ wailsjs IPC              │ WKWebView + JS bridge
       ▼                          ▼
 internal/relay (本地 mini)  ─远端→  atterm-relay (远端) ←─
       │                                            │
       └───────── 远端 session 双向同步 ─────────────┘

                       web/  (independent, unchanged)
                                  │
                                  ▼  PWA, same-origin to hosting relay
                          browser users
```

### Build targets

| Target | Entry build | Output | Consumed by |
|---|---|---|---|
| `wails` | `npm run build:wails` (Vite with `VITE_TARGET=wails`) | embedded into Wails binary | Wails desktop |
| `capacitor` | `npm run build:capacitor` (Vite with `VITE_TARGET=capacitor`) | `dist/` | `mobile/` Capacitor project, synced into `mobile/www` |

`mobile/scripts/sync-web.mjs` is updated to sync `desktop/frontend/dist` (not `web/dist`) and to invoke `npm run build:capacitor` in `desktop/frontend/`.

### PR sequencing

This is one spec with one writing-plans output, but the plan internally produces 4 PRs. Each PR landed leaves the tree in a working state.

| PR | Scope | Post-state |
|---|---|---|
| PR-A | `platform/types.ts` + `platform/wails.ts` + `platform/index.ts`; rewrite all 22 `wailsjs/*` import sites to consume `usePlatform()` | Desktop unchanged; mobile not yet buildable; all desktop tests green |
| PR-B | `platform/capacitor.ts` + capabilities gating in UI (hide Settings tabs / TitleBar / plugin pages when caps say so); Vite multi-target build config; `mobile/` repointed at `desktop/frontend` | Mobile boots in iOS simulator to a "configure relay" state; desktop unchanged |
| PR-C | Mobile setup page (port of PR #66's `setup/App.vue` into `desktop/frontend/src/setup/`) + mobile session list adaptation + attach path working end-to-end | iOS smoke checklist (6 items, same as PR #66) passes; Android smoke deferred |
| PR-D | Android target boot (Capacitor `android` platform add) + push notifications via `@capacitor/local-notifications` wired through `platform.system.showNotification` | iOS + Android both ship a working attach-only client with notifications |

### Per-PR scope is fixed in the writing-plans output, not negotiated in implementation.

**Note for the writing-plans phase:** the natural output of this spec is **four implementation plans, one per PR** (PR-A through PR-D), rather than a single mega plan. Each plan should produce working, shippable software on its own. PR-A is the foundation; PR-B/C/D each consume PR-A's `platform/` interface unchanged.

## Components & Interfaces

### `desktop/frontend/src/platform/types.ts` (new — single source of truth)

```ts
export interface Capabilities {
  localPty: boolean
  autoUpdate: boolean
  pluginHost: boolean
  windowControls: boolean
  systemClipboard: boolean
  notifications: boolean
  fileDialog: boolean
}

export interface RelayBridge {
  load(): Promise<RelayConfig | null>
  save(cfg: RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
}

export interface SessionBridge {
  newSession?(req: NewSessionReq): Promise<NewSessionResp>
  closeSession(sessionID: string): Promise<void>
  listShells(): Promise<string[]>
  listRemoteSessions(): Promise<RemoteSession[]>
}

export interface SystemBridge {
  showNotification(title: string, body: string): Promise<void>
  getClipboardPaste(): Promise<ClipboardPastePayload>
  pickLogFilePath?(): Promise<string>
  openExternalURL(url: string): Promise<void>
  getEnvironment(): Promise<Record<string, string>>
  windowMinimize?(): Promise<void>
  windowToggleMaximize?(): Promise<void>
  windowIsMaximized?(): Promise<boolean>
  quit?(): Promise<void>
}

// Implementation note (discovered during PR-A): window control methods
// (windowMinimize, windowToggleMaximize, windowIsMaximized, quit) and
// getEnvironment() were rolled into SystemBridge rather than a separate
// WindowControlsBridge. They are optional so the Capacitor implementation
// (PR-B onwards) can safely omit them; caps.windowControls gates all
// window-control UI. getEnvironment() is non-optional because it is
// needed by the Wails platform at init time to detect the runtime
// environment.

export interface EventBus {
  on(event: string, handler: (data: unknown) => void): () => void
  emit(event: string, data: unknown): void
}

export interface UpdaterBridge {
  getState(): Promise<UpdateState>
  checkUpdate(): Promise<void>
  startDownload(): Promise<void>
  installUpdate(): Promise<void>
}

export interface PluginHostBridge {
  getPluginConfig(): Promise<PluginConfig>
  setPluginConfig(cfg: PluginConfig): Promise<void>
  // PluginFS surface, 1:1 with wailsjs/go/main/PluginFS exports:
  // Note (corrected during PR-A implementation): watchDir returns a numeric
  // watcher ID (Promise<number>); unwatchDir takes that ID (Promise<void>).
  // readFile accepts an optional maxBytes second argument to cap read size.
  fs: {
    listDir(path: string): Promise<DirEntry[]>
    watchDir(path: string): Promise<number>
    unwatchDir(id: number): Promise<void>
    readFile(path: string, maxBytes?: number): Promise<string>
    fileMeta(path: string): Promise<FileMeta>
  }
}

export interface Platform {
  caps: Capabilities
  relay: RelayBridge
  sessions: SessionBridge
  system: SystemBridge
  events: EventBus
  updater?: UpdaterBridge
  pluginHost?: PluginHostBridge
}
```

All shared data types (`RelayConfig`, `RelayMe`, `NewSessionReq`, `NewSessionResp`, `RemoteSession`, `ClipboardPastePayload`, `UpdateState`, `PluginConfig`) move from `wailsjs/go/models.ts` into `platform/types.ts`. The Wails impl converts between Wails-generated shapes and the platform types where needed (most are 1:1).

### `desktop/frontend/src/platform/wails.ts` (new)

Single import surface that wraps every `wailsjs/go/main/App` export, `wailsjs/go/main/PluginFS` export, and `wailsjs/runtime/runtime` export used today. No other file in the codebase imports `wailsjs/*`.

```ts
export function createWailsPlatform(): Platform { /* see §2 of brainstorm */ }
```

### `desktop/frontend/src/platform/capacitor.ts` (new)

Same shape, different backends:
- `relay.load/save/clear` → `@capacitor/preferences` keyed `atterm.relay`.
- `relay.fetchMe` → `fetch(base + '/api/me', { Authorization: 'Bearer ' + token, credentials: 'omit' })`.
- `sessions.closeSession` / `listRemoteSessions` → relay HTTP API with Bearer.
- `sessions.newSession` is undefined; UI guards via `caps.localPty`.
- `system.showNotification` → `@capacitor/local-notifications`.
- `system.getClipboardPaste` → `@capacitor/clipboard`.
- `system.openExternalURL` → `@capacitor/browser`.
- `events` → in-process `mitt` (3 KB) or hand-rolled `EventTarget` wrapper; mobile has no Go-to-frontend event source.
- `updater` and `pluginHost` are undefined.

### `desktop/frontend/src/platform/index.ts` (new)

```ts
let _platform: Platform | null = null

export function initPlatform(): Platform {
  if (_platform) return _platform
  _platform = (import.meta.env.VITE_TARGET === 'capacitor')
    ? createCapacitorPlatform()
    : createWailsPlatform()
  return _platform
}

export function usePlatform(): Platform {
  if (!_platform) throw new Error('platform not initialized; call initPlatform() in main.ts first')
  return _platform
}

// Test helper
export function __setPlatformForTests(p: Platform | null) { _platform = p }
```

`main.ts` calls `initPlatform()` before `createApp(App).mount('#app')` and provides it via Vue `provide('platform', platform)` plus stashes on `app.config.globalProperties.$platform` so non-setup contexts can reach it.

### Capabilities-driven UI

Every component currently importing from `wailsjs/*` is rewritten to use `usePlatform()`. Conditional rendering uses `caps.*`:

- `SettingsDialog.vue` — `<n-tab-pane v-if="caps.autoUpdate">`, `v-if="caps.pluginHost"`, etc.
- `TitleBar.vue` + `WindowControls.vue` — top-level `v-if="caps.windowControls"` wrapper.
- `TabBar.vue` "New tab" button — `:disabled="!caps.localPty"`; tooltip "available on desktop only".
- `PaneGrid.vue` — pane split shortcuts no-op on mobile (no keyboard); pane creation guarded by `caps.localPty`.
- `SettingsLogging.vue` "Pick file" button — hidden when `!caps.fileDialog`.
- Plugin pages (FileExplorer, QuickInput) — entire `v-if="caps.pluginHost"` wrapping in `App.vue`.

### Vite config

`desktop/frontend/vite.config.ts` gains a target switch:

```ts
const target = process.env.VITE_TARGET ?? 'wails'  // default = wails for `wails dev`

export default defineConfig({
  // shared plugins, alias, etc.
  define: { 'import.meta.env.VITE_TARGET': JSON.stringify(target) },
  build: {
    outDir: target === 'capacitor' ? 'dist-capacitor' : 'dist',
    // ...
  },
  // wails-only and capacitor-only plugins gated by `target` if any
})
```

`desktop/frontend/package.json` scripts:
- `build` — existing, runs wails target.
- `build:wails` — explicit alias of `build`.
- `build:capacitor` — `VITE_TARGET=capacitor vite build`.

### Mobile shell repoint

`mobile/scripts/sync-web.mjs` changes:
- Build runs in `desktop/frontend/` with `VITE_TARGET=capacitor`.
- Source synced is `desktop/frontend/dist-capacitor/` instead of `web/dist/`.
- Existing exclude patterns stay.

`mobile/capacitor.config.json` `webDir` stays as `www` (the sync target).

`mobile/README.md` updates the onboarding wording to reflect that the bundled UI is now the desktop UX, not the lean browser UX.

### Browser PWA disposition

`web/` is untouched. The Capacitor branch in `web/src/shared/api/client.ts` and `web/src/shared/ws/client-conn.ts` and the `web/src/setup/` page become dead code (no Capacitor build of `web/` exists anymore). Cleanup is a follow-up spec.

## Data Flow

### Desktop startup

```
wails build → app launches → main.ts → initPlatform() → createWailsPlatform()
                                                              │
                                                              ▼
                                                wailsjs/go/main/App + runtime events
                                                              │
                                                              ▼
                                                App.vue + components consume via usePlatform()
```

No behavior change vs today.

### Mobile cold start (no config)

```
WKWebView loads capacitor://localhost/index.html
   │
   ▼ main.ts → initPlatform() → createCapacitorPlatform()
   │
   ▼ applyMobileEntryGuard('home')  ← reused from PR #66
       caps = platform.caps, relay = platform.relay
   │
   ▼ await relay.load() → null  →  location.replace('/setup.html')
   │
setup/App.vue mounts; user enters base URL + token + (insecure switch)
   │
   ▼ await fetch(base + '/api/me', { headers: Authorization: 'Bearer ' + token })
       200 → relay.save(cfg) → location.replace('/')
   │
   ▼ home page mounts; platform.sessions.listRemoteSessions()
       → fetch(base + '/api/sessions', { Bearer })
   │
   ▼ user taps a session → attach via WebSocket(wss://base/client?..., [token])
       receives output stream, sends input frames
```

### Notification path

```
SessionConnection receives META frame "exit_code=…"
   │
   ▼ component invokes platform.system.showNotification(title, body)
   │
desktop  → wailsjs App.ShowNotification → Go runtime → OS notification
mobile   → @capacitor/local-notifications → iOS/Android system notification
```

### Optional method guards

```ts
// PaneGrid.vue
const platform = usePlatform()
function onNewPane() {
  if (!platform.sessions.newSession) return  // mobile no-op (button disabled anyway)
  platform.sessions.newSession({ cols, rows, shell })
}
```

## Error Handling Matrix

| Failure | Desktop | Mobile |
|---|---|---|
| `relay.load()` returns null | SettingsRelay prompts user to fill in | Entry guard redirects to `/setup.html` |
| `relay.fetchMe()` 401 | Topbar error + `events.emit('relay:auth-error', {reason:'token_invalid'})`; SettingsRelay shows banner | Same event fires AND mobile-only side effect: `location.replace('/setup.html?reason=token_invalid')` |
| `relay.fetchMe()` network error | Topbar red indicator + inline error in SettingsRelay | Inline error in setup page + reconnect banner (from PR #66) |
| Optional method called on platform that doesn't implement it | TypeScript flags at compile time (`?` operator forces guard); runtime guard via `if (!platform.sessions.newSession)` | Same — TypeScript-enforced |
| Capacitor plugin missing at runtime (e.g., `@capacitor/local-notifications` not installed) | N/A | `createCapacitorPlatform` constructor catches import failure and downgrades `caps.notifications` to `false` |
| Wails IPC called on mobile (impossible if `platform.ts` is the sole import site) | N/A | `throw new Error('wails binding called on capacitor platform')` from a debug stub `wailsjs-stub.ts` that the capacitor Vite config aliases over `wailsjs/*` |
| `initPlatform()` not called before `usePlatform()` | Both: explicit `throw` with actionable error message | Same |
| Browser PWA accidentally imports `platform/` | Won't compile (different vite project) — and `web/` should not be touched by this work | N/A |

## Testing Strategy

| Layer | Coverage | Mechanism |
|---|---|---|
| `platform/types.ts` | Type-only; nothing to test at runtime | TypeScript compilation |
| `platform/wails.ts` | Each bridge method delegates to correct `wailsjs/*` export; argument shape passed through | Vitest, mock `wailsjs/*` modules |
| `platform/capacitor.ts` | Preferences/Notifications/Clipboard/Browser calls; relay fetch URL+headers+credentials; mobile `events` bus FIFO | Vitest, mock `@capacitor/*` modules |
| `platform/index.ts` `initPlatform` | `VITE_TARGET` selects right impl; singleton semantics; `__setPlatformForTests` resets cleanly | Vitest |
| Capabilities-driven component rendering | Table-driven: for each `v-if="caps.X"`, mount with `caps.X = true/false` and assert presence | `@vue/test-utils` + injected mock platform |
| Existing desktop component tests | No regression: import-path migration must keep tests green | Existing Vitest suite |
| Mobile setup page | Port of PR #66 test cases into `desktop/frontend/tests/unit/setup/` | Vitest + `@vue/test-utils` |
| `mobile-guard` equivalent | Decision table for `desktop/frontend/src/platform/mobile-guard.ts` (port of PR #66's logic) | Vitest, table-driven |
| End-to-end | iOS sim smoke (6-item checklist, same as PR #66); Android emulator smoke; `wails dev` desktop smoke | Manual, documented in `mobile/README.md` |

CI additions:
- `desktop/frontend && npm run build:capacitor` — verify capacitor build doesn't break.
- `desktop/frontend && VITE_TARGET=capacitor npx vue-tsc --noEmit` — type-check the capacitor target (the existing `npm run build` only type-checks for the wails target).
- `mobile && npm run sync-web` — verify the build → sync pipeline produces non-empty `mobile/www/`.

## Operational / Documentation Changes

- `desktop/frontend/README.md` (create if missing) — document `VITE_TARGET` switch and the two build commands.
- `mobile/README.md` — replace the "bundles `web/`" wording with "bundles `desktop/frontend/`"; document that mobile uses the full desktop UX, with PaneGrid / multi-pane present but possibly cramped on phones (acceptable trade for MVP).
- `AGENTS.md` "何时改哪里" table — add row: "改移动 / 桌面共享 UI" → `desktop/frontend/src/platform/` and the per-bridge files; "新增 Wails binding" extended to "and add it to `platform/wails.ts` + maybe `platform/capacitor.ts`".
- `docs/spec/` — no protocol docs touched.

## Risks & Open Questions

- **PaneGrid on small screens.** Spec keeps it as-is for PR-C; if it's unusable on iPhone we'll add a `caps.singlePaneOnly: true` mobile-side flag in a follow-up. Not blocking the architecture.
- **Plugin host on mobile.** `pluginHost` is undefined on mobile; SettingsPlugins tab is hidden via `caps.pluginHost`. If a plugin author later wants to reach mobile, that's a separate spec.
- **Auto-update on mobile.** Apple/Google handle this; `caps.autoUpdate=false` hides the Settings → Updates tab. Done.
- **Hotkey capture on mobile.** Mobile has no system-wide hotkeys; `HotkeyCaptureCell.vue` is only rendered inside SettingsShortcuts. Decision: hide alongside the plugin-host UI (`caps.pluginHost === false` already gates the parent tab). Do not introduce a separate `caps.hotkeys` flag.
- **`mitt` vs hand-rolled `EventBus` for capacitor.** `mitt` is 200 bytes minified, no runtime cost, well-tested. Spec defaults to `mitt` but allows the implementer to hand-roll if they want to avoid the dependency.
- **`@capacitor/preferences` plugin install.** Adds a native dependency; PR-B updates `mobile/ios/App/Podfile` and (eventually) `mobile/android/app/build.gradle`. iOS Pod install can be flaky on CI — call this out in PR-B.
- **Wails events on capacitor.** The `EventBus` on mobile is in-process only; events emitted from Go on desktop will not fire on mobile. The complete inventory of `EventsOn` callsites today:
  - `before-close` (App.vue) — desktop quit confirmation dialog; mobile no-op (no Quit button on iOS).
  - `relay:auth-error` (App.vue, SettingsRelay.vue) — relay token rejected; on mobile this is supplemented by the `apiFetch` 401 → `/setup.html?reason=token_invalid` redirect from PR #66, so the in-process EventBus on mobile can also emit this event when `RelayBridge.fetchMe` fails 401.
  - `relay:auth-info` (SettingsRelay.vue) — relay auth confirmed; mobile equivalent: emit from successful `fetchMe` in the Capacitor bridge.
  - `plugin-config-changed` (plugins/configStore.ts) — plugin config edited externally; mobile no-op (`caps.pluginHost === false` hides the consumer).
  - `plugin-fs:dir-changed` (plugins/fileExplorer) — file watcher; mobile no-op (same reason).
  PR-A wires the bus and ports `before-close`/`relay:*`/`plugin-*` callers to `platform.events.on(...)`; the capacitor bridge emits `relay:*` from its own RelayBridge methods.
- **Dead code in `web/`.** `web/src/setup/`, the Capacitor branch in `web/src/shared/api/client.ts`, the WS subprotocol code in `web/src/shared/ws/client-conn.ts`, and the relay-config TS module become unused after this work. Cleanup follow-up spec.

## References

- AGENTS.md red-lines 4, 5, 9, 10.
- `docs/superpowers/specs/2026-05-22-mobile-relay-base-url-design.md` — PR #66, source of the entry-guard / setup-page pattern reused here.
- `desktop/app.go:37 methods` — full Wails binding surface to wrap.
- `desktop/frontend/wailsjs/runtime/runtime.d.ts` — runtime events surface.
- Wails v2 docs — no mobile target.
- Capacitor 8.x docs — plugin reference for Preferences / LocalNotifications / Browser / Clipboard.
