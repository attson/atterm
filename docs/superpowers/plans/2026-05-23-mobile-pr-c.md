# PR-C: Mobile Attach-Only Client (Setup + Session List + Keepalive Terminal) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `MobilePlaceholder` with a real mobile attach-only client in `desktop/frontend/src/mobile/`: setup (relay config) → host-grouped session list → lean keepalive terminal, all in one `MobileApp.vue` root with internal view state.

**Architecture:** Single `MobileApp.vue` mounted by `main.capacitor.ts` owns `view` state (`setup|list|terminal`) + a keepalive registry of open terminals (soft LRU cap 4). `MobileTerminal.vue` reuses the wails-free `lib/connection.ts` `SessionConnection`. Opened terminals stay mounted (`v-show`) so their WS + xterm persist; switching is instant; back doesn't detach; a 401 anywhere resets to setup. Desktop and `web/` are untouched; PR-A/PR-B interfaces are consumed unchanged.

**Tech Stack:** Vue 3 + TypeScript + xterm + xterm-addon-fit + Vitest + @vue/test-utils (all already in `desktop/frontend`).

**Spec:** `docs/superpowers/specs/2026-05-23-mobile-pr-c-design.md`
**Branch:** `desktop-platform-pr-c` (stacked on PR-B `desktop-platform-pr-b`).

---

## Grounded facts (verified against current code)

- `SessionConnection` ctor: `new SessionConnection(endpoint: Endpoint, sessionId: string, handlers: ConnectionHandlers = {})`. Methods: `attach()`, `detach()`, `sendInput(s: string)`, `sendResize(cols, rows)`. Imports clean (no wails).
- `Endpoint = { url: string; token: string }`. `webSocketAuth` does `url = endpoint.url.replace(/\/$/,'') + '/client'` — so **`endpoint.url` is the WS base WITHOUT `/client`** (e.g. `wss://relay.example.com`). The session id travels in the ATTACH frame, not the URL.
- `ConnectionHandlers`: `onOutput?(data: Uint8Array)`, `onClose?(info)`, `onMeta?(meta)`, `onStatus?(s: Status)`, `onReplayProgress?(p)`. `Status = "connecting"|"attached"|"reconnecting"|"ended"|"error"`.
- `RemoteSession` (PR-A `platform/types.ts`): `{ session_id, host_id, host, user, title, cols, rows }`.
- `GET /api/sessions` returns `SessionInfo[]`: `{ id, command, cwd, title, cols, rows, started_at, host_id, host, user, remote_permission? }`. Map: `session_id = info.id`, `title = info.title || info.command`.
- Web's `validateRelayBase(base, allowInsecure)` + `isLoopbackHost` are the reference to port (http/https only; non-loopback http needs the insecure flag; reject path/query/fragment).

---

## File Structure

**New (`desktop/frontend/src/mobile/`):**
- `relay.ts` — `validateRelayBase(base, allowInsecure)`, `isLoopbackHost(host)`, `relayBaseToWsUrl(httpBase)` (https→wss, http→ws).
- `sessionGroups.ts` — `groupByHost(sessions: RemoteSession[]): { host: string; user: string; sessions: RemoteSession[] }[]` (pure, testable).
- `MobileSetup.vue`
- `MobileSessionList.vue`
- `MobileTerminal.vue`
- `MobileTerminalHost.vue`
- `MobileApp.vue`
- `__tests__/relay.test.ts`, `__tests__/sessionGroups.test.ts`, `__tests__/MobileSetup.test.ts`, `__tests__/MobileSessionList.test.ts`, `__tests__/MobileTerminal.test.ts`, `__tests__/MobileTerminalHost.test.ts`, `__tests__/MobileApp.test.ts`

**Modified:**
- `desktop/frontend/src/platform/capacitor.ts` — implement `listRemoteSessions` (GET /api/sessions, Bearer, map, 401→`relay_unauthorized`).
- `desktop/frontend/src/platform/__tests__/capacitor.test.ts` — add listRemoteSessions cases.
- `desktop/frontend/src/main.capacitor.ts` — mount `MobileApp` instead of `MobilePlaceholder`.
- `mobile/README.md` — PR-C smoke checklist.

**Deleted:**
- `desktop/frontend/src/MobilePlaceholder.vue`
- `desktop/frontend/src/__tests__/MobilePlaceholder.test.ts`

**NOT touched:** `lib/api.ts`, `lib/connection.ts`, `platform/{types,index,wails}.ts`, desktop components, `web/`.

---

### Task 1: mobile/relay.ts — validation + ws url helpers

**Files:**
- Create: `desktop/frontend/src/mobile/relay.ts`
- Create: `desktop/frontend/src/mobile/__tests__/relay.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/relay.test.ts
import { describe, it, expect } from 'vitest'
import { validateRelayBase, relayBaseToWsUrl } from '../relay'

describe('validateRelayBase', () => {
  it('accepts https with host', () => { expect(validateRelayBase('https://r.example.com', false)).toBeNull() })
  it('rejects empty', () => { expect(validateRelayBase('', false)).toMatch(/required/i) })
  it('rejects malformed', () => { expect(validateRelayBase('not a url', false)).toMatch(/invalid|malformed/i) })
  it('rejects non-http(s) scheme', () => { expect(validateRelayBase('wss://r.example.com', false)).toMatch(/http/i) })
  it('rejects path/query/fragment', () => {
    expect(validateRelayBase('https://r.example.com/x', false)).toMatch(/path|query|fragment/i)
    expect(validateRelayBase('https://r.example.com/?a=1', false)).toMatch(/path|query|fragment/i)
  })
  it('accepts http loopback without insecure flag', () => {
    expect(validateRelayBase('http://localhost:8080', false)).toBeNull()
    expect(validateRelayBase('http://127.0.0.1:8080', false)).toBeNull()
  })
  it('rejects http non-loopback without insecure flag', () => {
    expect(validateRelayBase('http://r.example.com', false)).toMatch(/insecure/i)
  })
  it('accepts http non-loopback with insecure flag', () => {
    expect(validateRelayBase('http://r.example.com', true)).toBeNull()
  })
})

describe('relayBaseToWsUrl', () => {
  it('https → wss (no trailing slash, no path)', () => {
    expect(relayBaseToWsUrl('https://r.example.com')).toBe('wss://r.example.com')
    expect(relayBaseToWsUrl('https://r.example.com/')).toBe('wss://r.example.com')
  })
  it('http → ws, preserves port', () => {
    expect(relayBaseToWsUrl('http://1.2.3.4:8080')).toBe('ws://1.2.3.4:8080')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/relay.test.ts
```
Expected: FAIL — module `../relay` missing.

- [ ] **Step 3: Implement**

```ts
// desktop/frontend/src/mobile/relay.ts
export function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase()
  if (h === 'localhost' || h === '127.0.0.1' || h === '::1') return true
  if (h.startsWith('127.')) return true
  if (h.startsWith('[') && h.endsWith(']') && h.slice(1, -1) === '::1') return true
  return false
}

export function validateRelayBase(base: string, allowInsecure: boolean): string | null {
  const trimmed = base.trim()
  if (!trimmed) return 'relay URL is required'
  let u: URL
  try {
    u = new URL(trimmed)
  } catch {
    return 'invalid or malformed relay URL'
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') {
    return 'relay URL must start with http:// or https://'
  }
  if (u.pathname !== '/' || u.search !== '' || u.hash !== '') {
    return 'relay URL must not contain a path, query, or fragment'
  }
  if (u.protocol === 'http:' && !isLoopbackHost(u.hostname) && !allowInsecure) {
    return 'enable "Allow insecure HTTP/WS" to use http:// with a non-loopback host'
  }
  return null
}

// Convert an http(s) relay base to the ws(s) base SessionConnection expects
// (no trailing slash, no path — SessionConnection appends /client itself).
export function relayBaseToWsUrl(httpBase: string): string {
  const u = new URL(httpBase)
  const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${u.host}`
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/relay.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/relay.ts desktop/frontend/src/mobile/__tests__/relay.test.ts
git commit -m "feat(mobile): relay URL validation + ws-base helper"
```

---

### Task 2: capacitor.listRemoteSessions wiring

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`

- [ ] **Step 1: Append failing tests**

Append to `desktop/frontend/src/platform/__tests__/capacitor.test.ts` (inside the existing `describe('createCapacitorPlatform', …)`):

```ts
  it('listRemoteSessions GETs base/api/sessions with Bearer and maps SessionInfo→RemoteSession', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_t', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify([
      { id: 's1', command: 'bash', cwd: '/', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h1', host: 'box', user: 'me' },
      { id: 's2', command: 'zsh', cwd: '/', title: 'claude', cols: 100, rows: 30, started_at: 0, host_id: 'h1', host: 'box', user: 'me' },
    ]), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const sessions = await p.sessions.listRemoteSessions()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/sessions')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_t')
    expect((init as RequestInit).credentials).toBe('omit')
    expect(sessions).toEqual([
      { session_id: 's1', host_id: 'h1', host: 'box', user: 'me', title: 'bash', cols: 80, rows: 24 },
      { session_id: 's2', host_id: 'h1', host: 'box', user: 'me', title: 'claude', cols: 100, rows: 30 },
    ])
  })

  it('listRemoteSessions returns [] when no config', async () => {
    const p = createCapacitorPlatform()
    expect(await p.sessions.listRemoteSessions()).toEqual([])
  })

  it('listRemoteSessions throws relay_unauthorized on 401', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bad', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    await expect(p.sessions.listRemoteSessions()).rejects.toThrow(/relay_unauthorized/)
  })
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts
```
Expected: FAIL — listRemoteSessions still returns `[]` (stub from PR-B), so the mapping/Bearer assertions fail.

- [ ] **Step 3: Implement**

In `desktop/frontend/src/platform/capacitor.ts`, replace the PR-B stub `listRemoteSessions: async () => []` with:

```ts
      listRemoteSessions: async (): Promise<RemoteSession[]> => {
        const cfg = loadCfg()
        if (!cfg || !cfg.url || !cfg.token) return []
        const base = cfg.url.replace(/\/$/, '')
        const res = await fetch(base + '/api/sessions', {
          headers: { Authorization: `Bearer ${cfg.token}` },
          credentials: 'omit',
        })
        if (res.status === 401) throw new Error('relay_unauthorized')
        if (!res.ok) throw new Error(`list sessions: HTTP ${res.status}`)
        const raw = (await res.json()) as Array<{
          id: string; command: string; title: string; cols: number; rows: number;
          host_id: string; host: string; user: string
        }>
        return raw.map((s) => ({
          session_id: s.id,
          host_id: s.host_id,
          host: s.host,
          user: s.user,
          title: s.title || s.command,
          cols: s.cols,
          rows: s.rows,
        }))
      },
```

Add `RemoteSession` to the existing `import type { Platform, RelayConfig, RelayMe } from './types'` line → `import type { Platform, RelayConfig, RelayMe, RemoteSession } from './types'`.

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts
```
Expected: PASS — all prior + 3 new.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(mobile): capacitor listRemoteSessions via GET /api/sessions (Bearer)"
```

---

### Task 3: sessionGroups.ts — group by host

**Files:**
- Create: `desktop/frontend/src/mobile/sessionGroups.ts`
- Create: `desktop/frontend/src/mobile/__tests__/sessionGroups.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/sessionGroups.test.ts
import { describe, it, expect } from 'vitest'
import { groupByHost } from '../sessionGroups'
import type { RemoteSession } from '../../platform/types'

const mk = (over: Partial<RemoteSession>): RemoteSession => ({
  session_id: 's', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24, ...over,
})

describe('groupByHost', () => {
  it('groups sessions by host preserving first-seen host order', () => {
    const out = groupByHost([
      mk({ session_id: 'a', host: 'box1', user: 'me' }),
      mk({ session_id: 'b', host: 'box2', user: 'me' }),
      mk({ session_id: 'c', host: 'box1', user: 'me' }),
    ])
    expect(out.map((g) => g.host)).toEqual(['box1', 'box2'])
    expect(out[0]!.sessions.map((s) => s.session_id)).toEqual(['a', 'c'])
    expect(out[1]!.sessions.map((s) => s.session_id)).toEqual(['b'])
  })
  it('carries the user of the first session in each host group', () => {
    const out = groupByHost([mk({ host: 'box', user: 'alice' })])
    expect(out[0]!.user).toBe('alice')
  })
  it('returns [] for empty input', () => {
    expect(groupByHost([])).toEqual([])
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/sessionGroups.test.ts
```
Expected: FAIL — module missing.

- [ ] **Step 3: Implement**

```ts
// desktop/frontend/src/mobile/sessionGroups.ts
import type { RemoteSession } from '../platform/types'

export interface HostGroup {
  host: string
  user: string
  sessions: RemoteSession[]
}

export function groupByHost(sessions: RemoteSession[]): HostGroup[] {
  const order: string[] = []
  const map = new Map<string, HostGroup>()
  for (const s of sessions) {
    let g = map.get(s.host)
    if (!g) {
      g = { host: s.host, user: s.user, sessions: [] }
      map.set(s.host, g)
      order.push(s.host)
    }
    g.sessions.push(s)
  }
  return order.map((h) => map.get(h)!)
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/sessionGroups.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/sessionGroups.ts desktop/frontend/src/mobile/__tests__/sessionGroups.test.ts
git commit -m "feat(mobile): groupByHost pure helper for session list"
```

---

### Task 4: MobileSetup.vue

**Files:**
- Create: `desktop/frontend/src/mobile/MobileSetup.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSetup from '../MobileSetup.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false, windowControls: false, autoUpdate: false, pluginHost: false, fileDialog: false }
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSetup', () => {
  it('renders url, token, insecure switch, connect', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="relay-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-token"]').exists()).toBe(true)
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(true)
    expect(w.find('[data-testid="connect"]').exists()).toBe(true)
  })

  it('shows validation error for malformed url', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('not a url')
    await w.find('[data-testid="relay-token"]').setValue('atk_x')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|malformed/i)
    expect(platform.relay.save).not.toHaveBeenCalled()
  })

  it('on success saves config, calls fetchMe, emits connected', async () => {
    ;(platform.relay.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'e' })
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-token"]').setValue('atk_good')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(platform.relay.save).toHaveBeenCalled()
    expect(platform.relay.fetchMe).toHaveBeenCalled()
    expect(w.emitted('connected')).toBeTruthy()
  })

  it('shows token-invalid error on fetchMe 401-style rejection', async () => {
    ;(platform.relay.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('relay fetchMe failed: HTTP 401'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-token"]').setValue('atk_bad')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/token|invalid|401/i)
    expect(w.emitted('connected')).toBeFalsy()
  })

  it('shows the token-invalid banner when reason prop is set', () => {
    const w = mount(MobileSetup, { props: { reason: 'token_invalid' } })
    expect(w.text()).toMatch(/token|expired|again/i)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileSetup.test.ts
```
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/mobile/MobileSetup.vue -->
<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePlatform } from '../platform'
import { validateRelayBase } from './relay'

const props = defineProps<{ reason?: 'token_invalid' | null }>()
const emit = defineEmits<{ (e: 'connected'): void }>()

const platform = usePlatform()
const url = ref('https://')
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)

const banner = computed(() =>
  props.reason === 'token_invalid'
    ? 'Your API token is no longer valid. Paste a fresh token to reconnect.'
    : null,
)

async function onConnect(): Promise<void> {
  error.value = null
  const v = validateRelayBase(url.value, allowInsecure.value)
  if (v) { error.value = v; return }
  if (!token.value.trim()) { error.value = 'API token is required'; return }
  submitting.value = true
  try {
    await platform.relay.save({
      url: url.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allow_insecure_relay: allowInsecure.value,
      remote_permission: 'full',
      connected: false,
    })
    await platform.relay.fetchMe()
    emit('connected')
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    error.value = /401/.test(msg)
      ? 'API token is invalid. Generate a new one from the relay web UI.'
      : /403|origin/i.test(msg)
        ? 'Relay rejected the origin. Start the relay with ATTERM_ORIGINS containing capacitor://localhost.'
        : `Cannot reach relay: ${msg}`
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="setup">
    <h1>AT Term</h1>
    <p class="sub">连接到 relay</p>
    <div v-if="banner" class="banner">{{ banner }}</div>
    <label class="field">
      <span>Relay URL</span>
      <input data-testid="relay-url" v-model="url" :disabled="submitting" placeholder="https://relay.example.com" autocomplete="off" autocapitalize="off" spellcheck="false" />
    </label>
    <label class="field">
      <span>API token</span>
      <input data-testid="relay-token" v-model="token" :disabled="submitting" type="password" placeholder="atk_…" autocomplete="off" />
    </label>
    <label class="row">
      <span>允许 insecure HTTP/WS（非 loopback）</span>
      <input data-testid="allow-insecure" v-model="allowInsecure" :disabled="submitting" type="checkbox" />
    </label>
    <p v-if="error" class="error">{{ error }}</p>
    <button data-testid="connect" class="btn" :disabled="submitting" @click="onConnect">Connect</button>
  </div>
</template>

<style scoped>
.setup { min-height: 100vh; display: flex; flex-direction: column; justify-content: center; padding: 2rem 1.25rem; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
h1 { text-align: center; margin: 0 0 4px; font-size: 1.6rem; }
.sub { text-align: center; color: #8d93a3; margin: 0 0 1.5rem; font-size: 0.9rem; }
.banner { background: rgba(245,158,11,.13); border: 1px solid rgba(245,158,11,.4); color: #f5c451; padding: 9px 11px; border-radius: 9px; margin-bottom: 1rem; font-size: 0.8rem; }
.field { display: block; margin-bottom: 0.9rem; }
.field span { display: block; font-size: 0.75rem; color: #8d93a3; margin-bottom: 0.35rem; }
.field input { width: 100%; height: 42px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #e6e7ea; padding: 0 12px; font-size: 0.95rem; font-family: ui-monospace, Menlo, monospace; }
.row { display: flex; align-items: center; justify-content: space-between; margin: 1rem 0; font-size: 0.85rem; }
.error { color: #f87171; font-size: 0.8rem; margin: 0 0 0.75rem; }
.btn { width: 100%; height: 46px; border: none; border-radius: 10px; background: #3b82f6; color: #fff; font-size: 1rem; font-weight: 600; }
.btn:disabled { opacity: 0.6; }
</style>
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileSetup.test.ts
```
Expected: PASS — 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileSetup.vue desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts
git commit -m "feat(mobile): MobileSetup relay-config screen"
```

---

### Task 5: MobileSessionList.vue (host-grouped)

**Files:**
- Create: `desktop/frontend/src/mobile/MobileSessionList.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import type { RemoteSession } from '../../platform/types'

const sessions: RemoteSession[] = [
  { session_id: 'a', host_id: 'h1', host: 'box1', user: 'me', title: 'claude', cols: 80, rows: 24 },
  { session_id: 'b', host_id: 'h1', host: 'box1', user: 'me', title: 'zsh', cols: 100, rows: 30 },
  { session_id: 'c', host_id: 'h2', host: 'box2', user: 'me', title: 'codex', cols: 120, rows: 40 },
]
let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue(sessions)
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSessionList', () => {
  it('lists sessions grouped by host on mount', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const headers = w.findAll('[data-testid="host-group"]').map((h) => h.text())
    expect(headers.some((t) => t.includes('box1'))).toBe(true)
    expect(headers.some((t) => t.includes('box2'))).toBe(true)
    expect(w.findAll('[data-testid="session-row"]').length).toBe(3)
  })

  it('marks open sessions with an open badge', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: ['a'] } })
    await flushPromises()
    expect(w.find('[data-testid="open-badge-a"]').exists()).toBe(true)
    expect(w.find('[data-testid="open-badge-b"]').exists()).toBe(false)
  })

  it('emits open(info) when a row is tapped', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="session-row"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
    expect((w.emitted('open')![0]![0] as RemoteSession).session_id).toBe('a')
  })

  it('refresh button re-fetches', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="refresh"]').trigger('click')
    await flushPromises()
    expect(platform.sessions.listRemoteSessions).toHaveBeenCalledTimes(2)
  })

  it('shows empty state when no sessions', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.text()).toMatch(/no remote sessions/i)
  })

  it('emits token-invalid when listRemoteSessions throws relay_unauthorized', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts
```
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/mobile/MobileSessionList.vue -->
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import { groupByHost, type HostGroup } from './sessionGroups'

defineProps<{ openSessionIds: string[] }>()
const emit = defineEmits<{
  (e: 'open', info: RemoteSession): void
  (e: 'editRelay'): void
  (e: 'tokenInvalid'): void
}>()

const platform = usePlatform()
const groups = ref<HostGroup[]>([])
const error = ref<string | null>(null)
const loading = ref(false)

async function refresh(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const sessions = await platform.sessions.listRemoteSessions()
    groups.value = groupByHost(sessions)
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'relay_unauthorized') { emit('tokenInvalid'); return }
    error.value = msg
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="list">
    <header class="bar">
      <span class="title">Sessions</span>
      <button data-testid="refresh" class="icon" :disabled="loading" @click="refresh">⟳</button>
      <button data-testid="gear" class="icon" @click="emit('editRelay')">⚙</button>
    </header>
    <div class="body">
      <p v-if="error" class="error">{{ error }}</p>
      <div v-for="g in groups" :key="g.host" class="group">
        <div data-testid="host-group" class="grouphdr">{{ g.host }} · {{ g.user }} · {{ g.sessions.length }}</div>
        <button
          v-for="s in g.sessions"
          :key="s.session_id"
          data-testid="session-row"
          class="sess"
          @click="emit('open', s)"
        >
          <span class="dot"></span>
          <span class="col2">
            <span class="ttl">{{ s.title }}</span>
            <span class="meta">{{ s.cols }}×{{ s.rows }}</span>
          </span>
          <span v-if="openSessionIds.includes(s.session_id)" :data-testid="`open-badge-${s.session_id}`" class="open">open</span>
        </button>
      </div>
      <p v-if="!loading && !error && groups.length === 0" class="empty">
        no remote sessions — start one from a desktop AT Term connected to this relay.
      </p>
    </div>
  </div>
</template>

<style scoped>
.list { min-height: 100vh; display: flex; flex-direction: column; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.bar .title { flex: 1; font-weight: 600; }
.icon { background: none; border: none; color: #8d93a3; font-size: 1.1rem; }
.body { flex: 1; overflow: auto; padding: 12px; }
.group { margin-bottom: 14px; }
.grouphdr { font-size: 0.72rem; color: #8d93a3; font-family: ui-monospace, Menlo, monospace; margin: 4px 2px 8px; }
.sess { width: 100%; display: flex; align-items: center; gap: 10px; padding: 11px 12px; margin-bottom: 8px; border-radius: 11px; background: #11182b; border: 1px solid #1e2638; color: inherit; text-align: left; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #22c55e; flex: 0 0 auto; }
.col2 { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ttl { font-size: 0.9rem; font-weight: 600; }
.meta { font-size: 0.72rem; color: #8d93a3; font-family: ui-monospace, Menlo, monospace; }
.open { font-size: 0.62rem; color: #9dc1ff; border: 1px solid rgba(59,130,246,.4); background: rgba(59,130,246,.12); border-radius: 5px; padding: 1px 6px; }
.empty { color: #8d93a3; font-size: 0.85rem; text-align: center; padding: 40px 12px; line-height: 1.6; }
.error { color: #f87171; font-size: 0.8rem; }
</style>
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts
```
Expected: PASS — 6 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileSessionList.vue desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts
git commit -m "feat(mobile): MobileSessionList host-grouped + open badges + refresh"
```

---

### Task 6: MobileTerminal.vue (lean xterm + SessionConnection)

**Files:**
- Create: `desktop/frontend/src/mobile/MobileTerminal.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts`

The component is hard to unit-test against a real xterm in jsdom, so we mock both `xterm` and `lib/connection`. The test asserts the wiring: a `SessionConnection` is created with the right endpoint+sessionId, `attach()` is called on mount, `detach()`+`dispose()` on unmount, and `ended`/`tokenInvalid` are emitted from the connection handlers.

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

const attach = vi.fn()
const detach = vi.fn()
const sendInput = vi.fn()
const sendResize = vi.fn()
let lastHandlers: any = null
let lastArgs: any = null

vi.mock('../../lib/connection', () => ({
  SessionConnection: class {
    constructor(endpoint: any, sessionId: string, handlers: any) {
      lastArgs = { endpoint, sessionId }
      lastHandlers = handlers
    }
    attach() { attach() }
    detach() { detach() }
    sendInput(s: string) { sendInput(s) }
    sendResize(c: number, r: number) { sendResize(c, r) }
  },
}))

const termWrite = vi.fn()
const termDispose = vi.fn()
const termFit = vi.fn()
vi.mock('xterm', () => ({
  Terminal: class {
    onData(cb: (s: string) => void) { (this as any)._onData = cb }
    onResize() {}
    open() {}
    write(d: unknown) { termWrite(d) }
    dispose() { termDispose() }
    focus() {}
    loadAddon() {}
  },
}))
vi.mock('xterm-addon-fit', () => ({
  FitAddon: class { fit() { termFit() } activate() {} },
}))

import MobileTerminal from '../MobileTerminal.vue'
import type { RemoteSession } from '../../platform/types'

const info: RemoteSession = { session_id: 's1', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24 }

beforeEach(() => { vi.clearAllMocks(); lastHandlers = null; lastArgs = null })

describe('MobileTerminal', () => {
  it('creates SessionConnection with endpoint+sessionId and attaches on mount', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    expect(lastArgs).toEqual({ endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1' })
    expect(attach).toHaveBeenCalledOnce()
  })

  it('writes incoming output to the terminal', () => {
    mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onOutput?.(new Uint8Array([104, 105]))
    expect(termWrite).toHaveBeenCalled()
  })

  it('emits ended on CLOSE', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onClose?.({ exit_code: 0 })
    expect(w.emitted('ended')).toBeTruthy()
  })

  it('emits tokenInvalid on error status', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    lastHandlers.onStatus?.('error')
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })

  it('detaches + disposes on unmount', () => {
    const w = mount(MobileTerminal, { props: { endpoint: { url: 'wss://r', token: 'atk_t' }, sessionId: 's1', info, active: true } })
    w.unmount()
    expect(detach).toHaveBeenCalledOnce()
    expect(termDispose).toHaveBeenCalledOnce()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
```
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/mobile/MobileTerminal.vue -->
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import 'xterm/css/xterm.css'
import { SessionConnection, type Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'

const props = defineProps<{
  endpoint: Endpoint
  sessionId: string
  info: RemoteSession
  active: boolean
}>()
const emit = defineEmits<{ (e: 'ended'): void; (e: 'tokenInvalid'): void }>()

const container = ref<HTMLDivElement | null>(null)
let term: Terminal | null = null
let fit: FitAddon | null = null
let conn: SessionConnection | null = null

function decode(data: Uint8Array): string {
  return new TextDecoder().decode(data)
}

const AUX_KEYS: { label: string; seq: string }[] = [
  { label: 'esc', seq: '\x1b' },
  { label: 'tab', seq: '\t' },
  { label: '⌃C', seq: '\x03' },
  { label: '↑', seq: '\x1b[A' },
  { label: '↓', seq: '\x1b[B' },
  { label: '←', seq: '\x1b[D' },
  { label: '→', seq: '\x1b[C' },
]
function sendAux(seq: string) { conn?.sendInput(seq) }

onMounted(() => {
  term = new Terminal({ fontSize: 12, convertEol: false, cursorBlink: true })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container.value!)
  try { fit.fit() } catch { /* not laid out yet */ }
  term.onData((s: string) => conn?.sendInput(s))
  term.onResize(({ cols, rows }) => conn?.sendResize(cols, rows))

  conn = new SessionConnection(props.endpoint, props.sessionId, {
    onOutput: (data) => term?.write(decode(data)),
    onClose: () => emit('ended'),
    onStatus: (s) => { if (s === 'error') emit('tokenInvalid') },
  })
  conn.attach()
})

watch(() => props.active, (now) => {
  if (now) {
    // xterm could not measure while hidden (v-show); re-fit + focus on activate.
    requestAnimationFrame(() => { try { fit?.fit() } catch { /* */ } ; term?.focus() })
  }
})

onBeforeUnmount(() => {
  conn?.detach()
  conn = null
  term?.dispose()
  term = null
  fit = null
})
</script>

<template>
  <div class="mobile-term">
    <div ref="container" class="term"></div>
    <div class="kbbar">
      <button v-for="k in AUX_KEYS" :key="k.label" class="key" @click="sendAux(k.seq)">{{ k.label }}</button>
    </div>
  </div>
</template>

<style scoped>
.mobile-term { display: flex; flex-direction: column; height: 100%; background: #000; }
.term { flex: 1; min-height: 0; }
.kbbar { height: 42px; border-top: 1px solid #1e2638; background: #0b1020; display: flex; align-items: center; gap: 6px; padding: 0 8px; overflow-x: auto; }
.key { flex: 0 0 auto; height: 28px; min-width: 34px; padding: 0 9px; border-radius: 7px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; font-family: ui-monospace, Menlo, monospace; }
</style>
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminal.test.ts
```
Expected: PASS — 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileTerminal.vue desktop/frontend/src/mobile/__tests__/MobileTerminal.test.ts
git commit -m "feat(mobile): lean MobileTerminal over lib/connection SessionConnection"
```

---

### Task 7: MobileTerminalHost.vue (tab strip + keepalive container)

**Files:**
- Create: `desktop/frontend/src/mobile/MobileTerminalHost.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileTerminalHost.test.ts`

Tests stub `MobileTerminal` (global stub) so we assert host behavior — tab rendering, switch/close/back emits, and that all open terminals stay mounted (v-show) regardless of active.

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/MobileTerminalHost.test.ts
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileTerminalHost from '../MobileTerminalHost.vue'
import type { RemoteSession } from '../../platform/types'

const mk = (id: string, title: string): RemoteSession =>
  ({ session_id: id, host_id: 'h', host: 'box', user: 'me', title, cols: 80, rows: 24 })

const stubs = { MobileTerminal: { props: ['active', 'sessionId'], template: '<div class="mt" :data-active="active" :data-sid="sessionId"></div>' } }

const baseProps = {
  endpoint: { url: 'wss://r', token: 'atk_t' },
  openTerminals: [
    { sessionId: 'a', info: mk('a', 'claude') },
    { sessionId: 'b', info: mk('b', 'codex') },
  ],
  activeSessionId: 'a',
}

describe('MobileTerminalHost', () => {
  it('renders one tab per open terminal + a MobileTerminal per open terminal', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    expect(w.findAll('[data-testid="term-tab"]').length).toBe(2)
    expect(w.findAll('.mt').length).toBe(2)
  })

  it('only the active terminal is visibly active; both stay mounted', () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    const actives = w.findAll('.mt').map((m) => m.attributes('data-active'))
    expect(actives.filter((a) => a === 'true').length).toBe(1)
    expect(w.findAll('.mt').length).toBe(2)   // both mounted (keepalive)
  })

  it('emits switch when a non-active tab is tapped', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.findAll('[data-testid="term-tab"]')[1]!.trigger('click')
    expect(w.emitted('switch')![0]).toEqual(['b'])
  })

  it('emits close when a tab × is tapped', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.find('[data-testid="tab-close-a"]').trigger('click')
    expect(w.emitted('close')![0]).toEqual(['a'])
  })

  it('emits back from the back button', async () => {
    const w = mount(MobileTerminalHost, { props: baseProps, global: { stubs } })
    await w.find('[data-testid="term-back"]').trigger('click')
    expect(w.emitted('back')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminalHost.test.ts
```
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/mobile/MobileTerminalHost.vue -->
<script setup lang="ts">
import MobileTerminal from './MobileTerminal.vue'
import type { Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'

export interface OpenTerminal { sessionId: string; info: RemoteSession }

const props = defineProps<{
  endpoint: Endpoint
  openTerminals: OpenTerminal[]
  activeSessionId: string
}>()
const emit = defineEmits<{
  (e: 'switch', sessionId: string): void
  (e: 'close', sessionId: string): void
  (e: 'back'): void
  (e: 'ended', sessionId: string): void
  (e: 'tokenInvalid'): void
}>()

function activeTitle(): string {
  return props.openTerminals.find((t) => t.sessionId === props.activeSessionId)?.info.title ?? ''
}
</script>

<template>
  <div class="host">
    <header class="bar">
      <button data-testid="term-back" class="back" @click="emit('back')">‹</button>
      <span class="title">{{ activeTitle() }}</span>
    </header>
    <div class="tabstrip">
      <div
        v-for="t in openTerminals"
        :key="t.sessionId"
        data-testid="term-tab"
        class="tab"
        :class="{ active: t.sessionId === activeSessionId }"
        @click="emit('switch', t.sessionId)"
      >
        <span class="lbl">{{ t.info.title }}</span>
        <span :data-testid="`tab-close-${t.sessionId}`" class="x" @click.stop="emit('close', t.sessionId)">×</span>
      </div>
    </div>
    <div class="stage">
      <div
        v-for="t in openTerminals"
        :key="t.sessionId"
        v-show="t.sessionId === activeSessionId"
        class="pane"
      >
        <MobileTerminal
          :endpoint="endpoint"
          :session-id="t.sessionId"
          :info="t.info"
          :active="t.sessionId === activeSessionId"
          @ended="emit('ended', t.sessionId)"
          @token-invalid="emit('tokenInvalid')"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.host { display: flex; flex-direction: column; height: 100vh; background: #000; color: #e6e7ea; }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 8px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.back { background: none; border: none; color: #3b82f6; font-size: 1.5rem; width: 28px; }
.title { flex: 1; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.95rem; }
.tabstrip { display: flex; gap: 6px; padding: 7px 8px; background: #0b1020; border-bottom: 1px solid #1e2638; overflow-x: auto; }
.tab { flex: 0 0 auto; height: 28px; padding: 0 8px; border-radius: 7px; display: flex; align-items: center; gap: 6px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; }
.tab.active { background: rgba(59,130,246,.16); border-color: rgba(59,130,246,.5); color: #cfe0ff; }
.tab .x { color: #5b6478; font-size: 0.85rem; }
.stage { flex: 1; min-height: 0; position: relative; }
.pane { position: absolute; inset: 0; }
</style>
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileTerminalHost.test.ts
```
Expected: PASS — 5 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileTerminalHost.vue desktop/frontend/src/mobile/__tests__/MobileTerminalHost.test.ts
git commit -m "feat(mobile): MobileTerminalHost tab strip + v-show keepalive container"
```

---

### Task 8: MobileApp.vue (root + navigation + keepalive registry)

**Files:**
- Create: `desktop/frontend/src/mobile/MobileApp.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileApp.test.ts`

Owns the keepalive registry + LRU cap. Tests stub the three child views so we assert state transitions, not their internals.

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/mobile/__tests__/MobileApp.test.ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileApp from '../MobileApp.vue'
import type { RemoteSession } from '../../platform/types'

const mk = (id: string, host = 'box'): RemoteSession =>
  ({ session_id: id, host_id: 'h', host, user: 'me', title: id, cols: 80, rows: 24 })

// Stub children to expose hooks via emitted events / props.
const stubs = {
  MobileSetup: { template: '<button data-testid="stub-connect" @click="$emit(\'connected\')"></button>' },
  MobileSessionList: {
    props: ['openSessionIds'],
    template: '<div data-testid="stub-list" :data-open="openSessionIds.join(\',\')"></div>',
  },
  MobileTerminalHost: {
    props: ['openTerminals', 'activeSessionId'],
    template: '<div data-testid="stub-host" :data-active="activeSessionId" :data-count="openTerminals.length"></div>',
  },
}

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false }
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

async function mountWith(loaded: boolean) {
  ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue(
    loaded ? { url: 'https://r', token: 'atk_t', allow_insecure_relay: false, remote_permission: 'full', connected: false } : null,
  )
  const w = mount(MobileApp, { global: { stubs } })
  await flushPromises()
  return w
}

describe('MobileApp navigation + keepalive', () => {
  it('boots to setup when no relay config', async () => {
    const w = await mountWith(false)
    expect(w.find('[data-testid="stub-connect"]').exists()).toBe(true)
  })

  it('boots to list when relay config present', async () => {
    const w = await mountWith(true)
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
  })

  it('connected event navigates setup → list', async () => {
    const w = await mountWith(false)
    await w.find('[data-testid="stub-connect"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
  })

  it('opening a session adds it to the keepalive registry and shows terminal host', async () => {
    const w = await mountWith(true)
    ;(w.findComponent({ name: 'MobileSessionList' }) as any).vm.$emit('open', mk('a'))
    await flushPromises()
    const host = w.find('[data-testid="stub-host"]')
    expect(host.exists()).toBe(true)
    expect(host.attributes('data-count')).toBe('1')
    expect(host.attributes('data-active')).toBe('a')
  })

  it('back returns to list but keeps terminals open', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    list().vm.$emit('open', mk('a'))
    await flushPromises()
    ;(w.findComponent({ name: 'MobileTerminalHost' }) as any).vm.$emit('back')
    await flushPromises()
    expect(w.find('[data-testid="stub-list"]').exists()).toBe(true)
    // open session still tracked → badge passed down
    expect(w.find('[data-testid="stub-list"]').attributes('data-open')).toBe('a')
  })

  it('LRU caps at 4 open terminals — opening a 5th evicts the oldest', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    const host = () => w.findComponent({ name: 'MobileTerminalHost' }) as any
    for (const id of ['a', 'b', 'c', 'd']) {
      list().vm.$emit('open', mk(id))
      await flushPromises()
      host().vm.$emit('back')
      await flushPromises()
    }
    list().vm.$emit('open', mk('e'))
    await flushPromises()
    expect(w.find('[data-testid="stub-host"]').attributes('data-count')).toBe('4')
  })

  it('tokenInvalid resets to setup and clears open terminals', async () => {
    const w = await mountWith(true)
    const list = () => w.findComponent({ name: 'MobileSessionList' }) as any
    list().vm.$emit('open', mk('a'))
    await flushPromises()
    ;(w.findComponent({ name: 'MobileTerminalHost' }) as any).vm.$emit('tokenInvalid')
    await flushPromises()
    expect(w.find('[data-testid="stub-connect"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileApp.test.ts
```
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```vue
<!-- desktop/frontend/src/mobile/MobileApp.vue -->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import type { Endpoint } from '../lib/connection'
import { relayBaseToWsUrl } from './relay'
import MobileSetup from './MobileSetup.vue'
import MobileSessionList from './MobileSessionList.vue'
import MobileTerminalHost, { type OpenTerminal } from './MobileTerminalHost.vue'

const MAX_OPEN_TERMINALS = 4

type View = 'setup' | 'list' | 'terminal'
const platform = usePlatform()

const view = ref<View>('setup')
const reason = ref<'token_invalid' | null>(null)
const openTerminals = ref<OpenTerminal[]>([])   // insertion order = LRU recency
const activeSessionId = ref<string>('')
const endpoint = ref<Endpoint>({ url: '', token: '' })

const openSessionIds = computed(() => openTerminals.value.map((t) => t.sessionId))

async function refreshEndpoint(): Promise<void> {
  const cfg = await platform.relay.load()
  if (cfg) endpoint.value = { url: relayBaseToWsUrl(cfg.url), token: cfg.token }
}

onMounted(async () => {
  const cfg = await platform.relay.load()
  if (cfg) { await refreshEndpoint(); view.value = 'list' } else { view.value = 'setup' }
})

async function onConnected(): Promise<void> {
  await refreshEndpoint()
  reason.value = null
  view.value = 'list'
}

function activate(sessionId: string): void {
  // move to end (most-recently-active) for LRU
  const idx = openTerminals.value.findIndex((t) => t.sessionId === sessionId)
  if (idx >= 0) {
    const [t] = openTerminals.value.splice(idx, 1)
    openTerminals.value.push(t!)
  }
  activeSessionId.value = sessionId
  view.value = 'terminal'
}

function onOpenSession(info: RemoteSession): void {
  const existing = openTerminals.value.find((t) => t.sessionId === info.session_id)
  if (existing) { activate(info.session_id); return }
  if (openTerminals.value.length >= MAX_OPEN_TERMINALS) {
    // evict least-recently-active that is not about to be active (the head)
    openTerminals.value.shift()
  }
  openTerminals.value.push({ sessionId: info.session_id, info })
  activate(info.session_id)
}

function onSwitch(sessionId: string): void { activate(sessionId) }

function removeTerminal(sessionId: string): void {
  const idx = openTerminals.value.findIndex((t) => t.sessionId === sessionId)
  if (idx < 0) return
  openTerminals.value.splice(idx, 1)
  if (activeSessionId.value === sessionId) {
    const next = openTerminals.value[openTerminals.value.length - 1]
    if (next) { activeSessionId.value = next.sessionId; view.value = 'terminal' }
    else { view.value = 'list' }
  }
}

function onClose(sessionId: string): void { removeTerminal(sessionId) }
function onEnded(sessionId: string): void { removeTerminal(sessionId) }
function onBack(): void { view.value = 'list' }
function onEditRelay(): void { reason.value = null; view.value = 'setup' }

function onTokenInvalid(): void {
  openTerminals.value = []   // unmounts all MobileTerminal → each detaches
  activeSessionId.value = ''
  reason.value = 'token_invalid'
  view.value = 'setup'
}
</script>

<!--
  NOTE: `openSessionIds` is the single source of open-session ids for the list
  badge; the template binds it (not an inline map) so the computed stays the one
  place that reads `t.sessionId`.
-->
<template>
  <MobileSetup v-if="view === 'setup'" :reason="reason" @connected="onConnected" />
  <MobileSessionList
    v-else-if="view === 'list'"
    :open-session-ids="openSessionIds"
    @open="onOpenSession"
    @edit-relay="onEditRelay"
    @token-invalid="onTokenInvalid"
  />
  <MobileTerminalHost
    v-else
    :endpoint="endpoint"
    :open-terminals="openTerminals"
    :active-session-id="activeSessionId"
    @switch="onSwitch"
    @close="onClose"
    @back="onBack"
    @ended="onEnded"
    @token-invalid="onTokenInvalid"
  />
</template>
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd desktop/frontend && npx vitest run src/mobile/__tests__/MobileApp.test.ts
```
Expected: PASS — 8 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileApp.vue desktop/frontend/src/mobile/__tests__/MobileApp.test.ts
git commit -m "feat(mobile): MobileApp root — nav + keepalive registry (LRU cap 4)"
```

---

### Task 9: Wire main.capacitor.ts + delete MobilePlaceholder

**Files:**
- Modify: `desktop/frontend/src/main.capacitor.ts`
- Delete: `desktop/frontend/src/MobilePlaceholder.vue`
- Delete: `desktop/frontend/src/__tests__/MobilePlaceholder.test.ts`

- [ ] **Step 1: Update `main.capacitor.ts`**

Replace its body so it mounts `MobileApp`:

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import MobileApp from './mobile/MobileApp.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const platform = initPlatform(createCapacitorPlatform)

const app = createApp(MobileApp)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
```

- [ ] **Step 2: Delete the placeholder + its test**

```bash
git rm desktop/frontend/src/MobilePlaceholder.vue desktop/frontend/src/__tests__/MobilePlaceholder.test.ts
```

- [ ] **Step 3: Type-check + capacitor build**

```bash
cd desktop/frontend && npx vue-tsc --noEmit && npm run build:capacitor 2>&1 | tail -5
test -f desktop/frontend/dist-capacitor/index.html && echo "OK"
```
Expected: zero tsc errors; build succeeds; entry present.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/main.capacitor.ts
git commit -m "feat(mobile): capacitor entry mounts MobileApp; remove MobilePlaceholder"
```

---

### Task 10: mobile/README PR-C smoke checklist + final verification

**Files:**
- Modify: `mobile/README.md`

- [ ] **Step 1: Replace the PR-B placeholder note in `mobile/README.md`**

Find the `## Relay configuration` section's PR-B placeholder paragraph and replace it with the PR-C smoke checklist:

```markdown
## Relay configuration & smoke (PR-C)

On first launch the app shows the **setup** screen: relay URL + API token + "allow insecure" toggle. Generate the token on a desktop browser (relay Settings → API Tokens). Start the relay allowing the WebView origin:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```

iOS simulator smoke checklist:

1. Cold start, no config → setup screen.
2. Bad token → "API token is invalid"; not navigated away.
3. Valid token → session list, grouped by host.
4. Tap a session → terminal attaches; output shows; typing echoes.
5. Back → list; tap the same session → instant (no reconnect/replay).
6. Open a second session → tab strip shows both; switch between tabs is instant.
7. Open 5 sessions → oldest auto-detaches (≤4 tabs).
8. `×` a tab → that terminal closes; others unaffected.
9. Revoke the token on the relay → next refresh → back to setup with "token invalid" banner.
10. Gear → setup → reconnect with a new token.
```

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add mobile/README.md
git commit -m "docs(mobile): PR-C relay setup + iOS smoke checklist"
```

- [ ] **Step 3: Final verification gate (no commit)**

```bash
cd desktop/frontend && npm test 2>&1 | tail -8
npx vue-tsc --noEmit && echo "tsc ok"
npm run build:wails 2>&1 | tail -3 && test -f dist/index.html && echo "wails ok"
npm run build:capacitor 2>&1 | tail -3 && test -f dist-capacitor/index.html && echo "capacitor ok"
cd ../mobile && npm run sync-web 2>&1 | tail -3 && test -f www/index.html && echo "sync ok"
```
Expected: all green; both builds produce their entry; sync populates `mobile/www/`.

- [ ] **Step 4: iOS simulator smoke (manual, owner runs)**

```bash
cd mobile && npm run ios:open
```
Walk the 10-item checklist above.

---

## Self-Review Notes

- **Spec coverage:** setup (Task 4), host-grouped list (Tasks 3+5), lean terminal over lib/connection (Task 6), keepalive tab host (Task 7), MobileApp nav + LRU cap 4 + token-invalid reset (Task 8), listRemoteSessions wiring (Task 2), validation (Task 1), main.capacitor + delete placeholder (Task 9), docs/smoke (Task 10). All spec sections mapped.
- **Endpoint contract:** Task 1's `relayBaseToWsUrl` produces the bare ws base; Task 6 passes `{url, token}` to `SessionConnection`, which appends `/client` — matches the verified `webSocketAuth` behavior (no double `/client`).
- **Keepalive correctness:** `MobileTerminalHost` keeps every open terminal mounted via `v-show`; `MobileApp` only adds/removes registry entries → mount/unmount = attach/detach. Tests assert mount count stays at the registry length and that back doesn't shrink it.
- **Out-of-scope guards:** no edits to `lib/api.ts`, `lib/connection.ts`, `platform/{types,index,wails}.ts`, desktop components, `web/`.
- **Desktop untouched:** capacitor-only files; `main.ts` (wails) and `App.vue` not modified. `build:wails` smoke in Task 10 confirms.
- **Token-invalid string match:** `MobileSessionList` matches `'relay_unauthorized'` (thrown by Task 2's listRemoteSessions); `MobileTerminal` emits `tokenInvalid` on `onStatus('error')`. Both bubble to `MobileApp.onTokenInvalid`.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-23-mobile-pr-c.md`. Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review.
2. **Inline Execution** — checkpoints in this session.

Which approach?
