# PR-A: platform/ Adapter Layer + Wails Implementation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce `desktop/frontend/src/platform/` with a typed `Platform` interface and a `createWailsPlatform()` implementation, then migrate all 22 direct `wailsjs/*` import sites to consume `usePlatform()`. Desktop behavior stays byte-for-byte the same. Mobile is not yet buildable (PR-B brings the Capacitor implementation).

**Architecture:** Single chokepoint refactor. `platform/wails.ts` becomes the only file in the codebase that imports from `wailsjs/*` and `desktop/frontend/src/lib/api.ts`. Other modules call `usePlatform().relay.fetchMe()` / `.events.on(...)` / `.system.windowMinimize?.()` etc. Existing `lib/api.ts` continues to exist as a wails-specific shim (its 15 consumers are NOT migrated in this PR — separate cleanup spec); `platform/wails.ts` consumes it internally.

**Tech Stack:** Vue 3 + TypeScript + Vitest + happy-dom. No new runtime dependencies.

**Spec:** `docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md`

---

## File Structure

**New files:**
- `desktop/frontend/src/platform/types.ts` — `Platform`, `Capabilities`, `RelayBridge`, `SessionBridge`, `SystemBridge`, `EventBus`, `UpdaterBridge`, `PluginHostBridge`, plus shared data types re-exported from `lib/api.ts` and `wailsjs/go/models`.
- `desktop/frontend/src/platform/index.ts` — `initPlatform()`, `usePlatform()`, `__setPlatformForTests()` singleton accessors.
- `desktop/frontend/src/platform/wails.ts` — `createWailsPlatform(): Platform` concrete implementation. The only file in `src/` allowed to import from `../wailsjs/*` or `../lib/api`.
- `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` — shared `createFakePlatform()` test helper (used by every migration test below).
- `desktop/frontend/src/platform/__tests__/index.test.ts` — singleton lifecycle + test helper.
- `desktop/frontend/src/platform/__tests__/wails.test.ts` — each bridge method delegates correctly.

**Modified (migrations — production file + test file together, per task):**
1. `desktop/frontend/src/main.ts` — call `initPlatform()` before `createApp(...)`.
2. `desktop/frontend/src/composables/useWindowMaximized.ts` + `.test.ts` — `WindowIsMaximised` → `platform.system.windowIsMaximized?.()`.
3. `desktop/frontend/src/components/WindowControls.vue` + `.test.ts` — `WindowMinimise` / `WindowToggleMaximise` / `Quit` → `platform.system.windowMinimize?.()` / etc.
4. `desktop/frontend/src/components/TitleBar.vue` + `.test.ts` — `Environment` / `WindowToggleMaximise` → `platform.system.getEnvironment()` / etc.
5. `desktop/frontend/src/App.vue` — `EventsOn("before-close" / "relay:auth-error")` + `Environment()` → `platform.events.on(...)` + `platform.system.getEnvironment()`.
6. `desktop/frontend/src/components/SettingsRelay.vue` — `BrowserOpenURL` + `EventsOn("relay:auth-info")` → `platform.system.openExternalURL(...)` + `platform.events.on(...)`.
7. `desktop/frontend/src/plugins/configStore.ts` + `.test.ts` — `GetPluginConfig` / `SetPluginConfig` / `EventsOn("plugin-config-changed")` → `platform.pluginHost?.getPluginConfig()` / `.setPluginConfig()` / `platform.events.on(...)`.
8. `desktop/frontend/src/plugins/PluginHost.test.ts` — `GetPluginConfig` mock → mock platform.
9. `desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts` — `GetPluginConfig` mock → mock platform.
10. `desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts` — `GetPluginConfig` / `SetPluginConfig` mocks → mock platform.
11. `desktop/frontend/src/components/SettingsPlugins.test.ts` — same as above.
12. `desktop/frontend/src/plugins/fileExplorer/FileTree.vue` + `.test.ts` — `ListDir` / `WatchDir` / `UnwatchDir` / `EventsOn("plugin-fs:dir-changed")` → `platform.pluginHost?.fs.*` + `platform.events.on(...)`.
13. `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue` + `.test.ts` — `ReadFile` / `FileMeta` / `EventsOn("plugin-fs:dir-changed")` → `platform.pluginHost?.fs.*` + `platform.events.on(...)`.

**Documentation:**
- `desktop/frontend/README.md` (create if missing) — explain `platform/` adapter and `VITE_TARGET` future hook.
- `AGENTS.md` — extend "何时改哪里" table with a row for "改桌面前端 ↔ Go IPC" pointing at `platform/wails.ts` + the relevant `wailsjs/go/main/App.go` method.

---

## Pre-flight: Discovered spec extensions

The spec lists `RelayBridge`, `SessionBridge`, `SystemBridge`, `EventBus`, `UpdaterBridge`, `PluginHostBridge`. While inventorying the 22 import sites, two more methods surfaced that the spec didn't explicitly enumerate but that PR-A must wire:

- **`SystemBridge.windowMinimize?()` / `windowToggleMaximize?()` / `windowIsMaximized?()` / `quit?()`** — used by `WindowControls.vue`, `TitleBar.vue`, `useWindowMaximized.ts`. All four are optional (gated by `caps.windowControls`).
- **`SystemBridge.getEnvironment(): Promise<EnvironmentInfo | null>`** — used by `App.vue` and `TitleBar.vue` to detect platform/build type. Returns `null` on Capacitor; not optional because callers tolerate `null`.

These are folded into `SystemBridge` rather than a separate `WindowControlsBridge` to keep the interface count manageable. Update the spec's `Components & Interfaces` section as part of Task 18 (docs).

---

### Task 1: Platform types

**Files:**
- Create: `desktop/frontend/src/platform/types.ts`

This task is type-only; no runtime tests. TypeScript compile is the validation.

- [ ] **Step 1: Create the types file**

```ts
// Shared data types re-exported so consumers don't need to know if they came
// from lib/api.ts (the existing Wails shim) or wailsjs/go/models (Wails-generated).
export type {
  Endpoint,
  NewSessionReq,
  NewSessionResp,
  RelayConfig,
  RelayMe,
  HostInfo,
  LoggingConfig,
  LogPreview,
  ClipboardPastePayload,
  UpdateState,
} from '../lib/api'

// PluginConfig + sub-types live in wailsjs/go/models, re-export here.
export type { main as PluginModels } from '../../wailsjs/go/models'

export interface DirEntry {
  name: string
  isDir: boolean
  size?: number
  modTime?: number
}

export interface FileMetaInfo {
  path: string
  size: number
  modTime: number
  isDir: boolean
  exists: boolean
}

export interface FileContent {
  path: string
  data: number[]
  isBinary: boolean
  truncatedAt?: number
}

export interface EnvironmentInfo {
  buildType: string
  platform: string
  arch: string
}

// ----- Capabilities -----
export interface Capabilities {
  localPty: boolean
  autoUpdate: boolean
  pluginHost: boolean
  windowControls: boolean
  systemClipboard: boolean
  notifications: boolean
  fileDialog: boolean
}

// ----- Bridges -----
import type { RelayConfig as _RelayConfig, RelayMe as _RelayMe, NewSessionReq as _Req, NewSessionResp as _Resp, ClipboardPastePayload as _Clip, UpdateState as _UpdateState } from '../lib/api'
import type { main as _Models } from '../../wailsjs/go/models'

export interface RelayBridge {
  load(): Promise<_RelayConfig | null>
  save(cfg: _RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<_RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
}

export interface RemoteSession {
  session_id: string
  host_id: string
  host: string
  user: string
  title: string
  cols: number
  rows: number
}

export interface SessionBridge {
  newSession?(req: _Req): Promise<_Resp>
  closeSession(sessionID: string): Promise<void>
  listShells(): Promise<string[]>
  listRemoteSessions(): Promise<RemoteSession[]>
}

export interface SystemBridge {
  showNotification(title: string, body: string): Promise<void>
  getClipboardPaste(): Promise<_Clip>
  pickLogFilePath?(): Promise<string>
  openExternalURL(url: string): Promise<void>
  getEnvironment(): Promise<EnvironmentInfo | null>
  // Window control surface — optional, gated by caps.windowControls.
  windowMinimize?(): Promise<void>
  windowToggleMaximize?(): Promise<void>
  windowIsMaximized?(): Promise<boolean>
  quit?(): Promise<void>
}

export interface EventBus {
  on(event: string, handler: (data: unknown) => void): () => void
  emit(event: string, data: unknown): void
}

export interface UpdaterBridge {
  getState(): Promise<_UpdateState>
  checkUpdate(): Promise<void>
  startDownload(): Promise<void>
  installUpdate(): Promise<void>
}

export interface PluginHostBridge {
  getPluginConfig(): Promise<_Models.PluginConfig>
  setPluginConfig(cfg: _Models.PluginConfig): Promise<void>
  fs: {
    listDir(path: string): Promise<DirEntry[]>
    watchDir(path: string): Promise<void>
    unwatchDir(path: string): Promise<void>
    readFile(path: string): Promise<FileContent>
    fileMeta(path: string): Promise<FileMetaInfo>
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

- [ ] **Step 2: Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors (the types are unused by any consumer yet, but they must compile).

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/types.ts
git commit -m "feat(desktop): add Platform interface types for cross-target frontend"
```

---

### Task 2: Platform singleton + test helper

**Files:**
- Create: `desktop/frontend/src/platform/index.ts`
- Create: `desktop/frontend/src/platform/__tests__/index.test.ts`

- [ ] **Step 1: Write failing test**

```ts
// desktop/frontend/src/platform/__tests__/index.test.ts
import { describe, it, expect, beforeEach } from 'vitest'
import type { Platform } from '../types'
import { initPlatform, usePlatform, __setPlatformForTests } from '../index'

function fakePlatform(): Platform {
  return {
    caps: {
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
    },
    relay: {
      load: async () => null,
      save: async () => {},
      clear: async () => {},
      fetchMe: async () => ({ user_id: 'u', email: 'e' }),
    },
    sessions: {
      closeSession: async () => {},
      listShells: async () => [],
      listRemoteSessions: async () => [],
    },
    system: {
      showNotification: async () => {},
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async () => {},
      getEnvironment: async () => null,
    },
    events: {
      on: () => () => {},
      emit: () => {},
    },
  }
}

describe('platform singleton', () => {
  beforeEach(() => {
    __setPlatformForTests(null)
  })

  it('usePlatform() throws before initPlatform()', () => {
    expect(() => usePlatform()).toThrow(/initPlatform/i)
  })

  it('initPlatform() returns a Platform and usePlatform() returns the same instance', () => {
    // Stub VITE_TARGET handling by pre-installing via the test helper instead
    // of actually invoking createWailsPlatform here (kept separate).
    const fake = fakePlatform()
    __setPlatformForTests(fake)
    expect(usePlatform()).toBe(fake)
  })

  it('initPlatform() is idempotent', () => {
    const fake = fakePlatform()
    __setPlatformForTests(fake)
    // Idempotency property check via subsequent usePlatform calls.
    expect(usePlatform()).toBe(usePlatform())
  })

  it('__setPlatformForTests(null) clears the singleton', () => {
    __setPlatformForTests(fakePlatform())
    expect(usePlatform()).toBeTruthy()
    __setPlatformForTests(null)
    expect(() => usePlatform()).toThrow()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/index.test.ts
```
Expected: FAIL — module `../index` does not exist.

- [ ] **Step 3: Implement the module**

```ts
// desktop/frontend/src/platform/index.ts
import type { Platform } from './types'

export type { Platform } from './types'
export * from './types'

let _platform: Platform | null = null

export function initPlatform(): Platform {
  if (_platform) return _platform
  // VITE_TARGET selects implementation. The default 'wails' covers desktop;
  // 'capacitor' will be wired in PR-B.
  const target = (import.meta as { env?: { VITE_TARGET?: string } }).env?.VITE_TARGET ?? 'wails'
  if (target === 'capacitor') {
    throw new Error('platform: VITE_TARGET=capacitor not yet implemented (PR-B)')
  }
  // Lazy import so this module stays runnable in tests that use __setPlatformForTests
  // without triggering the wails impl (which imports from wailsjs/* and lib/api.ts).
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { createWailsPlatform } = require('./wails') as typeof import('./wails')
  _platform = createWailsPlatform()
  return _platform
}

export function usePlatform(): Platform {
  if (!_platform) {
    throw new Error('platform: call initPlatform() in main.ts before usePlatform()')
  }
  return _platform
}

// Test-only escape hatch.
export function __setPlatformForTests(p: Platform | null): void {
  _platform = p
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/index.test.ts
```
Expected: PASS — 4 tests green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/index.ts desktop/frontend/src/platform/__tests__/index.test.ts
git commit -m "feat(desktop): add platform singleton + test helper"
```

---

### Task 2.5: Shared fake-platform test helper

**Files:**
- Create: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`

Used by every migration test below. Centralizes the "minimal valid Platform" so each migration task overrides only what it needs.

- [ ] **Step 1: Create the helper**

```ts
// desktop/frontend/src/platform/__tests__/_fakePlatform.ts
import { vi } from 'vitest'
import type { Platform } from '../types'

// Returns a fresh fake Platform with all bridges present and every method
// wired to vi.fn() returning a sensible default. Tests should override the
// specific methods they care about, e.g.:
//   const p = createFakePlatform()
//   p.system.windowMinimize = vi.fn().mockResolvedValue(undefined)
//   __setPlatformForTests(p)
export function createFakePlatform(): Platform {
  return {
    caps: {
      localPty: true,
      autoUpdate: true,
      pluginHost: true,
      windowControls: true,
      systemClipboard: true,
      notifications: true,
      fileDialog: true,
    },
    relay: {
      load: vi.fn().mockResolvedValue(null),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      fetchMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
      setUplinkPaused: vi.fn().mockResolvedValue(undefined),
    },
    sessions: {
      newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
      closeSession: vi.fn().mockResolvedValue(undefined),
      listShells: vi.fn().mockResolvedValue([]),
      listRemoteSessions: vi.fn().mockResolvedValue([]),
    },
    system: {
      showNotification: vi.fn().mockResolvedValue(undefined),
      getClipboardPaste: vi.fn().mockResolvedValue({ kind: 'none' }),
      pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
      openExternalURL: vi.fn().mockResolvedValue(undefined),
      getEnvironment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
      windowMinimize: vi.fn().mockResolvedValue(undefined),
      windowToggleMaximize: vi.fn().mockResolvedValue(undefined),
      windowIsMaximized: vi.fn().mockResolvedValue(false),
      quit: vi.fn().mockResolvedValue(undefined),
    },
    events: {
      on: vi.fn(() => () => {}),
      emit: vi.fn(),
    },
    updater: {
      getState: vi.fn().mockResolvedValue({ state: 'idle' }),
      checkUpdate: vi.fn().mockResolvedValue(undefined),
      startDownload: vi.fn().mockResolvedValue(undefined),
      installUpdate: vi.fn().mockResolvedValue(undefined),
    },
    pluginHost: {
      getPluginConfig: vi.fn().mockResolvedValue({ enabled_plugins: [] } as unknown as Platform['pluginHost'] extends infer P ? (P extends { getPluginConfig: () => Promise<infer R> } ? R : never) : never),
      setPluginConfig: vi.fn().mockResolvedValue(undefined),
      fs: {
        listDir: vi.fn().mockResolvedValue([]),
        watchDir: vi.fn().mockResolvedValue(undefined),
        unwatchDir: vi.fn().mockResolvedValue(undefined),
        readFile: vi.fn().mockResolvedValue({ path: '/x', data: [], isBinary: false }),
        fileMeta: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isDir: false, exists: true }),
      },
    },
  }
}
```

- [ ] **Step 2: Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/__tests__/_fakePlatform.ts
git commit -m "test(desktop): shared createFakePlatform helper for migration tests"
```

---

### Task 3: Wails platform implementation

**Files:**
- Create: `desktop/frontend/src/platform/wails.ts`
- Create: `desktop/frontend/src/platform/__tests__/wails.test.ts`

- [ ] **Step 1: Write failing test**

```ts
// desktop/frontend/src/platform/__tests__/wails.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock lib/api before the impl import so createWailsPlatform sees the mocks.
vi.mock('../../lib/api', () => ({
  getRelayConfig: vi.fn().mockResolvedValue({ url: 'https://r', token: 'atk_x', allow_insecure_relay: false, remote_permission: 'full', connected: false }),
  setRelayConfig: vi.fn().mockResolvedValue(undefined),
  setUplinkPaused: vi.fn().mockResolvedValue(undefined),
  fetchRelayMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
  getClipboardPastePayload: vi.fn().mockResolvedValue({ kind: 'none' }),
  showNotification: vi.fn().mockResolvedValue(undefined),
  pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
  newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
  closeSession: vi.fn().mockResolvedValue(undefined),
  listShells: vi.fn().mockResolvedValue(['/bin/zsh']),
  getUpdateState: vi.fn().mockResolvedValue({ state: 'idle' }),
  checkUpdate: vi.fn().mockResolvedValue(undefined),
  startDownload: vi.fn().mockResolvedValue(undefined),
  installUpdate: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
  EventsEmit: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(true),
  Quit: vi.fn(),
  Environment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
  BrowserOpenURL: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetPluginConfig: vi.fn().mockResolvedValue({ enabled_plugins: [] }),
  SetPluginConfig: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/go/main/PluginFS', () => ({
  ListDir: vi.fn().mockResolvedValue([]),
  WatchDir: vi.fn().mockResolvedValue(undefined),
  UnwatchDir: vi.fn().mockResolvedValue(undefined),
  ReadFile: vi.fn().mockResolvedValue({ path: '/x', data: [], isBinary: false }),
  FileMeta: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isDir: false, exists: true }),
}))

import { createWailsPlatform } from '../wails'
import { WindowMinimise, Environment, BrowserOpenURL, EventsOn, EventsEmit } from '../../../wailsjs/runtime/runtime'
import { GetPluginConfig, SetPluginConfig } from '../../../wailsjs/go/main/App'
import { ListDir, ReadFile } from '../../../wailsjs/go/main/PluginFS'
import { fetchRelayMe, showNotification, setUplinkPaused } from '../../lib/api'

describe('createWailsPlatform', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('caps has all desktop flags true', () => {
    const p = createWailsPlatform()
    expect(p.caps).toEqual({
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
    })
  })

  it('relay.fetchMe delegates to lib/api fetchRelayMe', async () => {
    const p = createWailsPlatform()
    const me = await p.relay.fetchMe()
    expect(fetchRelayMe).toHaveBeenCalledOnce()
    expect(me).toEqual({ user_id: 'u', email: 'e' })
  })

  it('relay.setUplinkPaused delegates', async () => {
    const p = createWailsPlatform()
    await p.relay.setUplinkPaused!(true)
    expect(setUplinkPaused).toHaveBeenCalledWith(true)
  })

  it('system.showNotification delegates', async () => {
    const p = createWailsPlatform()
    await p.system.showNotification('t', 'b')
    expect(showNotification).toHaveBeenCalledWith('t', 'b')
  })

  it('system.windowMinimize delegates to runtime WindowMinimise', async () => {
    const p = createWailsPlatform()
    await p.system.windowMinimize!()
    expect(WindowMinimise).toHaveBeenCalledOnce()
  })

  it('system.getEnvironment returns the EnvironmentInfo from runtime', async () => {
    const p = createWailsPlatform()
    const env = await p.system.getEnvironment()
    expect(Environment).toHaveBeenCalledOnce()
    expect(env).toEqual({ platform: 'darwin', arch: 'arm64', buildType: 'production' })
  })

  it('system.openExternalURL delegates to BrowserOpenURL', async () => {
    const p = createWailsPlatform()
    await p.system.openExternalURL('https://example.com')
    expect(BrowserOpenURL).toHaveBeenCalledWith('https://example.com')
  })

  it('events.on subscribes via EventsOn and returns the unsubscribe', () => {
    const off = vi.fn()
    ;(EventsOn as ReturnType<typeof vi.fn>).mockReturnValueOnce(off)
    const p = createWailsPlatform()
    const handler = vi.fn()
    const u = p.events.on('relay:auth-error', handler)
    expect(EventsOn).toHaveBeenCalledWith('relay:auth-error', handler)
    expect(u).toBe(off)
  })

  it('events.emit delegates to EventsEmit', () => {
    const p = createWailsPlatform()
    p.events.emit('foo', { x: 1 })
    expect(EventsEmit).toHaveBeenCalledWith('foo', { x: 1 })
  })

  it('pluginHost.getPluginConfig delegates to App.GetPluginConfig', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.getPluginConfig()
    expect(GetPluginConfig).toHaveBeenCalledOnce()
  })

  it('pluginHost.fs.listDir delegates to PluginFS.ListDir', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.listDir('/tmp')
    expect(ListDir).toHaveBeenCalledWith('/tmp')
  })

  it('pluginHost.fs.readFile delegates to PluginFS.ReadFile', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.readFile('/tmp/x')
    expect(ReadFile).toHaveBeenCalledWith('/tmp/x')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/wails.test.ts
```
Expected: FAIL — `../wails` module not found.

- [ ] **Step 3: Implement the module**

```ts
// desktop/frontend/src/platform/wails.ts
import * as api from '../lib/api'
import {
  EventsOn,
  EventsEmit,
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  Quit,
  Environment,
  BrowserOpenURL,
} from '../../wailsjs/runtime/runtime'
import { GetPluginConfig, SetPluginConfig } from '../../wailsjs/go/main/App'
import {
  ListDir,
  WatchDir,
  UnwatchDir,
  ReadFile,
  FileMeta,
} from '../../wailsjs/go/main/PluginFS'
import type { Platform, EnvironmentInfo, RemoteSession } from './types'

export function createWailsPlatform(): Platform {
  return {
    caps: {
      localPty: true,
      autoUpdate: true,
      pluginHost: true,
      windowControls: true,
      systemClipboard: true,
      notifications: true,
      fileDialog: true,
    },
    relay: {
      load: () => api.getRelayConfig().then((c) => c ?? null),
      save: async (cfg) => {
        await api.setRelayConfig({
          url: cfg.url,
          token: cfg.token,
          allow_insecure_relay: cfg.allow_insecure_relay,
          remote_permission: cfg.remote_permission,
        })
      },
      clear: async () => {
        await api.setRelayConfig({
          url: '',
          token: '',
          allow_insecure_relay: false,
          remote_permission: 'full',
        })
      },
      fetchMe: () => api.fetchRelayMe(),
      setUplinkPaused: (paused: boolean) => api.setUplinkPaused(paused),
    },
    sessions: {
      newSession: api.newSession,
      closeSession: api.closeSession,
      listShells: api.listShells,
      // Remote session listing currently goes through relay HTTP API in
      // existing components (RemoteSessionsDialog uses lib/api or direct
      // fetch). For now expose an empty implementation; consumers continue
      // to use their existing path until a follow-up PR consolidates.
      listRemoteSessions: async (): Promise<RemoteSession[]> => [],
    },
    system: {
      showNotification: api.showNotification,
      getClipboardPaste: api.getClipboardPastePayload,
      pickLogFilePath: api.pickLogFilePath,
      openExternalURL: async (url: string) => {
        BrowserOpenURL(url)
      },
      getEnvironment: async (): Promise<EnvironmentInfo | null> => {
        try {
          const info = await Environment()
          return {
            platform: info.platform,
            arch: info.arch,
            buildType: info.buildType,
          }
        } catch {
          return null
        }
      },
      windowMinimize: async () => WindowMinimise(),
      windowToggleMaximize: async () => WindowToggleMaximise(),
      windowIsMaximized: () => WindowIsMaximised(),
      quit: async () => Quit(),
    },
    events: {
      on: (event, handler) => EventsOn(event, handler as (...data: unknown[]) => void),
      emit: (event, data) => EventsEmit(event, data),
    },
    updater: {
      getState: api.getUpdateState,
      checkUpdate: api.checkUpdate,
      startDownload: api.startDownload,
      installUpdate: api.installUpdate,
    },
    pluginHost: {
      getPluginConfig: GetPluginConfig as () => Promise<import('../../wailsjs/go/models').main.PluginConfig>,
      setPluginConfig: SetPluginConfig as (cfg: import('../../wailsjs/go/models').main.PluginConfig) => Promise<void>,
      fs: {
        listDir: ListDir as (path: string) => Promise<import('./types').DirEntry[]>,
        watchDir: WatchDir,
        unwatchDir: UnwatchDir,
        readFile: ReadFile as (path: string) => Promise<import('./types').FileContent>,
        fileMeta: FileMeta as (path: string) => Promise<import('./types').FileMetaInfo>,
      },
    },
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/wails.test.ts
```
Expected: PASS — 12 tests green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/wails.ts desktop/frontend/src/platform/__tests__/wails.test.ts
git commit -m "feat(desktop): add createWailsPlatform delegating to lib/api + wailsjs runtime"
```

---

### Task 4: Wire initPlatform into main.ts

**Files:**
- Modify: `desktop/frontend/src/main.ts`

- [ ] **Step 1: Replace `desktop/frontend/src/main.ts` with this exact content**

(Current file is 8 lines: `createApp` + `createPinia` + mount. Add platform init while preserving Pinia.)

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initPlatform } from './platform'
import './style.css'

const platform = initPlatform()

const app = createApp(App)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
```

- [ ] **Step 2: Type-check the project**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 3: Run full vitest suite**

```bash
cd desktop/frontend && npm test
```
Expected: PASS — existing 30+ test files still green (only `main.ts` changed; no consumer file calls `usePlatform()` yet).

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/main.ts
git commit -m "feat(desktop): initialize Platform singleton in main.ts"
```

---

### Task 5: Migrate composables/useWindowMaximized

**Files:**
- Modify: `desktop/frontend/src/composables/useWindowMaximized.ts`
- Modify: `desktop/frontend/src/composables/useWindowMaximized.test.ts`

- [ ] **Step 1: Update useWindowMaximized.ts**

Replace contents with:

```ts
import { ref, type Ref } from 'vue'
import { usePlatform } from '../platform'

const isMaximized = ref(false)

let initStarted = false
function initOnce() {
  if (initStarted) return
  initStarted = true
  Promise.resolve()
    .then(() => {
      const platform = usePlatform()
      const fn = platform.system.windowIsMaximized
      return fn ? fn() : Promise.resolve(false)
    })
    .then((v) => {
      isMaximized.value = !!v
    })
    .catch(() => {
      isMaximized.value = false
    })
}

export function useWindowMaximized(): Ref<boolean> {
  initOnce()
  return isMaximized
}

export function setMaximized(v: boolean): void {
  isMaximized.value = v
}
```

- [ ] **Step 2: Update useWindowMaximized.test.ts**

Read the existing test first (`cat desktop/frontend/src/composables/useWindowMaximized.test.ts`), then replace it with the version below. This drops the `wailsjs/*` mock in favor of `__setPlatformForTests(createFakePlatform())`.

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'

beforeEach(() => {
  vi.resetModules()
  __setPlatformForTests(createFakePlatform())
})

afterEach(() => {
  __setPlatformForTests(null)
})

describe('useWindowMaximized', () => {
  it('initializes to false and asynchronously updates from platform', async () => {
    const platform = createFakePlatform()
    platform.system.windowIsMaximized = vi.fn().mockResolvedValue(false)
    __setPlatformForTests(platform)
    const mod = await import('./useWindowMaximized')
    const ref = mod.useWindowMaximized()
    expect(ref.value).toBe(false)
    await Promise.resolve()
    await Promise.resolve()
    expect(platform.system.windowIsMaximized).toHaveBeenCalledOnce()
  })

  it('setMaximized flips the ref synchronously', async () => {
    const mod = await import('./useWindowMaximized')
    const ref = mod.useWindowMaximized()
    mod.setMaximized(true)
    expect(ref.value).toBe(true)
  })
})
```

- [ ] **Step 3: Run the test**

```bash
cd desktop/frontend && npx vitest run src/composables/useWindowMaximized.test.ts
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/composables/useWindowMaximized.ts desktop/frontend/src/composables/useWindowMaximized.test.ts
git commit -m "refactor(desktop): useWindowMaximized routes through platform"
```

---

### Task 6: Migrate WindowControls.vue

**Files:**
- Modify: `desktop/frontend/src/components/WindowControls.vue`
- Modify: `desktop/frontend/src/components/WindowControls.test.ts`

- [ ] **Step 1: Read current WindowControls.vue**

```bash
cat desktop/frontend/src/components/WindowControls.vue
```

- [ ] **Step 2: Update the `<script setup>` block**

Replace the imports and call sites. The minimal change:

```ts
import { computed } from 'vue'
import { usePlatform } from '../platform'
import { useWindowMaximized, setMaximized } from '../composables/useWindowMaximized'

const platform = usePlatform()
const isMaximized = useWindowMaximized()
const maxLabel = computed(() => (isMaximized.value ? 'Restore' : 'Maximize'))

function safe(fn: () => void) {
  try {
    fn()
  } catch (e) {
    console.warn('[WindowControls] runtime call failed', e)
  }
}

function onMin() {
  safe(() => { void platform.system.windowMinimize?.() })
}

function onMax() {
  safe(() => {
    void platform.system.windowToggleMaximize?.()
    setMaximized(!isMaximized.value)
  })
}

function onClose() {
  safe(() => { void platform.system.quit?.() })
}
```

(Preserve the existing `<template>` and `<style>` blocks unchanged.)

- [ ] **Step 3: Update WindowControls.test.ts**

Read existing test first, preserve any additional cases not shown below, then replace the mock layer with `createFakePlatform()` per-test overrides:

```ts
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'
import { setMaximized } from '../composables/useWindowMaximized'
import WindowControls from './WindowControls.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  setMaximized(false)
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})

describe('WindowControls', () => {
  it('renders three buttons: minimise, maximise/restore, close', () => {
    const w = mount(WindowControls)
    expect(w.find('[data-testid="window-min"]').exists()).toBe(true)
    expect(w.find('[data-testid="window-max"]').exists()).toBe(true)
    expect(w.find('[data-testid="window-close"]').exists()).toBe(true)
  })

  it('min button calls platform.system.windowMinimize', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-min"]').trigger('click')
    expect(platform.system.windowMinimize).toHaveBeenCalledTimes(1)
  })

  it('max button calls platform.system.windowToggleMaximize and flips the shared ref', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-max"]').trigger('click')
    expect(platform.system.windowToggleMaximize).toHaveBeenCalledTimes(1)
  })

  it('close button calls platform.system.quit', async () => {
    const w = mount(WindowControls)
    await w.get('[data-testid="window-close"]').trigger('click')
    expect(platform.system.quit).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 4: Run the test**

```bash
cd desktop/frontend && npx vitest run src/components/WindowControls.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/WindowControls.vue desktop/frontend/src/components/WindowControls.test.ts
git commit -m "refactor(desktop): WindowControls routes through platform"
```

---

### Task 7: Migrate TitleBar.vue

**Files:**
- Modify: `desktop/frontend/src/components/TitleBar.vue`
- Modify: `desktop/frontend/src/components/TitleBar.test.ts`

- [ ] **Step 1: Read the current files**

```bash
cat desktop/frontend/src/components/TitleBar.vue desktop/frontend/src/components/TitleBar.test.ts
```

- [ ] **Step 2: Update TitleBar.vue script**

Replace the imports from `../../wailsjs/runtime/runtime` with platform-routed equivalents:

```ts
// Before:
// import { Environment, WindowToggleMaximise } from "../../wailsjs/runtime/runtime";

// After:
import { usePlatform } from '../platform'

const platform = usePlatform()

// Replace `await Environment()` callsite with `await platform.system.getEnvironment()`
// The default-to-linux logic should stay: when getEnvironment() returns null
// (mobile) or throws, fall back to 'linux' just like before.

// Replace `WindowToggleMaximise()` with `void platform.system.windowToggleMaximize?.()`
```

Keep all template, style, and other script logic unchanged. The Environment fallback at the existing `.catch(...)` block continues to work since `getEnvironment()` may resolve to `null` — handle null the same way as a thrown error (default to `linux`).

- [ ] **Step 3: Update TitleBar.test.ts**

Read the existing test first to preserve all case names and assertions. Replace the mock layer by removing any `vi.mock('../../wailsjs/runtime/runtime', ...)` block and using:

```ts
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'
import TitleBar from './TitleBar.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})

describe('TitleBar', () => {
  it('queries platform for environment on mount', async () => {
    mount(TitleBar)
    await flushPromises()
    expect(platform.system.getEnvironment).toHaveBeenCalled()
  })

  it('double-click on titlebar toggles maximize', async () => {
    const w = mount(TitleBar)
    await flushPromises()
    await w.find('[data-testid="titlebar"]').trigger('dblclick')
    expect(platform.system.windowToggleMaximize).toHaveBeenCalled()
  })

  // Merge in any other cases from the existing TitleBar.test.ts here,
  // adapted to call `platform.system.<method>` instead of importing wailsjs.
})
```

- [ ] **Step 4: Run the test**

```bash
cd desktop/frontend && npx vitest run src/components/TitleBar.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/TitleBar.vue desktop/frontend/src/components/TitleBar.test.ts
git commit -m "refactor(desktop): TitleBar routes through platform"
```

---

### Task 8: Migrate App.vue

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Modify: `desktop/frontend/src/App.test.ts`

App.vue has two wailsjs callsites: `EventsOn("before-close", ...)`, `EventsOn("relay:auth-error", ...)`, and one `await Environment()`.

- [ ] **Step 1: Read App.vue around lines 25 and 683-690**

```bash
sed -n '20,30p;680,700p' desktop/frontend/src/App.vue
```

- [ ] **Step 2: Update App.vue**

Replace:
```ts
import { EventsOn, Environment } from "../wailsjs/runtime/runtime";
```
with:
```ts
import { usePlatform } from './platform'
const platform = usePlatform()
```

Replace `EventsOn("before-close", handleBeforeClose)` with `platform.events.on('before-close', handleBeforeClose)`.

Replace `EventsOn("relay:auth-error", (data: { reason: string }) => { ... })` with `platform.events.on('relay:auth-error', (data) => { /* same body, cast data as needed */ })`.

Replace `const info = await Environment()` with `const info = await platform.system.getEnvironment()` and guard the `info` use with null-check (Environment used to always resolve; getEnvironment may return null on mobile in PR-B+, but on desktop it returns the same data).

- [ ] **Step 3: Update App.test.ts**

Read the test:

```bash
cat desktop/frontend/src/App.test.ts
```

Two `expect(source).toContain(...)` assertions need swapping. The current expectations are:
- `expect(source).toContain('EventsOn("relay:auth-error"'`)
- `expect(source).toContain('EventsOn("before-close"'`)

Change to (note single quotes inside `toContain` since the new source uses `platform.events.on('...')`):
- `expect(source).toContain("platform.events.on('relay:auth-error'")`
- `expect(source).toContain("platform.events.on('before-close'")`

If `App.test.ts` also uses `vi.mock('../wailsjs/runtime/runtime', ...)`, remove that block and replace with:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from './platform'
import { createFakePlatform } from './platform/__tests__/_fakePlatform'

beforeEach(() => {
  vi.clearAllMocks()
  __setPlatformForTests(createFakePlatform())
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

Preserve all existing test cases.

- [ ] **Step 4: Run the test**

```bash
cd desktop/frontend && npx vitest run src/App.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "refactor(desktop): App routes EventsOn + Environment through platform"
```

---

### Task 9: Migrate SettingsRelay.vue

**Files:**
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/SettingsRelay.test.ts`

SettingsRelay uses `BrowserOpenURL`, `EventsOn`. (Note: it also imports from `lib/api`, leave those alone — `lib/api.ts` is not touched in this PR.)

- [ ] **Step 1: Update the script imports**

Replace:
```ts
import { BrowserOpenURL, EventsOn } from "../../wailsjs/runtime/runtime";
```
with:
```ts
import { usePlatform } from '../platform'
const platform = usePlatform()
```

Replace `BrowserOpenURL(url)` with `void platform.system.openExternalURL(url)`.
Replace `EventsOn("relay:auth-info", async (data) => {...})` with `platform.events.on('relay:auth-info', async (data) => {...})`.

(Keep the `import { getRelayConfig, setRelayConfig, setUplinkPaused, fetchRelayMe } from "../lib/api"` line untouched.)

- [ ] **Step 2: Update SettingsRelay.test.ts**

Read the existing test. Keep the `vi.mock('../lib/api', ...)` block as-is. Remove any `vi.mock('../../wailsjs/runtime/runtime', ...)` block and replace with the per-test fake platform pattern:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

Update any assertion that checked `BrowserOpenURL` to check `platform.system.openExternalURL`; assertions that checked `EventsOn` become `platform.events.on`. Preserve all test cases.

- [ ] **Step 3: Run the test**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/components/SettingsRelay.vue desktop/frontend/src/components/SettingsRelay.test.ts
git commit -m "refactor(desktop): SettingsRelay routes BrowserOpenURL/EventsOn through platform"
```

---

### Task 10: Migrate plugins/configStore.ts

**Files:**
- Modify: `desktop/frontend/src/plugins/configStore.ts`
- Modify: `desktop/frontend/src/plugins/configStore.test.ts`

configStore uses `GetPluginConfig`, `SetPluginConfig` (App methods) and `EventsOn("plugin-config-changed")`.

- [ ] **Step 1: Update configStore.ts**

Replace:
```ts
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { GetPluginConfig, SetPluginConfig } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
```
with:
```ts
import { usePlatform, type PluginModels } from '../platform'
// `main` references become PluginModels.* — adjust the type alias usage accordingly.
```

Where the code calls `GetPluginConfig()` / `SetPluginConfig(cfg)`, route through `usePlatform().pluginHost?.getPluginConfig() ?? Promise.reject(new Error('no pluginHost'))` and similarly for set.

Where it calls `EventsOn("plugin-config-changed", ...)`, use `usePlatform().events.on('plugin-config-changed', ...)`.

(The `main` type imports become `PluginModels.PluginConfig` everywhere they appear.)

- [ ] **Step 2: Update configStore.test.ts**

Replace all `vi.mock` blocks targeting `wailsjs/*` with:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from '../platform'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

Then assertions that previously read `expect(GetPluginConfig).toHaveBeenCalled()` become `expect(platform.pluginHost!.getPluginConfig).toHaveBeenCalled()`. To override the resolved value for a specific test: `(platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValueOnce({...})`. Preserve all test cases.

- [ ] **Step 3: Run the test**

```bash
cd desktop/frontend && npx vitest run src/plugins/configStore.test.ts
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/plugins/configStore.ts desktop/frontend/src/plugins/configStore.test.ts
git commit -m "refactor(desktop): plugin configStore routes through platform.pluginHost"
```

---

### Task 11: Migrate test-only wailsjs imports in plugins/PluginHost, quickInput, SettingsPlugins

**Files:**
- Modify: `desktop/frontend/src/plugins/PluginHost.test.ts`
- Modify: `desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts`
- Modify: `desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts`
- Modify: `desktop/frontend/src/components/SettingsPlugins.test.ts`

Each of these is a test file that mocks `GetPluginConfig` / `SetPluginConfig` from wailsjs to influence the production code's behavior. Since the production code now consumes `platform.pluginHost.*`, the tests should switch to mocking the platform.

- [ ] **Step 1: For each of the 4 test files**

Adjust the relative path to `../../platform` / `../../../platform` based on file depth (PluginHost/SettingsPlugins → `../../platform`; quickInput/* → `../../../platform`).

Remove any `vi.mock('.../wailsjs/go/main/App', ...)` block. Remove any `import { GetPluginConfig, SetPluginConfig } from '.../wailsjs/go/main/App'`. Add the per-test fake platform pattern:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from '../../platform'   // or '../../../platform' for quickInput/*
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

For tests that previously checked `expect(GetPluginConfig).toHaveBeenCalledWith(arg)`, change to `expect(platform.pluginHost!.getPluginConfig).toHaveBeenCalledWith(arg)`. To override the default resolved value: `(platform.pluginHost!.getPluginConfig as ReturnType<typeof vi.fn>).mockResolvedValueOnce({/* test-specific value */})`.

Preserve all existing test cases and assertions; only swap the mock layer.

- [ ] **Step 2: Run all four tests**

```bash
cd desktop/frontend && npx vitest run \
  src/plugins/PluginHost.test.ts \
  src/plugins/quickInput/QuickInputBar.test.ts \
  src/plugins/quickInput/QuickInputSettings.test.ts \
  src/components/SettingsPlugins.test.ts
```
Expected: PASS — all four.

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/plugins/PluginHost.test.ts \
        desktop/frontend/src/plugins/quickInput/QuickInputBar.test.ts \
        desktop/frontend/src/plugins/quickInput/QuickInputSettings.test.ts \
        desktop/frontend/src/components/SettingsPlugins.test.ts
git commit -m "refactor(desktop): plugin-host tests mock via platform instead of wailsjs"
```

---

### Task 12: Migrate plugins/fileExplorer/FileTree.vue

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts`

FileTree uses `ListDir`, `WatchDir`, `UnwatchDir` (PluginFS) + `EventsOn("plugin-fs:dir-changed")`.

- [ ] **Step 1: Update FileTree.vue**

Replace:
```ts
import { ListDir, WatchDir, UnwatchDir } from "../../../wailsjs/go/main/PluginFS"
import { EventsOn } from "../../../wailsjs/runtime/runtime"
```
with:
```ts
import { usePlatform } from '../../platform'
const platform = usePlatform()
const fs = platform.pluginHost!.fs  // file explorer requires pluginHost; guarded upstream by caps.pluginHost
```

Replace `ListDir(path)` with `fs.listDir(path)`, etc.
Replace `EventsOn("plugin-fs:dir-changed", ...)` with `platform.events.on('plugin-fs:dir-changed', ...)`.

- [ ] **Step 2: Update FileTree.test.ts**

Remove `vi.mock('../../../wailsjs/go/main/PluginFS', ...)` and `vi.mock('../../../wailsjs/runtime/runtime', ...)` blocks; remove `import { ListDir } from '../../../wailsjs/go/main/PluginFS'`. Replace with:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

Assertions like `expect(ListDir).toHaveBeenCalledWith(...)` become `expect(platform.pluginHost!.fs.listDir).toHaveBeenCalledWith(...)`. Per-test override of resolved values:

```ts
(platform.pluginHost!.fs.listDir as ReturnType<typeof vi.fn>).mockResolvedValueOnce([
  { name: 'foo.txt', isDir: false, size: 10, modTime: 0 },
])
```

Preserve all existing test cases.

- [ ] **Step 3: Run the test**

```bash
cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileTree.test.ts
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/plugins/fileExplorer/FileTree.vue desktop/frontend/src/plugins/fileExplorer/FileTree.test.ts
git commit -m "refactor(desktop): FileTree routes PluginFS through platform.pluginHost.fs"
```

---

### Task 13: Migrate plugins/fileExplorer/FileEditor.vue

**Files:**
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileEditor.vue`
- Modify: `desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts`

FileEditor uses `ReadFile`, `FileMeta` (PluginFS) + `EventsOn("plugin-fs:dir-changed")`.

- [ ] **Step 1: Update FileEditor.vue**

Same pattern as Task 12:
```ts
import { usePlatform } from '../../platform'
const platform = usePlatform()
const fs = platform.pluginHost!.fs
```

Replace `ReadFile(path)` with `fs.readFile(path)`, `FileMeta(path)` with `fs.fileMeta(path)`.
Replace `EventsOn("plugin-fs:dir-changed", ...)` with `platform.events.on('plugin-fs:dir-changed', ...)`.

- [ ] **Step 2: Update FileEditor.test.ts**

Remove `vi.mock('../../../wailsjs/go/main/PluginFS', ...)` and `vi.mock('../../../wailsjs/runtime/runtime', ...)` blocks; remove `import { ReadFile, FileMeta } from '../../../wailsjs/go/main/PluginFS'`. Replace with:

```ts
import { beforeEach, afterEach, vi } from 'vitest'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  __setPlatformForTests(platform)
})

afterEach(() => {
  __setPlatformForTests(null)
})
```

Assertions `expect(ReadFile).toHaveBeenCalledWith(...)` → `expect(platform.pluginHost!.fs.readFile).toHaveBeenCalledWith(...)`. Same for `FileMeta` → `platform.pluginHost!.fs.fileMeta`. Per-test override:

```ts
(platform.pluginHost!.fs.readFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
  path: '/tmp/foo.txt', data: [/* bytes */], isBinary: false,
})
```

Preserve all existing test cases.

- [ ] **Step 3: Run the test**

```bash
cd desktop/frontend && npx vitest run src/plugins/fileExplorer/FileEditor.test.ts
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/plugins/fileExplorer/FileEditor.vue desktop/frontend/src/plugins/fileExplorer/FileEditor.test.ts
git commit -m "refactor(desktop): FileEditor routes PluginFS through platform.pluginHost.fs"
```

---

### Task 14: Final invariant — no `wailsjs/*` imports outside platform/

**Files:**
- No production code change.

- [ ] **Step 1: Verify the invariant**

```bash
cd /Users/attson/code/github.com.attson/atterm
grep -rn "from .*wailsjs" desktop/frontend/src/ | grep -v "src/platform/"
```
Expected: empty output. If any line shows, those are leftover migration targets — go back and fix them in a new task before proceeding.

- [ ] **Step 2: Run the full test suite**

```bash
cd desktop/frontend && npm test
```
Expected: all tests green (pre-existing test count, no regressions).

- [ ] **Step 3: Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 4: Wails dev smoke (manual)**

In a separate terminal:
```bash
cd desktop && wails dev -tags webkit2_41   # Linux; drop the tag on mac
```
Expected: app launches, terminal opens a new tab, settings dialog opens, Settings → Relay shows current config, Settings → Plugins lists configured plugins. Close the app cleanly via the close button.

Do NOT commit anything at this task — this is a verification gate.

---

### Task 15: Update docs

**Files:**
- Create or modify: `desktop/frontend/README.md`
- Modify: `AGENTS.md`
- Modify: `docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md`

- [ ] **Step 1: Create/update `desktop/frontend/README.md`**

If the file exists, append; otherwise create:

```markdown
# desktop/frontend

Vue 3 + TypeScript + Naive UI + xterm frontend for the Wails desktop app, and (via the `platform/` adapter) the mobile Capacitor shell.

## Platform adapter

All Go-bound calls and Wails runtime calls route through `src/platform/`. Components do:

```ts
import { usePlatform } from '@/platform'
const platform = usePlatform()
await platform.relay.fetchMe()
platform.events.on('before-close', handler)
```

`platform/wails.ts` is the only file allowed to import from `../wailsjs/*` or `../lib/api`. To call a new Go method:

1. Add the `App.go` method on the Go side; let Wails regenerate `wailsjs/go/main/App.{js,d.ts}`.
2. Wrap it in `src/platform/wails.ts` on the appropriate `Bridge` (e.g. `RelayBridge`, `SessionBridge`).
3. If it represents new functionality, add the method to `src/platform/types.ts` first.
4. The Capacitor implementation (`platform/capacitor.ts`, PR-B onwards) decides whether to implement, no-op, or omit (optional method).

## Build targets

- `npm run build` — builds for the Wails desktop target (default, current behaviour).
- `npm run build:capacitor` (added in PR-B) — builds for Capacitor mobile.

PR-A only delivers the adapter layer + Wails implementation; Capacitor build lands in PR-B.
```

- [ ] **Step 2: Append row to AGENTS.md "何时改哪里" table**

Open `AGENTS.md`, find the `## 何时改哪里` markdown table, append:

```markdown
| 改桌面前端 ↔ Go IPC | `desktop/frontend/src/platform/wails.ts`（适配器）；新方法先在 `desktop/app.go` 或 `desktop/plugin_*.go` 定义，让 Wails 重生成 `wailsjs/`，再在 `platform/wails.ts` 包一层。**不要**在 `src/platform/` 之外的文件直接 import `wailsjs/*`。 |
```

- [ ] **Step 3: Append a note to the spec**

Open `docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md` and add to the `SystemBridge` interface in the "Components & Interfaces" section the optional window methods (`windowMinimize?`, `windowToggleMaximize?`, `windowIsMaximized?`, `quit?`) and the non-optional `getEnvironment()`. Add a short paragraph noting these were discovered during PR-A implementation and rolled into `SystemBridge` rather than a separate `WindowControlsBridge`.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/README.md AGENTS.md docs/superpowers/specs/2026-05-23-desktop-frontend-mobile-shell-design.md
git commit -m "docs: platform adapter routing + AGENTS.md table row"
```

---

## Final verification

After Task 15:

- [ ] **Run full vitest suite**

```bash
cd desktop/frontend && npm test
```
Expected: all green.

- [ ] **Type-check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Confirm no remaining `wailsjs/*` imports outside platform/**

```bash
cd /Users/attson/code/github.com.attson/atterm
grep -rn "from .*wailsjs" desktop/frontend/src/ | grep -v "src/platform/"
```
Expected: empty.

- [ ] **Confirm wails desktop build still works**

```bash
cd desktop && wails build -tags webkit2_41  # or without tag on mac
```
Expected: build succeeds.

- [ ] **Open a PR**

```bash
git push -u origin platform-pr-a
gh pr create --base main --head platform-pr-a --title "feat(desktop): PR-A platform/ adapter layer + Wails implementation" --body "..."
```

(PR title and body per project convention; reference the spec.)
