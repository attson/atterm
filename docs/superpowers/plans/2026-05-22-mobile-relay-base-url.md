# Mobile Relay Base URL + Insecure HTTP/WS Toggle — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the Capacitor-wrapped iOS app (`mobile/`) connect to an arbitrary relay over `https`/`wss` (default) or `http`/`ws` with an explicit insecure opt-in, authenticated via a pasted API token. Browser web client keeps its existing same-origin cookie flow unchanged.

**Architecture:** Add a Capacitor-only branch in the two existing network chokepoints (`apiFetch` and `wsUrl`). Persist `{base, token, allowInsecure}` in `localStorage` via a new `relay-config.ts` module. Gate every page entry with `applyMobileEntryGuard` so the app redirects to a new `/setup.html` when unconfigured. Token auth via `Authorization: Bearer` for HTTP and `Sec-WebSocket-Protocol` for WS — same as desktop.

**Tech Stack:** Vue 3 + TypeScript + Naive UI + Vite 5 + Vitest (happy-dom). No new dependencies. Tests live in `web/tests/unit/**/*.test.ts`.

**Spec:** `docs/superpowers/specs/2026-05-22-mobile-relay-base-url-design.md`

---

## File Structure

**New files (web client):**
- `web/src/shared/api/relay-config.ts` — RelayConfig types, isMobileApp(), load/save/clear, validateRelayBase
- `web/src/shared/mobile-guard.ts` — applyMobileEntryGuard(page) entry-gate function
- `web/setup.html` — setup page entry HTML
- `web/src/setup/main.ts` — Vue mount entry for setup page
- `web/src/setup/App.vue` — setup form component
- `web/src/settings/tabs/Relay.vue` — settings tab to view/change relay config (mobile only)

**New tests:**
- `web/tests/unit/shared/api/relay-config.test.ts`
- `web/tests/unit/shared/mobile-guard.test.ts`
- `web/tests/unit/setup/App.test.ts`
- `web/tests/unit/settings/tabs/Relay.test.ts`

**Modified files:**
- `web/src/shared/api/client.ts` — apiFetch mobile branch + 401 redirect target switch
- `web/src/shared/ws/client-conn.ts` — wsUrl mobile branch + WS subprotocol arg + reconnect failure banner hook
- `web/src/main/main.ts`, `web/src/login/main.ts`, `web/src/signup/main.ts`, `web/src/settings/main.ts`, `web/src/admin/main.ts` — entry guard call
- `web/src/settings/App.vue` — render Relay tab when mobile
- `web/vite.config.ts` — add `setup` to rollupOptions.input
- `web/tests/unit/shared/api/client.test.ts` — add mobile branch coverage
- `mobile/README.md` — replace legacy "token panel" wording, add Capacitor onboarding flow + smoke checklist
- `AGENTS.md` — extend "何时改哪里" table with mobile relay config row

---

### Task 1: Create relay-config module (types + isMobileApp + storage)

**Files:**
- Create: `web/src/shared/api/relay-config.ts`
- Test: `web/tests/unit/shared/api/relay-config.test.ts`

- [ ] **Step 1: Write the failing test for storage + isMobileApp**

Create `web/tests/unit/shared/api/relay-config.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  isMobileApp,
  loadRelayConfig,
  saveRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'

describe('isMobileApp', () => {
  beforeEach(() => {
    __resetMobileDetectionCache()
    delete (globalThis as any).Capacitor
  })

  it('returns false when Capacitor global is absent', () => {
    expect(isMobileApp()).toBe(false)
  })

  it('returns false when Capacitor.isNativePlatform is missing', () => {
    ;(globalThis as any).Capacitor = {}
    expect(isMobileApp()).toBe(false)
  })

  it('returns false when isNativePlatform() returns false', () => {
    ;(globalThis as any).Capacitor = { isNativePlatform: () => false }
    expect(isMobileApp()).toBe(false)
  })

  it('returns true when isNativePlatform() returns true', () => {
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    expect(isMobileApp()).toBe(true)
  })

  it('caches the result after the first call', () => {
    const stub = vi.fn().mockReturnValue(true)
    ;(globalThis as any).Capacitor = { isNativePlatform: stub }
    isMobileApp()
    isMobileApp()
    isMobileApp()
    expect(stub).toHaveBeenCalledTimes(1)
  })
})

describe('relay config storage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('loadRelayConfig returns null when nothing is stored', () => {
    expect(loadRelayConfig()).toBeNull()
  })

  it('saveRelayConfig persists fields and loadRelayConfig reads them back', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_test', allowInsecure: false })
    expect(loadRelayConfig()).toEqual({
      base: 'https://r.example.com',
      token: 'atk_test',
      allowInsecure: false,
    })
  })

  it('loadRelayConfig returns null when stored JSON is malformed', () => {
    localStorage.setItem('atterm.relay', '{not json')
    expect(loadRelayConfig()).toBeNull()
  })

  it('loadRelayConfig returns null when stored config is missing required fields', () => {
    localStorage.setItem('atterm.relay', JSON.stringify({ base: 'https://r.example.com' }))
    expect(loadRelayConfig()).toBeNull()
  })

  it('clearRelayConfig removes the stored entry', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_x', allowInsecure: false })
    clearRelayConfig()
    expect(loadRelayConfig()).toBeNull()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/api/relay-config.test.ts
```
Expected: FAIL — module `@shared/api/relay-config` does not exist.

- [ ] **Step 3: Implement the module**

Create `web/src/shared/api/relay-config.ts`:

```ts
export interface RelayConfig {
  base: string
  token: string
  allowInsecure: boolean
}

const STORAGE_KEY = 'atterm.relay'

let cachedMobile: boolean | null = null

// Test-only escape hatch; not exported via index.
export function __resetMobileDetectionCache(): void {
  cachedMobile = null
}

export function isMobileApp(): boolean {
  if (cachedMobile !== null) return cachedMobile
  const cap = (globalThis as { Capacitor?: { isNativePlatform?: () => boolean } }).Capacitor
  cachedMobile = !!(cap && typeof cap.isNativePlatform === 'function' && cap.isNativePlatform())
  return cachedMobile
}

export function loadRelayConfig(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as Partial<RelayConfig>
    if (
      typeof parsed.base !== 'string' ||
      typeof parsed.token !== 'string' ||
      typeof parsed.allowInsecure !== 'boolean'
    ) {
      return null
    }
    return { base: parsed.base, token: parsed.token, allowInsecure: parsed.allowInsecure }
  } catch {
    return null
  }
}

export function saveRelayConfig(cfg: RelayConfig): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
}

export function clearRelayConfig(): void {
  localStorage.removeItem(STORAGE_KEY)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/api/relay-config.test.ts
```
Expected: PASS — all 10 cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/api/relay-config.ts web/tests/unit/shared/api/relay-config.test.ts
git commit -m "feat(web): add relay-config module for mobile storage + Capacitor detection"
```

---

### Task 2: Add validateRelayBase to relay-config

**Files:**
- Modify: `web/src/shared/api/relay-config.ts`
- Test: `web/tests/unit/shared/api/relay-config.test.ts`

- [ ] **Step 1: Append failing test block**

Append to `web/tests/unit/shared/api/relay-config.test.ts`:

```ts
import { validateRelayBase } from '@shared/api/relay-config'

describe('validateRelayBase', () => {
  it('accepts https with hostname', () => {
    expect(validateRelayBase('https://r.example.com', false)).toBeNull()
  })

  it('accepts https with port', () => {
    expect(validateRelayBase('https://r.example.com:8443', false)).toBeNull()
  })

  it('rejects wss scheme (use http(s) for base, ws scheme derives)', () => {
    expect(validateRelayBase('wss://r.example.com', false)).toMatch(/must start with http/i)
  })

  it('rejects ws scheme', () => {
    expect(validateRelayBase('ws://localhost:8080', true)).toMatch(/must start with http/i)
  })

  it('accepts http://localhost without insecure flag', () => {
    expect(validateRelayBase('http://localhost:8080', false)).toBeNull()
  })

  it('accepts http://127.0.0.1 without insecure flag', () => {
    expect(validateRelayBase('http://127.0.0.1:8080', false)).toBeNull()
  })

  it('accepts http://[::1] (IPv6 loopback) without insecure flag', () => {
    expect(validateRelayBase('http://[::1]:8080', false)).toBeNull()
  })

  it('rejects http to non-loopback host when allowInsecure is false', () => {
    const err = validateRelayBase('http://relay.example.com', false)
    expect(err).toMatch(/insecure/i)
  })

  it('accepts http to non-loopback host when allowInsecure is true', () => {
    expect(validateRelayBase('http://relay.example.com', true)).toBeNull()
  })

  it('rejects empty string', () => {
    expect(validateRelayBase('', false)).toMatch(/empty|required|missing/i)
  })

  it('rejects malformed URL', () => {
    expect(validateRelayBase('not a url', false)).toMatch(/invalid|malformed/i)
  })

  it('rejects URL with trailing path segment', () => {
    expect(validateRelayBase('https://r.example.com/api', false)).toMatch(/path/i)
  })

  it('accepts URL with trailing slash (treated as root path)', () => {
    expect(validateRelayBase('https://r.example.com/', false)).toBeNull()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/api/relay-config.test.ts
```
Expected: FAIL — `validateRelayBase` is not exported.

- [ ] **Step 3: Implement validateRelayBase**

Append to `web/src/shared/api/relay-config.ts`:

```ts
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
  if (u.pathname !== '' && u.pathname !== '/') {
    return 'relay URL must not contain a path segment'
  }
  if (u.protocol === 'http:' && !isLoopbackHost(u.hostname) && !allowInsecure) {
    return 'insecure http:// to non-loopback host requires the allowInsecure switch'
  }
  return null
}

function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase()
  if (h === 'localhost' || h === '127.0.0.1' || h === '::1') return true
  // URL parses [::1] hostname as "[::1]"; strip brackets when present.
  if (h.startsWith('[') && h.endsWith(']')) {
    const inner = h.slice(1, -1)
    if (inner === '::1') return true
  }
  return false
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/api/relay-config.test.ts
```
Expected: PASS — all cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/api/relay-config.ts web/tests/unit/shared/api/relay-config.test.ts
git commit -m "feat(web): add validateRelayBase mirroring desktop relay_security rules"
```

---

### Task 3: Add apiFetch mobile branch

**Files:**
- Modify: `web/src/shared/api/client.ts`
- Modify: `web/tests/unit/shared/api/client.test.ts`

- [ ] **Step 1: Write failing tests for the mobile branch**

Append to `web/tests/unit/shared/api/client.test.ts`:

```ts
import { saveRelayConfig, clearRelayConfig, __resetMobileDetectionCache } from '@shared/api/relay-config'

describe('apiFetch mobile branch', () => {
  beforeEach(() => {
    clearCsrfToken()
    clearRelayConfig()
    __resetMobileDetectionCache()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    vi.restoreAllMocks()
  })

  afterEach(() => {
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('prefixes path with configured base URL', async () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [url] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/me')
  })

  it('trims trailing slash from base when prefixing', async () => {
    saveRelayConfig({ base: 'https://r.example.com/', token: 'atk_t', allowInsecure: false })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    expect(fetchMock.mock.calls[0]![0]).toBe('https://r.example.com/api/me')
  })

  it('sends Authorization: Bearer <token>', async () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_abc', allowInsecure: false })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('Authorization')).toBe('Bearer atk_abc')
  })

  it('sets credentials: omit', async () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    expect((init as RequestInit).credentials).toBe('omit')
  })

  it('does not send X-CSRF-Token even on POST when CSRF cache is populated', async () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    setCsrfToken('cached-csrf')
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/foo', { method: 'POST', body: JSON.stringify({}) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('throws relay_not_configured when no config is stored', async () => {
    // do NOT saveRelayConfig
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiFetch('/api/me')).rejects.toMatchObject({
      status: 0,
      code: 'relay_not_configured',
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('apiFetch 401 mobile redirect', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    clearCsrfToken()
    clearRelayConfig()
    __resetMobileDetectionCache()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    vi.restoreAllMocks()
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock, pathname: '/' },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('redirects to /setup.html?reason=token_invalid on 401 in mobile mode', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(401, { error: 'unauthenticated' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiFetch('/api/me')).rejects.toMatchObject({ status: 401 })
    expect(replaceMock).toHaveBeenCalledWith('/setup.html?reason=token_invalid')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/api/client.test.ts
```
Expected: FAIL — Authorization header missing, URL not prefixed, 401 redirect target wrong.

- [ ] **Step 3: Implement the mobile branch in apiFetch**

Replace `web/src/shared/api/client.ts:50-103` with this new body (keep `safeNext`, CSRF cache, `ApiError`, `ApiResult` above lines 1-44 intact):

```ts
import { isMobileApp, loadRelayConfig } from './relay-config'

export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<ApiResult<T>> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)

  let url = path
  let credentials: RequestCredentials = 'same-origin'

  if (isMobileApp()) {
    const cfg = loadRelayConfig()
    if (!cfg) throw new ApiError(0, 'relay_not_configured', null)
    url = cfg.base.replace(/\/$/, '') + path
    headers.set('Authorization', `Bearer ${cfg.token}`)
    credentials = 'omit'
    if (!headers.has('Content-Type') && init.body !== undefined && method !== 'GET' && method !== 'HEAD') {
      headers.set('Content-Type', 'application/json')
    }
  } else {
    if (method !== 'GET' && method !== 'HEAD') {
      if (cachedCsrf) headers.set('X-CSRF-Token', cachedCsrf)
      if (!headers.has('Content-Type') && init.body !== undefined) {
        headers.set('Content-Type', 'application/json')
      }
    }
  }

  let res: Response
  try {
    res = await fetch(url, { ...init, headers, credentials })
  } catch {
    throw new ApiError(0, 'network_error', null)
  }

  if (res.status === 401) {
    if (isMobileApp()) {
      if (typeof location !== 'undefined') {
        location.replace('/setup.html?reason=token_invalid')
      }
    } else {
      const onAuthPage =
        typeof location !== 'undefined' &&
        (location.pathname === '/login.html' || location.pathname === '/signup.html')
      if (!onAuthPage && typeof location !== 'undefined') {
        const next = safeNext(location.pathname + location.search + location.hash)
        location.assign('/login.html?next=' + encodeURIComponent(next))
      }
    }
  }

  if (!res.ok) {
    let code = 'http_error'
    const ct = res.headers.get('Content-Type') || ''
    if (ct.includes('application/json')) {
      try {
        const j = (await res.clone().json()) as { error?: unknown }
        if (j && typeof j.error === 'string') code = j.error
      } catch {
        /* malformed JSON, keep http_error */
      }
    }
    throw new ApiError(res.status, code, res)
  }

  const ct = res.headers.get('Content-Type') || ''
  let data: T
  if (ct.includes('application/json')) {
    data = (await res.json()) as T
  } else {
    data = undefined as unknown as T
  }
  return { data, status: res.status, headers: res.headers }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/api/client.test.ts
```
Expected: PASS — both new describe blocks green AND all pre-existing tests still pass (browser branch unchanged).

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/api/client.ts web/tests/unit/shared/api/client.test.ts
git commit -m "feat(web): add Capacitor mobile branch to apiFetch (Bearer + base URL)"
```

---

### Task 4: Add wsUrl mobile branch + WS subprotocol

**Files:**
- Modify: `web/src/shared/ws/client-conn.ts`
- Create: `web/tests/unit/shared/ws/ws-url.test.ts`

The `wsUrl` function in `client-conn.ts` is currently file-private. We extract it so it can be unit-tested directly (it's a pure function; the surrounding class is too heavy to unit test as-is).

- [ ] **Step 1: Write failing test for the extracted wsUrl**

Create `web/tests/unit/shared/ws/ws-url.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { saveRelayConfig, clearRelayConfig, __resetMobileDetectionCache } from '@shared/api/relay-config'
import { wsUrl } from '@shared/ws/client-conn'

describe('wsUrl browser mode', () => {
  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    delete (globalThis as any).Capacitor
  })

  it('uses ws:// when location.protocol is http:', () => {
    Object.defineProperty(window, 'location', {
      value: { protocol: 'http:', host: 'example.com:5173' },
      writable: true,
    })
    expect(wsUrl('/client')).toBe('ws://example.com:5173/client')
  })

  it('uses wss:// when location.protocol is https:', () => {
    Object.defineProperty(window, 'location', {
      value: { protocol: 'https:', host: 'example.com' },
      writable: true,
    })
    expect(wsUrl('/client')).toBe('wss://example.com/client')
  })
})

describe('wsUrl mobile mode', () => {
  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
  })

  afterEach(() => {
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('derives ws:// from http:// base', () => {
    saveRelayConfig({ base: 'http://1.2.3.4:8080', token: 'atk_t', allowInsecure: true })
    expect(wsUrl('/client')).toBe('ws://1.2.3.4:8080/client')
  })

  it('derives wss:// from https:// base', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    expect(wsUrl('/client')).toBe('wss://r.example.com/client')
  })

  it('preserves the port from the base URL', () => {
    saveRelayConfig({ base: 'https://r.example.com:8443', token: 'atk_t', allowInsecure: false })
    expect(wsUrl('/client')).toBe('wss://r.example.com:8443/client')
  })

  it('throws relay_not_configured when no config is stored', () => {
    expect(() => wsUrl('/client')).toThrow(/relay_not_configured/)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/ws/ws-url.test.ts
```
Expected: FAIL — `wsUrl` not exported from `@shared/ws/client-conn`.

- [ ] **Step 3: Implement the mobile branch and export wsUrl**

In `web/src/shared/ws/client-conn.ts`:

(a) Add the import near the existing imports (top of file):

```ts
import { isMobileApp, loadRelayConfig } from '../api/relay-config'
```

(b) Replace the existing `wsUrl` (lines 197-201) with an exported version that handles mobile:

```ts
export function wsUrl(path: string): string {
  if (isMobileApp()) {
    const cfg = loadRelayConfig()
    if (!cfg) throw new Error('relay_not_configured')
    const u = new URL(cfg.base)
    const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${u.host}${path}`
  }
  if (typeof location === 'undefined') return `ws://localhost${path}`
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${path}`
}
```

(c) Update the `openWS` method (line 110-194) so the `WebSocket` constructor passes the token as a subprotocol in mobile mode. Replace lines 112-115:

```ts
    const url = wsUrl('/client')
    const cfg = isMobileApp() ? loadRelayConfig() : null
    const ws = cfg ? new WebSocket(url, [cfg.token]) : new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    this.ws = ws
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/ws/ws-url.test.ts
```
Expected: PASS — 7 cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/ws/client-conn.ts web/tests/unit/shared/ws/ws-url.test.ts
git commit -m "feat(web): wsUrl mobile branch + WS token subprotocol"
```

---

### Task 5: Add mobile-guard module

**Files:**
- Create: `web/src/shared/mobile-guard.ts`
- Test: `web/tests/unit/shared/mobile-guard.test.ts`

- [ ] **Step 1: Write the failing test (table-driven)**

Create `web/tests/unit/shared/mobile-guard.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  saveRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import { applyMobileEntryGuard, type EntryPage } from '@shared/mobile-guard'

interface Case {
  name: string
  mobile: boolean
  hasConfig: boolean
  page: EntryPage
  expectRedirect: string | null
  expectReturn: boolean
}

const CASES: Case[] = [
  { name: 'browser any page → no-op',          mobile: false, hasConfig: false, page: 'home',     expectRedirect: null,           expectReturn: false },
  { name: 'mobile no config + setup → render', mobile: true,  hasConfig: false, page: 'setup',    expectRedirect: null,           expectReturn: false },
  { name: 'mobile no config + home → setup',   mobile: true,  hasConfig: false, page: 'home',     expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile no config + login → setup',  mobile: true,  hasConfig: false, page: 'login',    expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile no config + admin → setup',  mobile: true,  hasConfig: false, page: 'admin',    expectRedirect: '/setup.html',  expectReturn: true  },
  { name: 'mobile + config + setup → home',    mobile: true,  hasConfig: true,  page: 'setup',    expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + login → home',    mobile: true,  hasConfig: true,  page: 'login',    expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + signup → home',   mobile: true,  hasConfig: true,  page: 'signup',   expectRedirect: '/',            expectReturn: true  },
  { name: 'mobile + config + home → render',   mobile: true,  hasConfig: true,  page: 'home',     expectRedirect: null,           expectReturn: false },
  { name: 'mobile + config + settings → render', mobile: true, hasConfig: true, page: 'settings', expectRedirect: null,           expectReturn: false },
  { name: 'mobile + config + admin → render',  mobile: true,  hasConfig: true,  page: 'admin',    expectRedirect: null,           expectReturn: false },
]

describe('applyMobileEntryGuard decision table', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  for (const c of CASES) {
    it(c.name, () => {
      if (c.mobile) (globalThis as any).Capacitor = { isNativePlatform: () => true }
      if (c.hasConfig) saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })

      const returned = applyMobileEntryGuard(c.page)

      expect(returned).toBe(c.expectReturn)
      if (c.expectRedirect === null) {
        expect(replaceMock).not.toHaveBeenCalled()
      } else {
        expect(replaceMock).toHaveBeenCalledWith(c.expectRedirect)
      }
    })
  }
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/mobile-guard.test.ts
```
Expected: FAIL — module `@shared/mobile-guard` does not exist.

- [ ] **Step 3: Implement the guard**

Create `web/src/shared/mobile-guard.ts`:

```ts
import { isMobileApp, loadRelayConfig } from './api/relay-config'

export type EntryPage = 'home' | 'login' | 'signup' | 'settings' | 'admin' | 'setup'

// applyMobileEntryGuard inspects the Capacitor environment and stored
// relay config, then issues a redirect when the current page would be
// unusable. Returns true when a redirect was issued — callers should
// `return` and skip mounting their Vue app.
export function applyMobileEntryGuard(page: EntryPage): boolean {
  if (!isMobileApp()) return false

  const hasConfig = loadRelayConfig() !== null

  if (!hasConfig) {
    if (page === 'setup') return false
    location.replace('/setup.html')
    return true
  }

  // hasConfig is true here.
  if (page === 'setup' || page === 'login' || page === 'signup') {
    location.replace('/')
    return true
  }

  return false
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/mobile-guard.test.ts
```
Expected: PASS — all 11 cases green.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/mobile-guard.ts web/tests/unit/shared/mobile-guard.test.ts
git commit -m "feat(web): add applyMobileEntryGuard for Capacitor page routing"
```

---

### Task 6: Wire entry guards into all 5 main.ts files

**Files:**
- Modify: `web/src/main/main.ts`, `web/src/login/main.ts`, `web/src/signup/main.ts`, `web/src/settings/main.ts`, `web/src/admin/main.ts`

No new tests — the guard logic is already covered by Task 5. We're only wiring the call.

- [ ] **Step 1: Update `web/src/main/main.ts`**

Replace contents with:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('home')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 2: Update `web/src/login/main.ts`**

Replace contents with:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('login')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 3: Update `web/src/signup/main.ts`**

Replace contents with:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('signup')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 4: Update `web/src/settings/main.ts`**

Replace contents with:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('settings')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 5: Update `web/src/admin/main.ts`**

Replace contents with:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('admin')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 6: Run the full test suite to ensure nothing regressed**

```bash
cd web && npm test
```
Expected: PASS.

- [ ] **Step 7: Type-check the web build**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 8: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/main/main.ts web/src/login/main.ts web/src/signup/main.ts web/src/settings/main.ts web/src/admin/main.ts
git commit -m "feat(web): wire applyMobileEntryGuard into all page entries"
```

---

### Task 7: Add setup page (HTML + main.ts + Vue component)

**Files:**
- Create: `web/setup.html`
- Create: `web/src/setup/main.ts`
- Create: `web/src/setup/App.vue`
- Modify: `web/vite.config.ts`
- Test: `web/tests/unit/setup/App.test.ts`

- [ ] **Step 1: Add setup.html to vite inputs**

Modify `web/vite.config.ts` lines 68-76 (the `rollupOptions.input` map). Add a `setup` entry:

```ts
    rollupOptions: {
      input: {
        index:    fileURLToPath(new URL('./index.html',           import.meta.url)),
        login:    fileURLToPath(new URL('./login.html',           import.meta.url)),
        signup:   fileURLToPath(new URL('./signup.html',          import.meta.url)),
        settings: fileURLToPath(new URL('./settings.html',        import.meta.url)),
        admin:    fileURLToPath(new URL('./admin/index.html',     import.meta.url)),
        setup:    fileURLToPath(new URL('./setup.html',           import.meta.url)),
      },
    },
```

- [ ] **Step 2: Create `web/setup.html`**

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
  <meta name="page" content="setup" />
  <meta name="theme-color" content="#0b1020" />
  <meta name="apple-mobile-web-app-capable" content="yes" />
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
  <meta name="apple-mobile-web-app-title" content="AT Term" />
  <title>AT Term · setup</title>
  <link rel="icon" href="/icon.svg" type="image/svg+xml" />
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/setup/main.ts"></script>
</body>
</html>
```

- [ ] **Step 3: Create `web/src/setup/main.ts`**

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'

if (!applyMobileEntryGuard('setup')) {
  createApp(App).mount('#app')
}
```

- [ ] **Step 4: Write failing component test**

Create `web/tests/unit/setup/App.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import {
  loadRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import App from '@/setup/App.vue'

function makeResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('setup/App.vue', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock, search: '', pathname: '/setup.html' },
      writable: true,
    })
    vi.restoreAllMocks()
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('renders the three required inputs', () => {
    const wrapper = mount(App)
    expect(wrapper.find('input[name="relay-base"]').exists()).toBe(true)
    expect(wrapper.find('input[name="relay-token"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="connect"]').exists()).toBe(true)
  })

  it('shows an inline error when base is invalid', async () => {
    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('not a url')
    await wrapper.find('input[name="relay-token"]').setValue('atk_test')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/invalid|malformed/i)
  })

  it('shows an inline error when http to non-loopback without insecure switch', async () => {
    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('http://relay.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_test')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/insecure/i)
  })

  it('on successful probe saves config and replaces location to /', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u1', email: 'e@x' }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_good')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalled()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/me')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_good')
    expect((init as RequestInit).credentials).toBe('omit')

    expect(loadRelayConfig()).toEqual({
      base: 'https://r.example.com',
      token: 'atk_good',
      allowInsecure: false,
    })
    expect(replaceMock).toHaveBeenCalledWith('/')
  })

  it('shows token-invalid error on 401', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(401, { error: 'unauthenticated' }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_bad')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/token|invalid/i)
    expect(loadRelayConfig()).toBeNull()
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('shows network error on fetch reject', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_t')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/connect|network|relay/i)
    expect(loadRelayConfig()).toBeNull()
  })

  it('shows origin-rejected error on 403', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(403, { error: 'origin_rejected' })))

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_t')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/origin|ATTERM_ORIGINS|capacitor/i)
  })

  it('shows token-invalid banner when ?reason=token_invalid in URL', () => {
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock, search: '?reason=token_invalid', pathname: '/setup.html' },
      writable: true,
    })
    const wrapper = mount(App)
    expect(wrapper.text()).toMatch(/token.*invalid|sign.*in.*again/i)
  })
})
```

- [ ] **Step 5: Run the test to verify it fails**

```bash
cd web && npx vitest run tests/unit/setup/App.test.ts
```
Expected: FAIL — `@/setup/App.vue` does not exist.

- [ ] **Step 6: Implement `web/src/setup/App.vue`**

```vue
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NCard,
  NInput,
  NSwitch,
  NButton,
  NAlert,
  NSpace,
  NForm,
  NFormItem,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import {
  saveRelayConfig,
  validateRelayBase,
  type RelayConfig,
} from '@shared/api/relay-config'

const base = ref('https://')
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)
const reasonBanner = ref<string | null>(null)

onMounted(() => {
  const params = new URLSearchParams(location.search)
  if (params.get('reason') === 'token_invalid') {
    reasonBanner.value = 'Your API token is no longer valid. Please paste a fresh token to sign in again.'
  }
})

const baseError = computed(() => {
  if (!base.value || base.value === 'https://') return null
  return validateRelayBase(base.value, allowInsecure.value)
})

async function onConnect(): Promise<void> {
  error.value = null
  const v = validateRelayBase(base.value, allowInsecure.value)
  if (v) {
    error.value = v
    return
  }
  if (!token.value.trim()) {
    error.value = 'API token is required'
    return
  }
  submitting.value = true
  try {
    const url = base.value.replace(/\/$/, '') + '/api/me'
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token.value.trim()}` },
      credentials: 'omit',
    })
    if (res.status === 401) {
      error.value = 'API token is invalid. Generate a new one from Settings → API Tokens on the relay web UI.'
      return
    }
    if (res.status === 403) {
      error.value =
        'Relay rejected the origin. Make sure the relay was started with ATTERM_ORIGINS containing capacitor://localhost.'
      return
    }
    if (!res.ok) {
      error.value = `Relay returned HTTP ${res.status}. Check the URL and try again.`
      return
    }
    const cfg: RelayConfig = {
      base: base.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allowInsecure: allowInsecure.value,
    }
    saveRelayConfig(cfg)
    location.replace('/')
  } catch (e) {
    error.value = `Cannot reach relay: ${e instanceof Error ? e.message : String(e)}`
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <main class="setup-page">
        <n-card title="Connect to relay" class="setup-card">
          <n-alert
            v-if="reasonBanner"
            type="warning"
            :show-icon="true"
            style="margin-bottom: 1rem;"
          >{{ reasonBanner }}</n-alert>

          <n-form @submit.prevent="onConnect">
            <n-form-item label="Relay URL" :feedback="baseError ?? ''" :validation-status="baseError ? 'error' : undefined">
              <n-input
                v-model:value="base"
                placeholder="https://relay.example.com"
                :input-props="{ name: 'relay-base', autocomplete: 'off' }"
                :disabled="submitting"
              />
            </n-form-item>

            <n-form-item label="API token">
              <n-input
                v-model:value="token"
                type="password"
                show-password-on="click"
                placeholder="atk_…"
                :input-props="{ name: 'relay-token', autocomplete: 'off' }"
                :disabled="submitting"
              />
            </n-form-item>

            <n-form-item label="Allow insecure HTTP/WS (non-loopback)">
              <n-switch
                v-model:value="allowInsecure"
                :disabled="submitting"
              />
            </n-form-item>

            <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 1rem;">
              {{ error }}
            </n-alert>

            <n-space justify="end">
              <n-button
                type="primary"
                :loading="submitting"
                :disabled="submitting"
                data-testid="connect"
                @click="onConnect"
              >Connect</n-button>
            </n-space>
          </n-form>
        </n-card>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.setup-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem 1rem;
  background: var(--bg);
  color: var(--fg);
}
.setup-card {
  width: 100%;
  max-width: 480px;
}
</style>
```

- [ ] **Step 7: Run the test to verify it passes**

```bash
cd web && npx vitest run tests/unit/setup/App.test.ts
```
Expected: PASS — 7 cases green.

- [ ] **Step 8: Confirm `npm run build` succeeds (Vite picks up setup entry)**

```bash
cd web && npm run build
```
Expected: build completes, `dist/setup.html` and `dist/assets/setup-*.js` exist.

- [ ] **Step 9: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/vite.config.ts web/setup.html web/src/setup/main.ts web/src/setup/App.vue web/tests/unit/setup/App.test.ts
git commit -m "feat(web): add mobile setup page for relay base URL + API token"
```

---

### Task 8: Add Relay tab to Settings (mobile only)

**Files:**
- Create: `web/src/settings/tabs/Relay.vue`
- Modify: `web/src/settings/App.vue`
- Test: `web/tests/unit/settings/tabs/Relay.test.ts`

- [ ] **Step 1: Write failing component test**

Create `web/tests/unit/settings/tabs/Relay.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import {
  saveRelayConfig,
  loadRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import Relay from '@/settings/tabs/Relay.vue'

function makeResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('settings/tabs/Relay.vue', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_existing', allowInsecure: false })
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock },
      writable: true,
    })
    vi.restoreAllMocks()
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('pre-fills inputs from stored config', () => {
    const wrapper = mount(Relay)
    const base = wrapper.find('input[name="relay-base"]').element as HTMLInputElement
    expect(base.value).toBe('https://r.example.com')
  })

  it('save validates + probes + persists', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u', email: 'e' })))
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toMatchObject({ base: 'https://other.example.com', token: 'atk_new' })
  })

  it('disconnect clears config and redirects to setup', async () => {
    const wrapper = mount(Relay)
    await wrapper.find('[data-testid="disconnect"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toBeNull()
    expect(replaceMock).toHaveBeenCalledWith('/setup.html')
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd web && npx vitest run tests/unit/settings/tabs/Relay.test.ts
```
Expected: FAIL — `@/settings/tabs/Relay.vue` does not exist.

- [ ] **Step 3: Implement `web/src/settings/tabs/Relay.vue`**

```vue
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NCard,
  NInput,
  NSwitch,
  NButton,
  NAlert,
  NSpace,
  NForm,
  NFormItem,
} from 'naive-ui'
import {
  loadRelayConfig,
  saveRelayConfig,
  clearRelayConfig,
  validateRelayBase,
} from '@shared/api/relay-config'

const base = ref('')
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const ok = ref<string | null>(null)
const submitting = ref(false)

onMounted(() => {
  const cfg = loadRelayConfig()
  if (cfg) {
    base.value = cfg.base
    token.value = cfg.token
    allowInsecure.value = cfg.allowInsecure
  }
})

const baseError = computed(() => {
  if (!base.value) return null
  return validateRelayBase(base.value, allowInsecure.value)
})

async function onSave(): Promise<void> {
  error.value = null
  ok.value = null
  const v = validateRelayBase(base.value, allowInsecure.value)
  if (v) {
    error.value = v
    return
  }
  if (!token.value.trim()) {
    error.value = 'API token is required'
    return
  }
  submitting.value = true
  try {
    const url = base.value.replace(/\/$/, '') + '/api/me'
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token.value.trim()}` },
      credentials: 'omit',
    })
    if (res.status === 401) {
      error.value = 'API token is invalid.'
      return
    }
    if (!res.ok) {
      error.value = `Relay returned HTTP ${res.status}.`
      return
    }
    saveRelayConfig({
      base: base.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allowInsecure: allowInsecure.value,
    })
    ok.value = 'Saved.'
  } catch (e) {
    error.value = `Cannot reach relay: ${e instanceof Error ? e.message : String(e)}`
  } finally {
    submitting.value = false
  }
}

function onDisconnect(): void {
  clearRelayConfig()
  location.replace('/setup.html')
}
</script>

<template>
  <n-card title="Relay">
    <n-form @submit.prevent="onSave">
      <n-form-item label="Relay URL" :feedback="baseError ?? ''" :validation-status="baseError ? 'error' : undefined">
        <n-input
          v-model:value="base"
          placeholder="https://relay.example.com"
          :input-props="{ name: 'relay-base', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item label="API token">
        <n-input
          v-model:value="token"
          type="password"
          show-password-on="click"
          placeholder="atk_…"
          :input-props="{ name: 'relay-token', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item label="Allow insecure HTTP/WS (non-loopback)">
        <n-switch v-model:value="allowInsecure" :disabled="submitting" />
      </n-form-item>

      <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 1rem;">{{ error }}</n-alert>
      <n-alert v-if="ok" type="success" :show-icon="true" style="margin-bottom: 1rem;">{{ ok }}</n-alert>

      <n-space justify="space-between">
        <n-button data-testid="disconnect" @click="onDisconnect">Disconnect</n-button>
        <n-button type="primary" :loading="submitting" :disabled="submitting" data-testid="save" @click="onSave">Save</n-button>
      </n-space>
    </n-form>
  </n-card>
</template>
```

- [ ] **Step 4: Wire the Relay tab into `web/src/settings/App.vue` (mobile-only)**

Replace the file with:

```vue
<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NTabs,
  NTabPane,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import Topbar from '@shared/components/Topbar.vue'
import { isMobileApp } from '@shared/api/relay-config'
import ApiTokens from './tabs/ApiTokens.vue'
import ChangePassword from './tabs/ChangePassword.vue'
import Sessions from './tabs/Sessions.vue'
import Notifications from './tabs/Notifications.vue'
import DangerZone from './tabs/DangerZone.vue'
import Relay from './tabs/Relay.vue'

const mobile = isMobileApp()

const TAB_NAMES = mobile
  ? (['relay'] as const)
  : (['api-tokens', 'change-password', 'sessions', 'notifications', 'danger'] as const)
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return (TAB_NAMES as readonly string[]).includes(h) ? (h as TabName) : TAB_NAMES[0]
}

const activeTab = ref<TabName>(nameFromHash())

function onHashChange() {
  activeTab.value = nameFromHash()
}

onMounted(() => window.addEventListener('hashchange', onHashChange))
onUnmounted(() => window.removeEventListener('hashchange', onHashChange))

function onTabChange(name: string) {
  if (!(TAB_NAMES as readonly string[]).includes(name)) return
  if (location.hash.replace(/^#/, '') !== name) {
    location.hash = '#' + name
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <Topbar active="settings" />
      <main class="settings-page">
        <n-tabs
          :value="activeTab"
          type="line"
          animated
          @update:value="onTabChange"
        >
          <template v-if="mobile">
            <n-tab-pane name="relay" tab="Relay">
              <Relay />
            </n-tab-pane>
          </template>
          <template v-else>
            <n-tab-pane name="api-tokens" tab="API Tokens">
              <ApiTokens />
            </n-tab-pane>
            <n-tab-pane name="change-password" tab="Change Password">
              <ChangePassword />
            </n-tab-pane>
            <n-tab-pane name="sessions" tab="Signed-in devices">
              <Sessions />
            </n-tab-pane>
            <n-tab-pane name="notifications" tab="Notifications">
              <Notifications />
            </n-tab-pane>
            <n-tab-pane name="danger" tab="Danger zone">
              <DangerZone />
            </n-tab-pane>
          </template>
        </n-tabs>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.settings-page {
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  color: var(--fg);
  min-height: calc(100vh - 80px);
}
</style>
```

- [ ] **Step 5: Run all settings tests to confirm nothing regressed and the new tab test passes**

```bash
cd web && npx vitest run tests/unit/settings tests/unit/settings/tabs/Relay.test.ts
```
Expected: PASS — existing tab tests still green, new Relay test green.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/settings/tabs/Relay.vue web/src/settings/App.vue web/tests/unit/settings/tabs/Relay.test.ts
git commit -m "feat(web): add mobile Relay settings tab"
```

---

### Task 9: WS reconnect failure banner

The reconnect logic in `client-conn.ts:180-189` keeps retrying silently. The spec calls for a visible banner after 5 consecutive failures OR 30 s elapsed since last success, linking to `/setup.html`.

**Files:**
- Modify: `web/src/shared/ws/client-conn.ts`
- Modify: `web/src/main/components/TerminalView.vue` (where SessionConnection status is currently consumed)
- Test: `web/tests/unit/shared/ws/reconnect-banner.test.ts`

- [ ] **Step 1: Write failing test for the threshold logic**

Create `web/tests/unit/shared/ws/reconnect-banner.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { shouldShowReconnectBanner } from '@shared/ws/client-conn'

describe('shouldShowReconnectBanner', () => {
  it('returns false when there has been no failure yet', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 0, firstFailureAt: null, now: 1000 })).toBe(false)
  })

  it('returns false with 4 failures and only 10s elapsed', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 4, firstFailureAt: 1000, now: 11_000 })).toBe(false)
  })

  it('returns true at 5 failures even if only 1s elapsed', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 5, firstFailureAt: 1000, now: 2000 })).toBe(true)
  })

  it('returns true after 30s elapsed even with only 2 failures', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 2, firstFailureAt: 1000, now: 31_001 })).toBe(true)
  })

  it('returns false exactly at threshold-minus-one of both axes', () => {
    expect(shouldShowReconnectBanner({ consecutiveFailures: 4, firstFailureAt: 1000, now: 30_999 })).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd web && npx vitest run tests/unit/shared/ws/reconnect-banner.test.ts
```
Expected: FAIL — `shouldShowReconnectBanner` not exported.

- [ ] **Step 3: Implement and wire the threshold**

In `web/src/shared/ws/client-conn.ts`, add at the bottom (after existing exports):

```ts
export interface ReconnectStatus {
  consecutiveFailures: number
  firstFailureAt: number | null  // epoch ms of first failure in current streak
  now: number
}

const RECONNECT_BANNER_MIN_FAILURES = 5
const RECONNECT_BANNER_MIN_ELAPSED_MS = 30_000

export function shouldShowReconnectBanner(s: ReconnectStatus): boolean {
  if (s.consecutiveFailures >= RECONNECT_BANNER_MIN_FAILURES) return true
  if (s.firstFailureAt !== null && s.now - s.firstFailureAt >= RECONNECT_BANNER_MIN_ELAPSED_MS) {
    return s.consecutiveFailures > 0
  }
  return false
}
```

Then update the class to track these counters and expose the boolean via the existing `onStatus` channel by adding a new status `'lost'`. Modify the `ws.onopen` callback (around line 118) to reset:

```ts
    ws.onopen = () => {
      this.reconnectAttempts = 0
      this.consecutiveFailures = 0
      this.firstFailureAt = null
      this.handlers.onStatus?.('attached')
      // …existing body…
    }
```

And modify `ws.onclose` (around line 180):

```ts
    ws.onclose = () => {
      this.ws = null
      if (this.detached) return
      this.consecutiveFailures += 1
      if (this.firstFailureAt === null) this.firstFailureAt = Date.now()
      const lost = shouldShowReconnectBanner({
        consecutiveFailures: this.consecutiveFailures,
        firstFailureAt: this.firstFailureAt,
        now: Date.now(),
      })
      this.handlers.onStatus?.(lost ? 'lost' : 'reconnecting')
      const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_INITIAL_MS * Math.pow(2, this.reconnectAttempts++))
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = null
        this.openWS()
      }, delay)
    }
```

Declare the new private fields near the existing `private reconnectAttempts = 0` field (around line 35-45):

```ts
  private consecutiveFailures = 0
  private firstFailureAt: number | null = null
```

Extend the `SessionStatus` union at `web/src/shared/ws/client-conn.ts:20`. Change:

```ts
export type SessionStatus = 'connecting' | 'attached' | 'reconnecting' | 'ended'
```

to:

```ts
export type SessionStatus = 'connecting' | 'attached' | 'reconnecting' | 'ended' | 'lost'
```

- [ ] **Step 4: Render the banner in `web/src/main/components/TerminalView.vue`**

Add the Naive UI `NAlert` import at the top of `<script setup lang="ts">` (alongside existing imports):

```ts
import { NAlert } from 'naive-ui'
```

Replace the `<template>` block (lines 140-157) with:

```vue
<template>
  <section class="term-view">
    <n-alert
      v-if="status === 'lost'"
      type="warning"
      :show-icon="true"
      class="lost-banner"
      data-testid="lost-banner"
    >
      Cannot reach relay.
      <a href="/setup.html">Tap to change configuration.</a>
    </n-alert>
    <div class="term-wrap">
      <div ref="termContainer" class="term"></div>
      <div
        v-if="replay"
        class="replay-overlay"
        data-testid="replay-progress"
      >
        <div class="replay-text">loading history… {{ pct() }}%</div>
        <div class="replay-track" aria-hidden="true">
          <div class="replay-fill" :style="{ width: pct() + '%' }"></div>
        </div>
      </div>
    </div>
    <p class="status-line" data-testid="status-line">{{ status }}</p>
  </section>
</template>
```

Add the banner CSS at the end of the `<style scoped>` block:

```css
.lost-banner {
  margin: 0;
  border-radius: 0;
}
```

(The `status` ref already updates via `onStatus`; no other wiring needed.)

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd web && npx vitest run tests/unit/shared/ws/reconnect-banner.test.ts tests/unit/shared/ws/ws-url.test.ts
```
Expected: PASS — banner threshold tests green, no regression in wsUrl tests.

- [ ] **Step 6: Type-check the web build**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/shared/ws/client-conn.ts web/src/main/components/TerminalView.vue web/tests/unit/shared/ws/reconnect-banner.test.ts
git commit -m "feat(web): show reconnect banner after 5 failures or 30s lost"
```

---

### Task 10: Sync to mobile and verify in iOS Simulator

**Files:**
- No code changes; verifies end-to-end.

- [ ] **Step 1: Run the existing sync-web pipeline**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npm run sync-web
```
Expected: web builds, `mobile/www/setup.html` and `mobile/www/assets/setup-*.js` present.

- [ ] **Step 2: Verify www contents include setup page**

```bash
ls /Users/attson/code/github.com.attson/atterm/mobile/www/setup.html
grep -l "setup" /Users/attson/code/github.com.attson/atterm/mobile/www/assets/*.js | head
```
Expected: setup.html exists; setup chunk present in assets.

- [ ] **Step 3: Launch a local relay with the Capacitor origin allowed**

In a separate terminal (the user runs this — do not background):

```bash
cd /Users/attson/code/github.com.attson/atterm
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure
```

- [ ] **Step 4: Mint an API token in the relay**

Use a desktop browser:
1. Open `http://127.0.0.1:8080/login.html`, sign in as `admin@example.com`.
2. Settings → API Tokens → Create. Copy the `atk_…` plaintext (it's only shown once).

- [ ] **Step 5: Build + run the iOS app**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npm run ios:open
```

In Xcode: Product → Clean Build Folder, then Run (iPhone 17 Pro simulator).

- [ ] **Step 6: Verify cold-start onboarding**

Expected at launch:
- Setup page renders (NOT login).
- Fill Relay URL: `http://127.0.0.1:8080`
- Toggle "Allow insecure HTTP/WS" ON (since it's plain http to a non-loopback-from-simulator IP).
- Paste the `atk_…` token.
- Tap Connect.
- After ~1 s, app navigates to the home page and shows the session list.

(Note: in the iOS Simulator, `127.0.0.1` actually points at the simulator itself — to reach the host Mac use the host's LAN IP or `localhost` mapping. The simulator's `host.docker.internal` analogue is the host's LAN IP. The exact URL depends on the host network; substitute whatever the relay binds to that's reachable from the simulator.)

- [ ] **Step 7: Verify session open + send/receive**

- Tap a session entry.
- Send a key (any letter).
- Expected: WS connects (no banner). Output stream displays.

- [ ] **Step 8: Verify token-invalid flow**

- Revoke the token on the desktop browser (Settings → API Tokens → Trash).
- In the simulator, tap a session entry or refresh.
- Expected: app redirects to `/setup.html?reason=token_invalid`, banner says token invalid, base is preserved, token field is empty.

- [ ] **Step 9: Verify disconnect from Settings**

- Re-paste a fresh token; reconnect.
- Open Settings → Relay tab → Disconnect.
- Expected: setup page appears again with empty fields.

- [ ] **Step 10: No commit (manual smoke; nothing changed)**

If anything fails, file a follow-up — do not amend earlier commits.

---

### Task 11: Update mobile/README.md

**Files:**
- Modify: `mobile/README.md`

- [ ] **Step 1: Replace the "Relay configuration" section**

Open `mobile/README.md`. Replace the entire `## Relay configuration` section (lines roughly 16-43 in the current file) with:

```markdown
## Relay configuration

The bundled app cannot make same-origin `/api/sessions` calls. On first launch, the app opens a **setup screen** asking for:

- **Relay URL** — e.g. `https://relay.example.com` (or `http://1.2.3.4:8080` for IP testing)
- **API token** — paste an `atk_…` token. Generate one on a desktop browser via the relay's `/settings.html#api-tokens` page (Settings → API Tokens → Create).
- **Allow insecure HTTP/WS** — turn on only for IP/port testing against a plain HTTP relay. Production must use HTTPS.

The relay must allow the WebView origin. Start the relay with:

```bash
ATTERM_ORIGINS=capacitor://localhost \
ATTERM_BOOTSTRAP_ADMIN_EMAIL='admin@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Strong-Bootstrap-Pass-2026!' \
atterm-relay --addr :8080 --web web/dist
```

To change the relay later: in the app, Settings → Relay → edit fields or Disconnect.

## Mobile smoke checklist

After any change to `web/src/setup/`, `web/src/shared/api/relay-config.ts`, `web/src/shared/mobile-guard.ts`, `web/src/shared/api/client.ts`, or `web/src/shared/ws/client-conn.ts`, run through this in the iOS simulator before merging:

1. Cold start with no config → setup screen renders.
2. Invalid token → inline "API token is invalid" error; not redirected away.
3. Valid token → home screen renders; session list loads.
4. Open session → WS connects; characters echo.
5. Revoke token externally → next API call redirects to `/setup.html?reason=token_invalid`.
6. Settings → Relay → Disconnect → setup screen with empty fields.
```

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add mobile/README.md
git commit -m "docs(mobile): update README for new setup-screen onboarding flow"
```

---

### Task 12: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Add a row to the "何时改哪里" table**

Open `AGENTS.md`. Find the section starting with `## 何时改哪里` and the markdown table that follows. Add this row at the end of the table (before the next heading):

```markdown
| 改移动 app relay 配置 | `web/src/setup/` + `web/src/shared/api/relay-config.ts` + `web/src/shared/mobile-guard.ts` + `web/src/settings/tabs/Relay.vue`；`apiFetch`/`wsUrl` 的 mobile 分支在 `web/src/shared/api/client.ts` 和 `web/src/shared/ws/client-conn.ts` |
```

- [ ] **Step 2: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add AGENTS.md
git commit -m "docs: AGENTS.md routing for mobile relay config touch-points"
```

---

## Final verification

After Task 12:

- [ ] **Run the full test suite once more end-to-end**

```bash
cd web && npm test
```
Expected: all green.

- [ ] **Type-check**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: zero errors.

- [ ] **Build mobile www**

```bash
cd /Users/attson/code/github.com.attson/atterm/mobile && npm run sync-web
```
Expected: build + sync clean.

- [ ] **Confirm git log shows 11 focused commits**

```bash
git log --oneline -15
```
Expected: One commit per task (Tasks 1–9, 11, 12 = 11 commits; Task 10 is a manual smoke with no commit).
