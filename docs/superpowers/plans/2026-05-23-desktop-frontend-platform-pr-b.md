# PR-B: Capacitor Platform Impl + Multi-Target Build + Caps Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `platform/capacitor.ts` + a multi-target Vite build + a mobile entry that mounts a placeholder, so `desktop/frontend/` builds for Capacitor and boots in the iOS simulator; add capabilities gating to `SettingsDialog` and `TitleBar` as groundwork; repoint `mobile/scripts/sync-web.mjs` at `desktop/frontend/dist-capacitor/`. End-state: iOS Capacitor app boots to a "configure relay (PR-C)" placeholder; desktop unchanged.

**Architecture:** Two Vite build targets selected by `VITE_TARGET` produce `dist/` (wails, default) or `dist-capacitor/index.html` (capacitor). Each target has its own entry: `src/main.ts` mounts the full desktop `App.vue` with `createWailsPlatform`; `src/main.capacitor.ts` mounts `MobilePlaceholder.vue` with `createCapacitorPlatform`. Because `App.vue`'s `onMounted` calls Wails-only bindings (`getEndpoint`, `getHostInfo`, …), the capacitor entry deliberately does NOT mount `App.vue` in PR-B — that wiring is PR-C's job. Capabilities-driven `v-if`s added to `SettingsDialog` and `App.vue`'s `TitleBar` are unit-tested groundwork that PR-C activates when the real mobile UI mounts desktop-derived components.

**Tech Stack:** Vue 3 + TypeScript + Vite 5 + Vitest. No new runtime dependencies (Capacitor native plugins land in PR-D).

**Spec:** `docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md`
**Prereq:** PR-A merged to main (commit `146f001`).

---

## File Structure

**New files:**
- `desktop/frontend/src/platform/capacitor.ts` — `createCapacitorPlatform(): Platform`; all desktop-only caps false; relay via `localStorage` (Capacitor Preferences plugin lands in PR-D); events via in-process event bus; desktop-only optional methods/bridges omitted.
- `desktop/frontend/src/platform/__tests__/capacitor.test.ts` — bridge delegation tests.
- `desktop/frontend/src/MobilePlaceholder.vue` — minimal "mobile shell loaded, relay config in PR-C" page. The capacitor entry mounts this directly.
- `desktop/frontend/src/__tests__/MobilePlaceholder.test.ts` — render test.
- `desktop/frontend/src/main.capacitor.ts` — capacitor entry: `initPlatform(createCapacitorPlatform)` then mounts `MobilePlaceholder` (NOT `App.vue`).
- `desktop/frontend/index.capacitor.html` — capacitor HTML shell (mobile viewport meta, references `/src/main.capacitor.ts`).

**Modified files:**
- `desktop/frontend/vite.config.ts` — multi-target via `process.env.VITE_TARGET`: input HTML + `build.outDir`.
- `desktop/frontend/package.json` — add `build:wails` + `build:capacitor` scripts.
- `desktop/frontend/src/App.vue` — gate `<TitleBar v-if="caps.windowControls">` (groundwork; desktop default keeps it visible).
- `desktop/frontend/src/App.test.ts` — assert TitleBar shows by default, hides when `windowControls=false`.
- `desktop/frontend/src/components/SettingsDialog.vue` — `v-if` the Updates / Plugins / Shortcuts / Logging nav buttons + panes against caps; fall back to `general` if the initial tab is hidden.
- `desktop/frontend/src/components/SettingsDialog.test.ts` — table-driven caps render check (create if absent).
- `mobile/scripts/sync-web.mjs` — build `desktop/frontend/` with `VITE_TARGET=capacitor`; sync `desktop/frontend/dist-capacitor/` → `mobile/www/`.
- `desktop/frontend/README.md` — document the two build targets + PR-B boot state.
- `mobile/README.md` — document the `desktop/frontend` build path + PR-B placeholder boot.

**NOT touched:**
- `desktop/frontend/src/lib/api.ts` and its 16 consumers (separate cleanup spec).
- `desktop/frontend/src/platform/{types.ts, index.ts, wails.ts}` (PR-A interface consumed unchanged).
- `desktop/frontend/src/main.ts` (keeps wails wiring).
- `mobile/capacitor.config.json` (`webDir: "www"` already correct; `appId`/`scheme` unchanged).
- All other `desktop/frontend/src/components/*.vue` (UX redesign out of scope).

---

### Task 1: Capacitor platform implementation

**Files:**
- Create: `desktop/frontend/src/platform/capacitor.ts`
- Create: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/platform/__tests__/capacitor.test.ts
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createCapacitorPlatform } from '../capacitor'

describe('createCapacitorPlatform', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('caps disables all desktop-only flags', () => {
    const p = createCapacitorPlatform()
    expect(p.caps).toEqual({
      localPty: false,
      autoUpdate: false,
      pluginHost: false,
      windowControls: false,
      systemClipboard: true,
      notifications: true,
      fileDialog: false,
    })
  })

  it('omits updater and pluginHost bridges', () => {
    const p = createCapacitorPlatform()
    expect(p.updater).toBeUndefined()
    expect(p.pluginHost).toBeUndefined()
  })

  it('omits window control + log-file system methods', () => {
    const p = createCapacitorPlatform()
    expect(p.system.windowMinimize).toBeUndefined()
    expect(p.system.windowToggleMaximize).toBeUndefined()
    expect(p.system.windowIsMaximized).toBeUndefined()
    expect(p.system.quit).toBeUndefined()
    expect(p.system.pickLogFilePath).toBeUndefined()
  })

  it('omits sessions.newSession and relay.setUplinkPaused', () => {
    const p = createCapacitorPlatform()
    expect(p.sessions.newSession).toBeUndefined()
    expect(p.relay.setUplinkPaused).toBeUndefined()
  })

  it('relay.load returns null when nothing stored', async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.save persists to localStorage under atterm.relay and load reads it back', async () => {
    const p = createCapacitorPlatform()
    const cfg = {
      url: 'https://relay.example.com', token: 'atk_xyz',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await p.relay.save(cfg)
    expect(JSON.parse(localStorage.getItem('atterm.relay')!)).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await p.relay.load()).toMatchObject({ url: cfg.url, token: cfg.token })
  })

  it('relay.load returns null on malformed JSON', async () => {
    localStorage.setItem('atterm.relay', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.clear removes the localStorage entry', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r', token: 'atk_x', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    await p.relay.clear()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })

  it('relay.fetchMe GETs base/api/me with Bearer + credentials omit', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bear', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1', email: 'e@x' }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    vi.stubGlobal('fetch', fetchMock)
    const me = await p.relay.fetchMe()
    expect(me).toEqual({ user_id: 'u1', email: 'e@x' })
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/me')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_bear')
    expect((init as RequestInit).credentials).toBe('omit')
  })

  it('relay.fetchMe throws relay_not_configured when no config stored', async () => {
    const p = createCapacitorPlatform()
    await expect(p.relay.fetchMe()).rejects.toThrow(/relay_not_configured/i)
  })

  it('sessions.listShells + listRemoteSessions return empty arrays', async () => {
    const p = createCapacitorPlatform()
    expect(await p.sessions.listShells()).toEqual([])
    expect(await p.sessions.listRemoteSessions()).toEqual([])
  })

  it('sessions.closeSession is a no-op placeholder', async () => {
    const p = createCapacitorPlatform()
    await expect(p.sessions.closeSession('s1')).resolves.toBeUndefined()
  })

  it('system.openExternalURL calls window.open in a new tab', async () => {
    const p = createCapacitorPlatform()
    const open = vi.fn()
    vi.stubGlobal('open', open) // window.open is globalThis.open in happy-dom
    await p.system.openExternalURL('https://example.com')
    expect(open).toHaveBeenCalledWith('https://example.com', '_blank')
  })

  it('system.getEnvironment returns the Capacitor environment shape', async () => {
    const p = createCapacitorPlatform()
    expect(await p.system.getEnvironment()).toEqual({ buildType: 'capacitor', platform: 'ios', arch: 'arm64' })
  })

  it('system.getClipboardPaste returns kind=none placeholder', async () => {
    const p = createCapacitorPlatform()
    expect(await p.system.getClipboardPaste()).toEqual({ kind: 'none' })
  })

  it('system.showNotification resolves (native plugin lands PR-D)', async () => {
    const p = createCapacitorPlatform()
    await expect(p.system.showNotification('t', 'b')).resolves.toBeUndefined()
  })

  it('events.on/emit invoke handlers in order; off unsubscribes', () => {
    const p = createCapacitorPlatform()
    const calls: unknown[] = []
    const off1 = p.events.on('x', (d) => calls.push(['a', d]))
    p.events.on('x', (d) => calls.push(['b', d]))
    p.events.emit('x', { n: 1 })
    expect(calls).toEqual([['a', { n: 1 }], ['b', { n: 1 }]])
    off1()
    p.events.emit('x', { n: 2 })
    expect(calls).toEqual([['a', { n: 1 }], ['b', { n: 1 }], ['b', { n: 2 }]])
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts
```
Expected: FAIL — module `../capacitor` does not exist.

- [ ] **Step 3: Implement the module**

```ts
// desktop/frontend/src/platform/capacitor.ts
import type { Platform, RelayConfig, RelayMe } from './types'

const STORAGE_KEY = 'atterm.relay'

function loadCfg(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as RelayConfig
  } catch {
    return null
  }
}

function createEventBus() {
  const handlers = new Map<string, Set<(data: unknown) => void>>()
  return {
    on(event: string, handler: (data: unknown) => void): () => void {
      let set = handlers.get(event)
      if (!set) {
        set = new Set()
        handlers.set(event, set)
      }
      set.add(handler)
      return () => { set!.delete(handler) }
    },
    emit(event: string, data: unknown): void {
      const set = handlers.get(event)
      if (!set) return
      for (const h of [...set]) h(data)
    },
  }
}

export function createCapacitorPlatform(): Platform {
  return {
    caps: {
      localPty: false,
      autoUpdate: false,
      pluginHost: false,
      windowControls: false,
      systemClipboard: true,
      notifications: true,
      fileDialog: false,
    },
    relay: {
      load: async () => loadCfg(),
      save: async (cfg) => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
      },
      clear: async () => {
        if (typeof localStorage === 'undefined') return
        localStorage.removeItem(STORAGE_KEY)
      },
      fetchMe: async (): Promise<RelayMe> => {
        const cfg = loadCfg()
        if (!cfg || !cfg.url || !cfg.token) throw new Error('relay_not_configured')
        const base = cfg.url.replace(/\/$/, '')
        const res = await fetch(base + '/api/me', {
          method: 'GET',
          headers: { Authorization: `Bearer ${cfg.token}` },
          credentials: 'omit',
        })
        if (!res.ok) throw new Error(`relay fetchMe failed: HTTP ${res.status}`)
        return (await res.json()) as RelayMe
      },
      // setUplinkPaused omitted — desktop-only
    },
    sessions: {
      // newSession omitted — capacitor cannot fork local PTYs
      closeSession: async () => {
        // PR-C wires real remote close via relay HTTP API
      },
      listShells: async () => [],
      listRemoteSessions: async () => [],
    },
    system: {
      showNotification: async () => {
        // PR-D wires @capacitor/local-notifications
      },
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async (url: string) => {
        if (typeof window !== 'undefined' && typeof window.open === 'function') {
          window.open(url, '_blank')
        }
      },
      getEnvironment: async () => ({ buildType: 'capacitor', platform: 'ios', arch: 'arm64' }),
      // window* + pickLogFilePath omitted — desktop-only
    },
    events: createEventBus(),
    // updater + pluginHost omitted — desktop-only
  }
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts
```
Expected: PASS — all cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(desktop): add createCapacitorPlatform with localStorage relay + in-process event bus"
```

---

### Task 2: MobilePlaceholder component

**Files:**
- Create: `desktop/frontend/src/MobilePlaceholder.vue`
- Create: `desktop/frontend/src/__tests__/MobilePlaceholder.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/__tests__/MobilePlaceholder.test.ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobilePlaceholder from '../MobilePlaceholder.vue'

describe('MobilePlaceholder', () => {
  it('renders the AT Term app name', () => {
    expect(mount(MobilePlaceholder).text()).toMatch(/AT Term/i)
  })

  it('mentions that setup/relay config lands in PR-C', () => {
    expect(mount(MobilePlaceholder).text()).toMatch(/PR-C|setup|configure|relay/i)
  })

  it('exposes a data-testid for smoke targeting', () => {
    expect(mount(MobilePlaceholder).find('[data-testid="mobile-placeholder"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/__tests__/MobilePlaceholder.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the component**

```vue
<!-- desktop/frontend/src/MobilePlaceholder.vue -->
<template>
  <div class="mobile-placeholder" data-testid="mobile-placeholder">
    <h1>AT Term</h1>
    <p class="lead">Mobile shell loaded.</p>
    <p class="muted">
      Relay configuration UI ships in PR-C. For now this confirms the
      Capacitor bundle and the desktop frontend's <code>platform/</code>
      adapter wire up end-to-end inside iOS WebView.
    </p>
  </div>
</template>

<style scoped>
.mobile-placeholder {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 2rem 1.25rem;
  text-align: center;
  background: #05070d;
  color: #e6e7ea;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}
.mobile-placeholder h1 { margin: 0 0 1rem; font-size: 1.75rem; font-weight: 600; }
.mobile-placeholder .lead { margin: 0 0 0.75rem; font-size: 1rem; }
.mobile-placeholder .muted { margin: 0; max-width: 32rem; font-size: 0.875rem; color: #8d93a3; line-height: 1.5; }
.mobile-placeholder code { background: rgba(255,255,255,0.08); padding: 0.125rem 0.375rem; border-radius: 0.25rem; font-size: 0.8125rem; }
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/__tests__/MobilePlaceholder.test.ts
```
Expected: PASS — 3 cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/MobilePlaceholder.vue desktop/frontend/src/__tests__/MobilePlaceholder.test.ts
git commit -m "feat(desktop): MobilePlaceholder page for the capacitor boot until PR-C ships setup"
```

---

### Task 3: Multi-target Vite config + package scripts

**Files:**
- Modify: `desktop/frontend/vite.config.ts`
- Modify: `desktop/frontend/package.json`

Note: no runtime code reads `VITE_TARGET` — target selection happens at build time via separate entry files. So no `define` is needed; the env var only steers Vite's input/output.

- [ ] **Step 1: Replace `desktop/frontend/vite.config.ts`**

```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// VITE_TARGET picks the build:
//   - 'wails'     (default): index.html → dist/        (Wails embeds dist)
//   - 'capacitor':           index.capacitor.html → dist-capacitor/index.html
//                            (mobile/scripts/sync-web.mjs syncs into mobile/www/)
// The capacitor input is keyed `index` so Vite emits dist-capacitor/index.html,
// which is the entry filename Capacitor expects.
const target = process.env.VITE_TARGET ?? 'wails'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: target === 'capacitor' ? 'dist-capacitor' : 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: target === 'capacitor'
        ? { index: fileURLToPath(new URL('./index.capacitor.html', import.meta.url)) }
        : fileURLToPath(new URL('./index.html', import.meta.url)),
    },
  },
})
```

- [ ] **Step 2: Update `desktop/frontend/package.json` scripts**

Read the current `package.json`, then set the `scripts` block to:

```json
"scripts": {
  "dev": "vite",
  "build": "vue-tsc --noEmit && vite build",
  "build:wails": "vue-tsc --noEmit && VITE_TARGET=wails vite build",
  "build:capacitor": "vue-tsc --noEmit && VITE_TARGET=capacitor vite build",
  "preview": "vite preview",
  "test": "vitest run",
  "test:watch": "vitest"
}
```

(`build` stays the default wails build so `wails build`'s frontend hook keeps working.)

- [ ] **Step 3: Smoke the existing wails build (no regression)**

```bash
cd desktop/frontend && npm run build:wails 2>&1 | tail -8
test -f desktop/frontend/dist/index.html && echo "OK: wails dist/index.html"
```
Expected: succeeds; `dist/index.html` present.

(The `build:capacitor` script will fail until Task 4 creates `index.capacitor.html` + `main.capacitor.ts` — that's expected; it's smoked in Task 4.)

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/vite.config.ts desktop/frontend/package.json
git commit -m "build(desktop): multi-target Vite config (VITE_TARGET=wails|capacitor)"
```

---

### Task 4: Mobile entry point + HTML shell

**Files:**
- Create: `desktop/frontend/src/main.capacitor.ts`
- Create: `desktop/frontend/index.capacitor.html`

`main.capacitor.ts` mounts `MobilePlaceholder` directly, NOT `App.vue`. `App.vue`'s `onMounted` calls Wails-only bindings (`getEndpoint`, `getHostInfo`, `getCommandNotifyThresholdSeconds`) that would reject on capacitor; mounting the full desktop tree on mobile is PR-C's job once those paths are platform-routed.

- [ ] **Step 1: Create `desktop/frontend/src/main.capacitor.ts`**

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import MobilePlaceholder from './MobilePlaceholder.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const platform = initPlatform(createCapacitorPlatform)

const app = createApp(MobilePlaceholder)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
```

- [ ] **Step 2: Create `desktop/frontend/index.capacitor.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"/>
  <meta name="theme-color" content="#0b1020"/>
  <meta name="apple-mobile-web-app-capable" content="yes"/>
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent"/>
  <meta name="apple-mobile-web-app-title" content="AT Term"/>
  <title>AT Term</title>
</head>
<body>
<div id="app"></div>
<script src="./src/main.capacitor.ts" type="module"></script>
</body>
</html>
```

- [ ] **Step 3: Run the capacitor build**

```bash
cd desktop/frontend && npm run build:capacitor 2>&1 | tail -12
test -f desktop/frontend/dist-capacitor/index.html && echo "OK: dist-capacitor/index.html"
```
Expected: build succeeds; `dist-capacitor/index.html` exists (emitted from `index.capacitor.html` via the `index` input key).

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/main.capacitor.ts desktop/frontend/index.capacitor.html
git commit -m "feat(desktop): capacitor entry mounts MobilePlaceholder via createCapacitorPlatform"
```

---

### Task 5: App.vue — gate TitleBar behind caps.windowControls

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`

Groundwork only: on desktop `caps.windowControls === true` so TitleBar stays visible (byte-for-byte). The gate matters in PR-C when mobile mounts desktop-derived components.

- [ ] **Step 1: Read the TitleBar usage + the platform alias**

```bash
sed -n '1,40p;748,772p' desktop/frontend/src/App.vue
```

Confirm the platform is aliased (PR-A named it `$platform` to avoid colliding with the existing `platform` ref). If `$platform` exists, reuse it; otherwise add `const $platform = usePlatform()` near the top.

- [ ] **Step 2: Expose caps + gate TitleBar**

In `<script setup>`, after the platform alias, add:

```ts
const caps = $platform.caps
```

In `<template>`, add `v-if="caps.windowControls"` to the existing `<TitleBar ... />` element (preserve all its existing props/bindings).

- [ ] **Step 3: Update App.test.ts**

Read the test. It already wires `__setPlatformForTests(createFakePlatform())` in `beforeEach` (from PR-A). Append:

```ts
import { createFakePlatform } from './platform/__tests__/_fakePlatform'

it('renders TitleBar by default (caps.windowControls=true)', async () => {
  const w = mount(App)
  await flushPromises()
  expect(w.find('[data-testid="titlebar-root"]').exists()).toBe(true)
})

it('hides TitleBar when caps.windowControls is false', async () => {
  const platform = createFakePlatform()
  platform.caps = { ...platform.caps, windowControls: false }
  __setPlatformForTests(platform)
  const w = mount(App)
  await flushPromises()
  expect(w.find('[data-testid="titlebar-root"]').exists()).toBe(false)
})
```

(If `flushPromises` / `mount` / `__setPlatformForTests` aren't already imported in the file, add them.)

- [ ] **Step 4: Run the test**

```bash
cd desktop/frontend && npx vitest run src/App.test.ts
```
Expected: PASS — preserved tests + 2 new ones.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(desktop): gate TitleBar behind caps.windowControls"
```

---

### Task 6: SettingsDialog caps gating

**Files:**
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify/Create: `desktop/frontend/src/components/SettingsDialog.test.ts`

| Tab | Hide when |
|---|---|
| Updates | `!caps.autoUpdate` |
| Plugins | `!caps.pluginHost` |
| Shortcuts | `!caps.pluginHost` |
| Logging | `!caps.fileDialog` |

General + Relay always show. If `initialTab` is currently hidden, fall back to `general`.

- [ ] **Step 1: Update the `<script setup>` of SettingsDialog.vue**

Add (alongside existing imports):

```ts
import { usePlatform } from '../platform'
const caps = usePlatform().caps
```

Immediately after the existing `const activeTab = ref(...)` declaration (line 33), add:

```ts
const hiddenTabs = new Set<string>()
if (!caps.autoUpdate) hiddenTabs.add('updates')
if (!caps.pluginHost) { hiddenTabs.add('plugins'); hiddenTabs.add('shortcuts') }
if (!caps.fileDialog) hiddenTabs.add('logging')
if (hiddenTabs.has(activeTab.value)) activeTab.value = 'general'
```

- [ ] **Step 2: Add `v-if` to the four conditional nav buttons + their panes**

Nav buttons (in the `<aside class="settings-nav">` block): add `v-if="caps.fileDialog"` to the Logging button, `v-if="caps.autoUpdate"` to Updates, `v-if="caps.pluginHost"` to Plugins and to Shortcuts. Leave General + Relay buttons unchanged.

Panes (in the body): add the matching `v-if` to each component instance, keeping the existing `v-show`/props:

```vue
<SettingsLogging v-if="caps.fileDialog" v-show="activeTab === 'logging'" ... />
<SettingsUpdates v-if="caps.autoUpdate" v-show="activeTab === 'updates'" ... />
<SettingsPlugins v-if="caps.pluginHost" v-show="activeTab === 'plugins'" />
<SettingsShortcuts v-if="caps.pluginHost" v-show="activeTab === 'shortcuts'" />
```

- [ ] **Step 3: Create/append `desktop/frontend/src/components/SettingsDialog.test.ts`**

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'
import SettingsDialog from './SettingsDialog.vue'

const baseProps = { localSessionCount: 0, remoteSessionCount: 0, terminalThemeId: 'default' }
let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

function navLabels(w: ReturnType<typeof mount>) {
  return w.findAll('.settings-nav-item').map((b) => b.text())
}

describe('SettingsDialog caps gating', () => {
  it('renders all 6 tabs with full desktop caps', () => {
    const w = mount(SettingsDialog, { props: baseProps })
    expect(navLabels(w)).toEqual(['General', 'Relay', 'Logging', 'Updates', 'Plugins', 'Shortcuts'])
  })

  it('hides Updates when autoUpdate=false', () => {
    platform.caps = { ...platform.caps, autoUpdate: false }
    __setPlatformForTests(platform)
    expect(navLabels(mount(SettingsDialog, { props: baseProps }))).not.toContain('Updates')
  })

  it('hides Plugins + Shortcuts when pluginHost=false', () => {
    platform.caps = { ...platform.caps, pluginHost: false }
    __setPlatformForTests(platform)
    const labels = navLabels(mount(SettingsDialog, { props: baseProps }))
    expect(labels).not.toContain('Plugins')
    expect(labels).not.toContain('Shortcuts')
  })

  it('hides Logging when fileDialog=false', () => {
    platform.caps = { ...platform.caps, fileDialog: false }
    __setPlatformForTests(platform)
    expect(navLabels(mount(SettingsDialog, { props: baseProps }))).not.toContain('Logging')
  })

  it('with capacitor-style caps only General + Relay show', () => {
    platform.caps = { ...platform.caps, autoUpdate: false, pluginHost: false, fileDialog: false }
    __setPlatformForTests(platform)
    expect(navLabels(mount(SettingsDialog, { props: baseProps }))).toEqual(['General', 'Relay'])
  })

  it('falls back to general when initialTab is hidden under current caps', () => {
    platform.caps = { ...platform.caps, autoUpdate: false }
    __setPlatformForTests(platform)
    const w = mount(SettingsDialog, { props: { ...baseProps, initialTab: 'updates' } })
    expect(w.find('.settings-nav-item.active').text()).toBe('General')
  })
})
```

- [ ] **Step 4: Run the test**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsDialog.test.ts
```
Expected: PASS — 6 cases (plus any pre-existing tests in the file).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsDialog.vue desktop/frontend/src/components/SettingsDialog.test.ts
git commit -m "feat(desktop): gate Settings tabs (Updates/Plugins/Shortcuts/Logging) by caps"
```

---

### Task 7: Repoint mobile/scripts/sync-web.mjs at desktop/frontend/dist-capacitor

**Files:**
- Modify: `mobile/scripts/sync-web.mjs`
- Conditionally: `mobile/scripts/sync-web.test.mjs`

- [ ] **Step 1: Read the current runNpmBuild + main**

```bash
sed -n '52,88p' mobile/scripts/sync-web.mjs
```

- [ ] **Step 2: Replace `runNpmBuild()` and `main()`**

```js
function runNpmBuild(cwd, script) {
  return new Promise((resolveSpawn, reject) => {
    const child = spawn("npm", ["run", script], { cwd, stdio: "inherit" });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) resolveSpawn();
      else reject(new Error(`npm run ${script} exited with code ${code} in ${cwd}`));
    });
  });
}

async function main() {
  const scriptDir = dirname(fileURLToPath(import.meta.url));
  const mobileDir = dirname(scriptDir);
  const repoRoot = dirname(mobileDir);
  const frontendDir = resolve(repoRoot, "desktop", "frontend");
  const distDir = resolve(frontendDir, "dist-capacitor");

  console.log(`building desktop/frontend (capacitor target) in ${frontendDir}`);
  await runNpmBuild(frontendDir, "build:capacitor");

  const distStat = await stat(distDir).catch(() => null);
  if (!distStat || !distStat.isDirectory()) {
    throw new Error(`expected capacitor build output at ${distDir}`);
  }

  const result = await syncWebAssets(distDir, resolve(mobileDir, "www"));
  console.log(`synced ${result.copied.length} capacitor assets to ${result.dest}`);
}
```

- [ ] **Step 3: Check + update the test if present**

```bash
ls mobile/scripts/sync-web.test.mjs 2>/dev/null && cat mobile/scripts/sync-web.test.mjs || echo "no test file"
```

If the test only exercises the pure `syncWebAssets(src, dest)` helper (unchanged signature), no edit needed — confirm by reading it. If it asserts the old `web/` build path, update those expectations to `desktop/frontend/dist-capacitor`. Then:

```bash
cd mobile && npm test 2>&1 | tail -8
```

- [ ] **Step 4: Smoke the full pipeline**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npm run sync-web 2>&1 | tail -10
test -f mobile/www/index.html && echo "OK: www/index.html"
test -d mobile/www/assets && echo "OK: www/assets/"
```
Expected: builds capacitor target, copies into `mobile/www/`, both checks pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add mobile/scripts/sync-web.mjs
git commit -m "build(mobile): sync from desktop/frontend/dist-capacitor instead of web/dist"
```

---

### Task 8: Update desktop/frontend/README.md

**Files:**
- Modify: `desktop/frontend/README.md`

- [ ] **Step 1: Replace the PR-A "Build targets" placeholder paragraph**

Read the current README. PR-A added a "Build targets" section saying `build:capacitor` lands in PR-B. Replace that section with:

```markdown
## Build targets

The same source builds for two targets, selected by `VITE_TARGET`:

- `npm run build:wails` (or the default `npm run build`) — `index.html` → `dist/`; consumed by `wails build`.
- `npm run build:capacitor` — `VITE_TARGET=capacitor`, builds `index.capacitor.html` → `dist-capacitor/index.html`. `mobile/scripts/sync-web.mjs` syncs this into `mobile/www/`.

`src/main.ts` (Wails) mounts `App.vue` with `createWailsPlatform`. `src/main.capacitor.ts` (Capacitor) mounts `MobilePlaceholder.vue` with `createCapacitorPlatform`. The capacitor entry deliberately does NOT mount `App.vue` yet — `App.vue`'s `onMounted` calls Wails-only bindings; mounting the real mobile UI is PR-C. Vite tree-shakes the unused platform impl per target, so the capacitor bundle never pulls in `wailsjs/*`/`lib/api.ts`.

## Mobile boot state (PR-B)

The iOS Capacitor app boots into `MobilePlaceholder.vue`, confirming the bundle + `platform/` adapter load inside iOS WebView. Relay config + remote session UI land in PR-C.
```

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/README.md
git commit -m "docs(desktop): document build:capacitor + PR-B mobile boot state"
```

---

### Task 9: Update mobile/README.md

**Files:**
- Modify: `mobile/README.md`

- [ ] **Step 1: Replace the "Develop" + "Relay configuration" sections; delete the old smoke checklist**

Read the current `mobile/README.md`, then:

Replace the `## Develop` body with:

```markdown
## Develop

```bash
cd mobile
npm install
npm run ios:add     # first time only; creates mobile/ios
npm run ios:open    # syncs desktop/frontend (capacitor target) and opens Xcode
```

`npm run sync-web` builds `desktop/frontend/` with `VITE_TARGET=capacitor` and copies `desktop/frontend/dist-capacitor/` into `mobile/www/`. The bundled UI is the desktop frontend's capacitor entry; relay-config and remote-session UI ship in PR-C.

After `ios:add`, keep the generated `mobile/ios` project in git, but do not commit `node_modules`, `www`, `Pods`, or copied Capacitor public assets.
```

Replace the `## Relay configuration` body with:

```markdown
## Relay configuration

PR-B boots the mobile app to a placeholder page (`MobilePlaceholder.vue`) confirming the Capacitor bundle and the desktop frontend's `platform/` adapter load inside iOS WebView. **Actual relay configuration UI ships in PR-C.**

The relay must allow the WebView origin. Start it with:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```
```

Delete the `## Mobile smoke checklist` section entirely (it described PR #66's web-based flow; PR-C re-adds the desktop-frontend smoke checklist).

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add mobile/README.md
git commit -m "docs(mobile): README documents desktop/frontend build path + PR-B placeholder boot"
```

---

### Task 10: Final verification gate

**Files:** none (verification only — no commit).

- [ ] **Step 1: Full vitest suite**

```bash
cd desktop/frontend && npm test 2>&1 | tail -10
```
Expected: all green; count = previous total + new tests from Tasks 1, 2, 5, 6.

- [ ] **Step 2: Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 3: Wails build (no regression)**

```bash
cd desktop/frontend && npm run build:wails 2>&1 | tail -5
test -f desktop/frontend/dist/index.html && echo "OK"
```

- [ ] **Step 4: Capacitor build + entry shape**

```bash
cd desktop/frontend && npm run build:capacitor 2>&1 | tail -5
test -f desktop/frontend/dist-capacitor/index.html && echo "OK: index.html"
grep -q "main.capacitor" desktop/frontend/dist-capacitor/index.html && echo "OK: references main.capacitor entry"
```

- [ ] **Step 5: Mobile sync pipeline**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npm run sync-web 2>&1 | tail -5
test -f mobile/www/index.html && echo "OK: www/index.html"
test -d mobile/www/assets && echo "OK: www/assets/"
```

- [ ] **Step 6: Smoke in iOS simulator (manual — owner runs, requires Xcode)**

```bash
cd mobile && npm run ios:open
# Xcode: Product → Clean Build Folder, then Run (iPhone simulator)
```
Expected: simulator boots to the `MobilePlaceholder` page ("AT Term", "Mobile shell loaded", PR-C mention); no JS errors in Safari → Develop → Simulator → AT Term console.

No commit — verification gate. Any failure points back to the producing task.

---

## Self-Review Notes

- **Spec coverage:** capacitor.ts (Task 1); MobilePlaceholder + capacitor entry boot (Tasks 2, 4); Vite multi-target (Task 3); TitleBar + Settings caps gating (Tasks 5, 6); mobile sync repoint (Task 7); docs (Tasks 8, 9). Placeholder boot = the spec's "configure relay state".
- **No-runtime-VITE_TARGET:** target selection is build-time only (separate entry files), so no `import.meta.env.VITE_TARGET` runtime branch and no `define` — avoids the Vite `import.meta.env` override gotcha.
- **App.vue onMounted hazard handled:** capacitor mounts `MobilePlaceholder`, never `App.vue`, so the Wails-only `onMounted` bindings never fire on mobile. The TitleBar gate is groundwork tested with fake caps, not exercised by the PR-B boot.
- **Desktop preserved:** all caps gates are `true` on the desktop fake/runtime; `build:wails` smoke (Task 10 Step 3) catches regressions.
- **Out-of-scope guards:** `lib/api.ts`, `platform/{types,index,wails}.ts`, `main.ts`, `capacitor.config.json` untouched.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-23-desktop-frontend-platform-pr-b.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review, fast iteration.
2. **Inline Execution** — execute in this session with checkpoints.

Which approach?
