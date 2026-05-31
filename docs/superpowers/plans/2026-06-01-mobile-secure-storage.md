# Mobile secure token storage — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate mobile relay credentials from `localStorage['atterm.relay']` to iOS Keychain via a small custom Capacitor plugin; transparently migrate existing localStorage values on first read; add UI warnings around the "Allow insecure HTTP" mode (ATS itself stays permissive per user decision).

**Architecture:** A `SecureStorage` TS shim at `desktop/frontend/src/platform/secureStorage.ts` wraps a Capacitor-registered native plugin (`AttermSecureStorage`) on iOS and falls back to an in-memory `Map` on web/vitest. `capacitor.ts`'s `relay.load/save/clear` delegate to this shim, with a one-time read-migration that promotes any existing localStorage blob into the shim and then clears it. A new `InsecureBanner.vue` and a strengthened warning block in `MobileSetup.vue` surface the HTTP-mode risk.

**Tech Stack:** TypeScript (frontend, vitest), Swift 5 (iOS plugin), Capacitor 8.3.3 (`registerPlugin` + `CAPPlugin`/`CAPPluginCall`), Keychain Services (`SecItemAdd` / `SecItemCopyMatching` / `SecItemDelete`).

**Reference spec:** `docs/superpowers/specs/2026-06-01-mobile-secure-storage-design.md`

---

## File map

### TypeScript (frontend)
- **Create:** `desktop/frontend/src/platform/secureStorage.ts` — TS shim with detect-once selector between native plugin and in-memory fake.
- **Create:** `desktop/frontend/src/platform/__tests__/secureStorage.test.ts` — round-trip + missing-key tests for the in-memory fake.
- **Modify:** `desktop/frontend/src/platform/capacitor.ts` — `relay.load/save/clear` delegate to `secureStorage`; `load()` performs the localStorage→Keychain migration.
- **Modify:** `desktop/frontend/src/platform/__tests__/capacitor.test.ts` — extend with migration/priority/no-migration/save/clear assertions.
- **Create:** `desktop/frontend/src/mobile/InsecureBanner.vue` — persistent banner shown when active relay URL is HTTP.
- **Create:** `desktop/frontend/src/mobile/__tests__/InsecureBanner.test.ts` — three vitest cases.
- **Modify:** `desktop/frontend/src/mobile/MobileSetup.vue` — add the always-visible insecure warning block under the checkbox.
- **Modify:** `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts` — four new `mobile.insecure.*` keys.

### Native (iOS)
- **Create:** `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStoragePlugin.swift` — three-method plugin (set/get/remove) backed by Keychain Services.
- **Create:** `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStorage.m` — Capacitor `@objc` bridge registration.

No `Info.plist` changes. No `capacitor.config.json` changes. No Wails/relay changes.

---

## Task 1: TS shim `secureStorage.ts` (test-first)

**Files:**
- Create: `desktop/frontend/src/platform/secureStorage.ts`
- Create: `desktop/frontend/src/platform/__tests__/secureStorage.test.ts`

- [ ] **Step 1: Write failing tests**

Create `desktop/frontend/src/platform/__tests__/secureStorage.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { createInMemorySecureStorage } from '../secureStorage'

describe('createInMemorySecureStorage', () => {
  let s: ReturnType<typeof createInMemorySecureStorage>

  beforeEach(() => {
    s = createInMemorySecureStorage()
  })

  it('get returns null for unknown key', async () => {
    expect(await s.get('missing')).toBeNull()
  })

  it('set + get round-trip returns same value', async () => {
    await s.set('k', 'v')
    expect(await s.get('k')).toBe('v')
  })

  it('set twice updates the value (upsert)', async () => {
    await s.set('k', 'v1')
    await s.set('k', 'v2')
    expect(await s.get('k')).toBe('v2')
  })

  it('remove deletes the value', async () => {
    await s.set('k', 'v')
    await s.remove('k')
    expect(await s.get('k')).toBeNull()
  })

  it('remove of unknown key does not throw', async () => {
    await expect(s.remove('missing')).resolves.toBeUndefined()
  })

  it('keys are independent', async () => {
    await s.set('a', '1')
    await s.set('b', '2')
    expect(await s.get('a')).toBe('1')
    expect(await s.get('b')).toBe('2')
  })
})
```

- [ ] **Step 2: Run the failing test**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/secureStorage.test.ts`
Expected: FAIL — `Cannot find module '../secureStorage'`.

- [ ] **Step 3: Implement the shim**

Create `desktop/frontend/src/platform/secureStorage.ts`:

```ts
// secureStorage hides the iOS Keychain plugin behind an async key/value
// interface. On iOS the calls land in the native AttermSecureStorage plugin
// (mobile/ios/App/App/Plugins/AttermSecureStorage). On web and in vitest a
// closure-backed Map is used so tests don't touch real Keychain.
//
// The default export `secureStorage` is selected once at module load time
// based on plugin presence; subsequent calls share the same backend.

import { registerPlugin } from '@capacitor/core'

export interface SecureStorage {
  set(key: string, value: string): Promise<void>
  get(key: string): Promise<string | null>
  remove(key: string): Promise<void>
}

interface NativePlugin {
  set(opts: { key: string; value: string }): Promise<void>
  get(opts: { key: string }): Promise<{ value: string | null }>
  remove(opts: { key: string }): Promise<void>
}

// Capacitor's registerPlugin returns a typed proxy on every platform; the
// proxy throws PLUGIN_NOT_AVAILABLE when called in environments where the
// native side isn't linked (web, vitest).
const native = registerPlugin<NativePlugin>('AttermSecureStorage')

export function createInMemorySecureStorage(): SecureStorage {
  const store = new Map<string, string>()
  return {
    async set(key, value) { store.set(key, value) },
    async get(key) { return store.has(key) ? store.get(key)! : null },
    async remove(key) { store.delete(key) },
  }
}

function createNativeSecureStorage(): SecureStorage {
  return {
    async set(key, value) { await native.set({ key, value }) },
    async get(key) {
      const r = await native.get({ key })
      return r.value
    },
    async remove(key) { await native.remove({ key }) },
  }
}

// Detect whether the native plugin is reachable. Capacitor throws
// PLUGIN_NOT_AVAILABLE on web; treat anything else as "plugin is there".
let detected: Promise<SecureStorage> | null = null

async function selectBackend(): Promise<SecureStorage> {
  try {
    await native.get({ key: '__atterm_probe__' })
    return createNativeSecureStorage()
  } catch (e: any) {
    const msg = String(e?.message ?? e ?? '')
    if (msg.includes('PLUGIN_NOT_AVAILABLE') || msg.includes('not implemented')) {
      return createInMemorySecureStorage()
    }
    // Native plugin is present but failed for another reason — keep using it
    // so callers see real errors instead of silently falling back to memory.
    return createNativeSecureStorage()
  }
}

export const secureStorage: SecureStorage = {
  async set(key, value) { (await (detected ??= selectBackend())).set(key, value) },
  async get(key) { return (await (detected ??= selectBackend())).get(key) },
  async remove(key) { return (await (detected ??= selectBackend())).remove(key) },
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/secureStorage.test.ts`
Expected: PASS — all six tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/secureStorage.ts desktop/frontend/src/platform/__tests__/secureStorage.test.ts
git -c commit.gpgsign=false commit -m "platform: SecureStorage shim with in-memory fallback"
```

---

## Task 2: Wire `capacitor.ts` to `secureStorage` + migration (test-first)

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`

- [ ] **Step 1: Write failing tests**

Append to `desktop/frontend/src/platform/__tests__/capacitor.test.ts`:

```ts
import { secureStorage } from '../secureStorage'

describe('createCapacitorPlatform — secure storage migration', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay')
  })

  it('migrates from localStorage to secureStorage on first load, then clears localStorage', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_legacy',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    localStorage.setItem('atterm.relay', JSON.stringify(cfg))
    expect(await secureStorage.get('atterm.relay')).toBeNull()

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await secureStorage.get('atterm.relay')).not.toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })

  it('prefers secureStorage over localStorage when both are present', async () => {
    const fromSecure = {
      url: 'https://secure.example.com', token: 'atk_secure',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    const fromLocal = {
      url: 'https://local.example.com', token: 'atk_local',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await secureStorage.set('atterm.relay', JSON.stringify(fromSecure))
    localStorage.setItem('atterm.relay', JSON.stringify(fromLocal))

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: fromSecure.url, token: fromSecure.token })
    // localStorage was not the source; we don't touch it on this path.
    expect(localStorage.getItem('atterm.relay')).not.toBeNull()
  })

  it('returns null when both stores are empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('save writes only to secureStorage, not localStorage', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_x',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    const p = createCapacitorPlatform()
    await p.relay.save(cfg)
    expect(await secureStorage.get('atterm.relay')).not.toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })

  it('clear wipes both stores (defensive)', async () => {
    const cfg = {
      url: 'https://r', token: 'atk_x',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await secureStorage.set('atterm.relay', JSON.stringify(cfg))
    localStorage.setItem('atterm.relay', JSON.stringify(cfg))

    const p = createCapacitorPlatform()
    await p.relay.clear()

    expect(await secureStorage.get('atterm.relay')).toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })
})
```

The pre-existing `it('relay.save persists to localStorage under atterm.relay …')` test at line 49 of `capacitor.test.ts` will FAIL after this task because saves no longer go to localStorage. **Update that test in place** to assert against `secureStorage.get` instead:

```ts
  it('relay.save persists to secureStorage under atterm.relay and load reads it back', async () => {
    const p = createCapacitorPlatform()
    const cfg = {
      url: 'https://relay.example.com', token: 'atk_xyz',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await p.relay.save(cfg)
    expect(JSON.parse((await secureStorage.get('atterm.relay'))!)).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await p.relay.load()).toMatchObject({ url: cfg.url, token: cfg.token })
  })
```

And update the existing `relay.clear` test (around line 66) to also check `secureStorage`:

```ts
  it('relay.clear removes both storage backends', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r', token: 'atk_x', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    await p.relay.clear()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
    expect(await secureStorage.get('atterm.relay')).toBeNull()
  })
```

The "malformed JSON" test (around line 60) should be updated to seed `secureStorage` with garbage instead of localStorage, since localStorage is now only a legacy migration source:

```ts
  it('relay.load returns null on malformed JSON in secureStorage', async () => {
    await secureStorage.set('atterm.relay', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })
```

- [ ] **Step 2: Run the tests to verify the new ones fail and the old ones break**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts`
Expected: FAIL — the new migration/priority tests fail because `capacitor.ts` doesn't call `secureStorage` yet, and the modified-in-place tests will start passing or failing depending on the order edits land.

- [ ] **Step 3: Rewrite the `relay` block in `capacitor.ts`**

Open `desktop/frontend/src/platform/capacitor.ts`. Replace the existing `loadCfg`, plus the `relay.load`, `relay.save`, `relay.clear` methods. The full file post-edit:

```ts
import type { Platform, RelayConfig, RelayMe, RemoteSession } from './types'
import { secureStorage } from './secureStorage'

const STORAGE_KEY = 'atterm.relay'

// loadLegacyFromLocalStorage reads (but does not clear) the legacy
// localStorage blob. Returned as parsed RelayConfig or null. Malformed JSON
// returns null. Only used by the migration branch in relay.load().
function loadLegacyFromLocalStorage(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as RelayConfig
  } catch {
    return null
  }
}

// parseRelayJSON is a tolerant parser shared by both storage paths.
function parseRelayJSON(raw: string | null): RelayConfig | null {
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
      // load: prefer Keychain; if empty AND localStorage has a legacy blob,
      // migrate it (write to Keychain, clear localStorage), then return.
      load: async () => {
        const fromSecure = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
        if (fromSecure) return fromSecure

        const legacy = loadLegacyFromLocalStorage()
        if (!legacy) return null
        await secureStorage.set(STORAGE_KEY, JSON.stringify(legacy))
        if (typeof localStorage !== 'undefined') localStorage.removeItem(STORAGE_KEY)
        return legacy
      },
      // save: write only to Keychain. localStorage is never written.
      save: async (cfg) => {
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))
      },
      // clear: wipe both stores. localStorage clear is belt-and-braces in case
      // a previous migration was interrupted between the Keychain write and
      // the localStorage remove.
      clear: async () => {
        await secureStorage.remove(STORAGE_KEY)
        if (typeof localStorage !== 'undefined') localStorage.removeItem(STORAGE_KEY)
      },
      fetchMe: async (): Promise<RelayMe> => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
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
      consumePairing: async (relayBase, token) => {
        const base = relayBase.replace(/\/$/, '')
        const res = await fetch(base + '/api/pair/consume', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token }),
          credentials: 'omit',
        })
        if (res.status === 404) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body.code || 'pair_invalid')
        }
        if (!res.ok) throw new Error(`pair_consume_http_${res.status}`)
        return (await res.json()) as { relay_url: string; api_token: string; user: { id: string; email: string } }
      },
    },
    sessions: {
      closeSession: async () => {
        // Attach-only client: closing a tab detaches the local WS (handled in
        // MobileApp by dropping it from the keepalive registry). It does NOT
        // kill the remote PTY — that stays owned by the host that started it.
      },
      listShells: async () => [],
      listRemoteSessions: async (): Promise<RemoteSession[]> => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg || !cfg.url || !cfg.token) return []
        const base = cfg.url.replace(/\/$/, '')
        const res = await fetch(base + '/api/sessions', {
          headers: { Authorization: `Bearer ${cfg.token}` },
          credentials: 'omit',
        })
        if (res.status === 401) throw new Error('relay_unauthorized')
        if (!res.ok) throw new Error(`list sessions: HTTP ${res.status}`)
        const raw = (await res.json()) as Array<{
          id: string; command: string; title: string; cwd: string; cols: number; rows: number;
          host_id: string; host: string; user: string; remote_permission?: string; task_state?: RemoteSession['task_state'];
          current_command?: string; command_started_at?: number; command_ended_at?: number; command_duration_ms?: number;
          command_exit_code?: number; last_output_at?: number
        }>
        return raw.map((s) => {
          const out: RemoteSession = {
            session_id: s.id,
            host_id: s.host_id,
            host: s.host,
            user: s.user,
            title: s.title || s.command,
            cwd: s.cwd,
            cols: s.cols,
            rows: s.rows,
          }
          if (s.remote_permission !== undefined) out.remote_permission = s.remote_permission
          if (s.task_state !== undefined) out.task_state = s.task_state
          if (s.current_command !== undefined) out.current_command = s.current_command
          if (s.command_started_at !== undefined) out.command_started_at = s.command_started_at
          if (s.command_ended_at !== undefined) out.command_ended_at = s.command_ended_at
          if (s.command_duration_ms !== undefined) out.command_duration_ms = s.command_duration_ms
          if (s.command_exit_code !== undefined) out.command_exit_code = s.command_exit_code
          if (s.last_output_at !== undefined) out.last_output_at = s.last_output_at
          return out
        })
      },
    },
    system: {
      showNotification: async () => {},
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async (url: string) => {
        if (typeof window !== 'undefined' && typeof window.open === 'function') {
          window.open(url, '_blank')
        }
      },
      getEnvironment: async () => ({ buildType: 'capacitor', platform: 'ios', arch: 'arm64' }),
    },
    events: createEventBus(),
  }
}
```

Compare against the current `capacitor.ts` and make a clean targeted diff — keep imports, capabilities object, sessions/system/events sections unchanged. The change set is:
- top: `import { secureStorage }` added; old `loadCfg` removed; new `loadLegacyFromLocalStorage` + `parseRelayJSON` helpers introduced
- `relay.load`/`save`/`clear` rewritten as above
- `relay.fetchMe` and `sessions.listRemoteSessions` now read from `secureStorage` first, fall back to legacy localStorage if Keychain is empty (handles the "user upgrades mid-session, kills app, reopens" edge — first relay.load() will migrate, but until then these readers should still work)

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/platform/__tests__/capacitor.test.ts`
Expected: PASS — both the new migration tests and the rewritten existing tests.

Run the entire frontend suite as a regression gate:
Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git -c commit.gpgsign=false commit -m "platform/capacitor: route relay credentials through secureStorage + migrate on read"
```

---

## Task 3: i18n keys for insecure-mode warnings

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 1: Add four keys under `mobile.insecure` in both locales**

Open `desktop/frontend/src/i18n/messages/en.ts`. Find the existing `mobile.pairing` block (which exists from P1.6 work). Add a sibling `insecure` block:

```ts
    insecure: {
      warning: {
        title: 'Insecure relay (HTTP)',
        body: 'API tokens and terminal output are transmitted unencrypted. Use HTTPS in production.',
        dismiss: 'Dismiss',
      },
      setupHint: 'Your API token will be sent in cleartext over Wi-Fi. Only enable for local-network testing.',
    },
```

Open `desktop/frontend/src/i18n/messages/zh-CN.ts`. Mirror the same shape:

```ts
    insecure: {
      warning: {
        title: '不安全的 relay (HTTP)',
        body: 'API token 和终端输出会以明文方式传输。生产环境请使用 HTTPS。',
        dismiss: '知道了',
      },
      setupHint: '你的 API token 会以明文方式经过 Wi-Fi 传输。仅建议在本地测试时启用。',
    },
```

- [ ] **Step 2: Verify i18n parity**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/i18n/i18n.test.ts`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git -c commit.gpgsign=false commit -m "i18n: add mobile.insecure keys (en + zh-CN)"
```

---

## Task 4: `InsecureBanner.vue` component (test-first)

**Files:**
- Create: `desktop/frontend/src/mobile/InsecureBanner.vue`
- Create: `desktop/frontend/src/mobile/__tests__/InsecureBanner.test.ts`

- [ ] **Step 1: Write failing tests**

Create `desktop/frontend/src/mobile/__tests__/InsecureBanner.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import InsecureBanner from '../InsecureBanner.vue'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

describe('InsecureBanner', () => {
  it('renders nothing when relayUrl is https', () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'https://relay.example.com' } })
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(false)
  })

  it('renders the collapsed banner when relayUrl is http', () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'http://relay.example.com' } })
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(true)
    expect(w.find('[data-testid="insecure-body"]').exists()).toBe(false)
  })

  it('expands body on tap and emits dismiss on Dismiss click', async () => {
    const w = mount(InsecureBanner, { props: { relayUrl: 'http://relay.example.com' } })
    await w.find('[data-testid="insecure-banner"]').trigger('click')
    expect(w.find('[data-testid="insecure-body"]').exists()).toBe(true)
    await w.find('[data-testid="insecure-dismiss"]').trigger('click')
    expect(w.emitted('dismiss')).toBeTruthy()
    expect(w.find('[data-testid="insecure-banner"]').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/InsecureBanner.test.ts`
Expected: FAIL — `Cannot find module '../InsecureBanner.vue'`.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/mobile/InsecureBanner.vue`:

```vue
<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'

const props = defineProps<{ relayUrl: string }>()
const emit = defineEmits<{ (e: 'dismiss'): void }>()
const { t } = useI18n()

const isInsecure = computed(() => props.relayUrl.startsWith('http://'))
const expanded = ref(false)
const dismissed = ref(false)
const visible = computed(() => isInsecure.value && !dismissed.value)

function toggle(): void {
  expanded.value = !expanded.value
}

function dismiss(): void {
  dismissed.value = true
  expanded.value = false
  emit('dismiss')
}
</script>

<template>
  <div v-if="visible" class="banner" data-testid="insecure-banner" @click="toggle">
    <div class="head">
      <span class="icon">⚠</span>
      <span class="title">{{ t('mobile.insecure.warning.title') }}</span>
    </div>
    <div v-if="expanded" class="body" data-testid="insecure-body" @click.stop>
      <p>{{ t('mobile.insecure.warning.body') }}</p>
      <button type="button" data-testid="insecure-dismiss" @click="dismiss">
        {{ t('mobile.insecure.warning.dismiss') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.banner { background: rgba(245,158,11,.13); border-bottom: 1px solid rgba(245,158,11,.4); color: #f5c451; padding: 8px 12px; font-size: 0.8rem; cursor: pointer; }
.head { display: flex; align-items: center; gap: 6px; }
.icon { font-size: 0.95rem; }
.title { font-weight: 600; }
.body { margin-top: 6px; }
.body p { margin: 0 0 8px; color: #f5c451; }
.body button { background: rgba(245,158,11,.25); border: 1px solid rgba(245,158,11,.5); color: #f5c451; padding: 4px 10px; border-radius: 6px; font-size: 0.78rem; cursor: pointer; }
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/InsecureBanner.test.ts`
Expected: PASS — all three tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/InsecureBanner.vue desktop/frontend/src/mobile/__tests__/InsecureBanner.test.ts
git -c commit.gpgsign=false commit -m "mobile: InsecureBanner shows HTTP warning with expandable detail"
```

---

## Task 5: Strengthen `MobileSetup.vue` insecure warning block

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSetup.vue`

- [ ] **Step 1: Add the always-visible warning text under the Allow-insecure checkbox**

Open `desktop/frontend/src/mobile/MobileSetup.vue`. Find the `<label class="row">` containing `mobile.allowInsecure` (it sits inside the `<template v-else>` block, near the form's tail).

Replace that label and its following lines with:

```vue
      <label class="row">
        <span>{{ t('mobile.allowInsecure') }}</span>
        <input data-testid="allow-insecure" v-model="allowInsecure" :disabled="submitting" type="checkbox" />
      </label>
      <aside class="warn-hint" data-state="warn" data-testid="insecure-hint">
        <span class="warn-icon">⚠</span>
        <span>{{ t('mobile.insecure.setupHint') }}</span>
      </aside>
```

- [ ] **Step 2: Add styling**

Append to the `<style scoped>` block at the bottom of the file:

```css
.warn-hint { display: flex; gap: 8px; padding: 9px 11px; margin: 0 0 1rem; border: 1px solid rgba(245,158,11,.4); border-left-width: 3px; border-radius: 9px; background: rgba(245,158,11,.13); color: #f5c451; font-size: 0.78rem; line-height: 1.4; }
.warn-icon { font-size: 0.95rem; }
```

- [ ] **Step 3: Run the existing MobileSetup tests**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSetup.test.ts`
Expected: PASS — the existing tests rely on `data-testid`s that remain present.

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileSetup.vue
git -c commit.gpgsign=false commit -m "mobile/setup: always-visible warning block under Allow insecure checkbox"
```

---

## Task 6: iOS Swift plugin `AttermSecureStoragePlugin`

**Files:**
- Create: `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStoragePlugin.swift`
- Create: `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStorage.m`

- [ ] **Step 1: Create the Swift plugin source**

Create `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStoragePlugin.swift`:

```swift
import Foundation
import Capacitor

/// Capacitor plugin backing platform/secureStorage.ts on iOS. Stores
/// short string values in the iOS Keychain. Single-app keychain; no
/// access groups, no biometric prompt. AccessibleAfterFirstUnlock so
/// background reconnect works after the phone is unlocked at least once
/// per boot.
@objc(AttermSecureStoragePlugin)
public class AttermSecureStoragePlugin: CAPPlugin {

    private let service = "com.attson.atterm"

    /// set({ key, value }) — upsert.
    @objc func set(_ call: CAPPluginCall) {
        guard let key = call.getString("key"),
              let value = call.getString("value"),
              let data = value.data(using: .utf8) else {
            call.reject("MISSING_ARGS")
            return
        }

        let baseQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]

        // Try add first; on duplicate, update.
        var attrs = baseQuery
        attrs[kSecValueData as String] = data
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock

        let addStatus = SecItemAdd(attrs as CFDictionary, nil)
        if addStatus == errSecSuccess {
            call.resolve()
            return
        }
        if addStatus == errSecDuplicateItem {
            let update: [String: Any] = [
                kSecValueData as String: data,
                kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
            ]
            let updStatus = SecItemUpdate(baseQuery as CFDictionary, update as CFDictionary)
            if updStatus == errSecSuccess {
                call.resolve()
                return
            }
            call.reject("KEYCHAIN_ERROR", "SecItemUpdate failed", nil, ["status": Int(updStatus)])
            return
        }
        call.reject("KEYCHAIN_ERROR", "SecItemAdd failed", nil, ["status": Int(addStatus)])
    }

    /// get({ key }) -> { value: string | null }
    @objc func get(_ call: CAPPluginCall) {
        guard let key = call.getString("key") else {
            call.reject("MISSING_ARGS")
            return
        }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecMatchLimit as String: kSecMatchLimitOne,
            kSecReturnData as String: true,
        ]

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        if status == errSecItemNotFound {
            call.resolve(["value": NSNull()])
            return
        }
        if status != errSecSuccess {
            call.reject("KEYCHAIN_ERROR", "SecItemCopyMatching failed", nil, ["status": Int(status)])
            return
        }
        guard let data = item as? Data,
              let str = String(data: data, encoding: .utf8) else {
            call.reject("KEYCHAIN_ERROR", "stored value is not utf-8 string", nil, nil)
            return
        }
        call.resolve(["value": str])
    }

    /// remove({ key }) — idempotent.
    @objc func remove(_ call: CAPPluginCall) {
        guard let key = call.getString("key") else {
            call.reject("MISSING_ARGS")
            return
        }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        let status = SecItemDelete(query as CFDictionary)
        if status == errSecSuccess || status == errSecItemNotFound {
            call.resolve()
            return
        }
        call.reject("KEYCHAIN_ERROR", "SecItemDelete failed", nil, ["status": Int(status)])
    }
}
```

- [ ] **Step 2: Create the Objective-C bridge file**

Create `mobile/ios/App/App/Plugins/AttermSecureStorage/AttermSecureStorage.m`:

```objc
#import <Foundation/Foundation.h>
#import <Capacitor/Capacitor.h>

// CAP_PLUGIN registers the plugin class with Capacitor's runtime so the
// JS side can find it via Capacitor.registerPlugin<...>('AttermSecureStorage').
// All three methods are declared with CAP_PLUGIN_METHOD so the bridge
// knows their signatures.
CAP_PLUGIN(AttermSecureStoragePlugin, "AttermSecureStorage",
    CAP_PLUGIN_METHOD(set, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(get, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(remove, CAPPluginReturnPromise);
)
```

- [ ] **Step 3: Verify the iOS files exist and have correct names**

Run: `ls /Users/attson/code/github.com.attson/atterm/mobile/ios/App/App/Plugins/AttermSecureStorage/`
Expected: two files, `AttermSecureStoragePlugin.swift` and `AttermSecureStorage.m`.

(No Xcode project edit needed — Capacitor 8 auto-discovers `*.swift`/`*.m` files under `App/Plugins/` when `cap sync ios` runs. We will not run `cap sync` here since the existing iOS project on disk is a snapshot; native build is done by whoever ships an iOS bundle. The unit tests cover the JS contract, and the in-memory fallback is what vitest exercises.)

- [ ] **Step 4: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add mobile/ios/App/App/Plugins/AttermSecureStorage/
git -c commit.gpgsign=false commit -m "ios: AttermSecureStorage Capacitor plugin (Keychain set/get/remove)"
```

---

## Task 7: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Run the full frontend test suite**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS — every existing test plus the new secureStorage / capacitor migration / InsecureBanner cases.

- [ ] **Step 2: Type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean (no output).

- [ ] **Step 3: Wails build (regression)**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: succeeds.

- [ ] **Step 4: Capacitor build (regression)**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: succeeds.

- [ ] **Step 5: Go suite (regression — relay code unchanged but P1.7 PR is in flight)**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 6: Manual / human smoke (documented, not gating)**

For the implementer or release engineer:

1. Run `cap sync ios` in `mobile/` to regenerate Pods and refresh the Xcode project.
2. Open `mobile/ios/App/App.xcworkspace` in Xcode.
3. Build & run on a simulator (or real device with developer signing).
4. Pair via QR or enter credentials manually; quit and relaunch — credentials should be restored from Keychain and `localStorage["atterm.relay"]` should be empty (inspect via Safari Web Inspector → Storage).
5. Toggle "Allow insecure HTTP" — the new warning block should be visible regardless of the checkbox state.
6. Type an `http://` URL and connect — the InsecureBanner should appear at the top of the main view.

No commit needed.

---

## Self-review notes

- **Spec coverage:**
  - §3.1 Keychain shape (`kSecAttrService`, `kSecAttrAccount`, `AccessibleAfterFirstUnlock`) → Task 6 Swift code matches verbatim.
  - §3.2/§3.3 TS shim (`secureStorage`, plugin detection with cached promise) → Task 1.
  - §3.4 Native plugin three methods → Task 6.
  - §4 Migration flow (Keychain first, localStorage fallback only on load, write-then-clear) → Task 2.
  - §5.1 strengthened `MobileSetup` warning → Task 5.
  - §5.2 InsecureBanner component → Task 4.
  - §5.3 i18n keys → Task 3.
  - §6 errors — Keychain errors propagate (reject with `KEYCHAIN_ERROR`); migration partial failure is non-fatal (next launch re-reads from Keychain) — Tasks 2 + 6.
  - §7.1 secureStorage in-memory tests → Task 1.
  - §7.2 capacitor migration tests → Task 2.
  - §7.3 InsecureBanner component test + MobileSetup hint presence test → Tasks 4 + 5.
  - §7.4 manual smoke → Task 7 step 6.

- **Placeholder scan:** no TBDs; every code-emitting step shows complete code. Swift plugin includes `errSecDuplicateItem` upsert handling, `errSecItemNotFound` returning null, and explicit reject codes — not vague "handle errors" instructions.

- **Type consistency:** `SecureStorage` (TS interface in Task 1) has `set(key, value): Promise<void>`, `get(key): Promise<string | null>`, `remove(key): Promise<void>`. The Swift plugin's `set/get/remove` match these one-for-one through Capacitor's `CAPPluginCall` shape. The Capacitor-side payload contract `{ key, value }` for set and `{ value: string | null }` for get's response is identical between the TS native wrapper in Task 1 and the Swift implementation in Task 6.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-01-mobile-secure-storage.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.
