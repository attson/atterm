# Mobile Email/Password Login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mobile setup screen's "URL + API token" manual entry with "URL + email + password" login that calls `/api/auth/login`; persist the returned session token + email + password to iOS Keychain so re-login is a single tap. QR pairing path unchanged.

**Architecture:** Add optional `login` / `logout` / `loadSavedPassword` methods to `RelayBridge`; Capacitor implements them with `fetch` + Keychain writes (new `PASSWORD_KEY` for the password, existing `STORAGE_KEY` for the rest of `RelayConfig`). Wails leaves them undefined (desktop already has email/password via `LoginRemoteRelay`). `MobileSetup.vue` swaps the token field for email + password (with show/hide toggle, matching `SettingsRelay.vue`'s pattern); `MobileApp.onLogout()` calls `platform.relay.logout()` for a hard server-side logout before navigating back to setup.

**Tech Stack:** Vue 3 (Composition API + `<script setup>`), TypeScript, Vitest + @vue/test-utils + happy-dom, Capacitor 5, `@capacitor/core` for `registerPlugin`, custom `AttermSecureStorage` plugin (iOS Keychain wrapper).

**Spec:** `docs/superpowers/specs/2026-06-11-mobile-email-password-login-design.md`

---

## File Structure

| File | Role | Status |
|---|---|---|
| `desktop/frontend/src/platform/types.ts` | Add optional `login`, `logout`, `loadSavedPassword` to `RelayBridge` | Modify |
| `desktop/frontend/src/platform/capacitor.ts` | Implement the three methods via `fetch` + Keychain; new `PASSWORD_KEY` constant | Modify |
| `desktop/frontend/src/platform/wails.ts` | Untouched — Wails leaves the methods undefined | (no change) |
| `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` | Add `login`, `logout`, `loadSavedPassword` stubs so the fake satisfies the new interface | Modify |
| `desktop/frontend/src/platform/__tests__/capacitor.test.ts` | Tests for `relay.login`, `relay.logout`, `relay.loadSavedPassword` (success + error paths) | Modify |
| `desktop/frontend/src/i18n/messages/en.ts` | Replace token-related keys with email/password keys | Modify |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | Same | Modify |
| `desktop/frontend/src/mobile/MobileSetup.vue` | Swap token field for email + password + show/hide toggle; wire `onConnect` to `platform.relay.login` | Modify |
| `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts` | Rewrite for the email/password flow | Modify |
| `desktop/frontend/src/mobile/MobileApp.vue` | Call `platform.relay.logout()` in `onLogout()`; rewrite the stale comment | Modify |
| `desktop/frontend/src/mobile/__tests__/MobileSettings.test.ts` | Extend: logout click calls `platform.relay.logout` | Modify |

All work lives in `desktop/frontend/`. No Go-side, no backend, no Wails-binding regen.

**Convention for paths:** every command below assumes `cwd` is the repo root `/Users/attson/code/github.com.attson/atterm`. Tests run via `npm --prefix desktop/frontend test --run -- <file>` to avoid watch mode.

---

## Task 1: Extend `RelayBridge` interface + fake platform

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts:56-63`
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts:40-46`

- [ ] **Step 1: Add the three optional methods to `RelayBridge`**

Edit `desktop/frontend/src/platform/types.ts`, replace the `RelayBridge` interface with:

```ts
export interface RelayBridge {
  load(): Promise<_RelayConfig | null>
  save(cfg: _RelayConfig): Promise<void>
  clear(): Promise<void>
  fetchMe(): Promise<_RelayMe>
  setUplinkPaused?(paused: boolean): Promise<void>
  consumePairing?(relayBase: string, token: string): Promise<PairingConsumeResult>
  /** Mobile-only. POST {url}/api/auth/login with {email, password}; on success
   *  writes the returned session token + email into the persisted RelayConfig
   *  and stores the password under a separate Keychain key for one-tap
   *  re-login. Throws Error with one of these messages:
   *    'invalid_credentials' | 'rate_limited' | 'cannot_reach_relay' |
   *    'http_<status>'. */
  login?(url: string, email: string, password: string, allowInsecure: boolean): Promise<void>
  /** Mobile-only. POST /api/auth/logout (best-effort, network errors ignored)
   *  and clear the local session token. Preserves url + last_email + the
   *  saved password so the next login is one tap. */
  logout?(): Promise<void>
  /** Mobile-only. Reads the saved password from Keychain. Returns '' when
   *  nothing is stored. */
  loadSavedPassword?(): Promise<string>
}
```

- [ ] **Step 2: Add the three methods to the fake platform**

Edit `desktop/frontend/src/platform/__tests__/_fakePlatform.ts:40-46`, replace the `relay` block with:

```ts
    relay: {
      load: vi.fn().mockResolvedValue(null),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      fetchMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
      setUplinkPaused: vi.fn().mockResolvedValue(undefined),
      login: vi.fn().mockResolvedValue(undefined),
      logout: vi.fn().mockResolvedValue(undefined),
      loadSavedPassword: vi.fn().mockResolvedValue(''),
    },
```

- [ ] **Step 3: Run typecheck and existing tests to confirm nothing broke**

Run: `npm --prefix desktop/frontend run typecheck && npm --prefix desktop/frontend test --run`
Expected: PASS (the new optional methods don't break any existing usage).

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/__tests__/_fakePlatform.ts
git commit -m "feat(platform): add optional login/logout/loadSavedPassword to RelayBridge"
```

---

## Task 2: Capacitor `relay.login` — success path (TDD)

**Files:**
- Test: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`

- [ ] **Step 1: Add a failing test for the success path**

Append the following block to `desktop/frontend/src/platform/__tests__/capacitor.test.ts` (after the existing `describe('createCapacitorPlatform')` block, before the migration block):

```ts
describe('createCapacitorPlatform — relay.login', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    await secureStorage.remove('atterm.relay.password')
    vi.restoreAllMocks()
  })

  it('POSTs /api/auth/login and persists session_token + last_email + password', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      session_token: 'sess_abc',
      expires_at: 1234567890,
      user: { id: 'u1', email: 'me@example.com' },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const p = createCapacitorPlatform()
    await p.relay.login!('https://r.example.com', 'me@example.com', 'hunter2hunter2', false)

    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/auth/login')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).credentials).toBe('omit')
    expect(new Headers((init as RequestInit).headers).get('Content-Type')).toBe('application/json')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'me@example.com', password: 'hunter2hunter2',
    })

    const saved = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    expect(saved).toMatchObject({
      url: 'https://r.example.com',
      token: 'sess_abc',
      session_expires_at: 1234567890,
      last_email: 'me@example.com',
      allow_insecure_relay: false,
      remote_permission: 'full',
    })
    expect(await secureStorage.get('atterm.relay.password')).toBe('hunter2hunter2')
  })
})
```

- [ ] **Step 2: Run the test to confirm it fails**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: FAIL with `p.relay.login is not a function` or similar.

- [ ] **Step 3: Implement `relay.login`**

In `desktop/frontend/src/platform/capacitor.ts`:

(a) At the top of the file, after `const AUXKEYS_KEY = 'atterm.auxkeys'`, add:

```ts
const PASSWORD_KEY = 'atterm.relay.password'
```

(b) Inside the `relay:` block (after `consumePairing`), add:

```ts
      login: async (url, email, password, allowInsecure) => {
        const base = url.replace(/\/$/, '')
        let res: Response
        try {
          res = await fetch(base + '/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password }),
            credentials: 'omit',
          })
        } catch (e) {
          throw new Error('cannot_reach_relay')
        }
        if (res.status === 401) throw new Error('invalid_credentials')
        if (res.status === 429) throw new Error('rate_limited')
        if (!res.ok) throw new Error('http_' + res.status)
        const body = (await res.json()) as {
          session_token: string
          expires_at: number
          user: { id: string; email: string }
        }
        const cfg: RelayConfig = {
          url: base,
          token: body.session_token,
          session_expires_at: body.expires_at,
          allow_insecure_relay: allowInsecure,
          remote_permission: 'full',
          last_email: body.user.email,
          connected: false,
        }
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))
        await secureStorage.set(PASSWORD_KEY, password)
      },
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(capacitor): implement relay.login (POST /api/auth/login + Keychain)"
```

---

## Task 3: Capacitor `relay.login` — error paths (TDD)

**Files:**
- Test: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`

- [ ] **Step 1: Add failing tests for the four error paths**

Append inside the `describe('createCapacitorPlatform — relay.login')` block (created in Task 2):

```ts
  it('throws invalid_credentials on 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('invalid_credentials')
    expect(await secureStorage.get('atterm.relay.session')).toBeNull()
    expect(await secureStorage.get('atterm.relay.password')).toBeNull()
  })

  it('throws rate_limited on 429', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 429 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('rate_limited')
  })

  it('throws http_<status> on other non-2xx', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('http_500')
  })

  it('throws cannot_reach_relay when fetch rejects', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('cannot_reach_relay')
  })
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: PASS (Task 2's implementation already covers these paths).

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "test(capacitor): cover relay.login error paths (401/429/other/network)"
```

---

## Task 4: Capacitor `relay.logout` (TDD)

**Files:**
- Test: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`

- [ ] **Step 1: Add failing tests for `relay.logout`**

Append after the `relay.login` describe block in `capacitor.test.ts`:

```ts
describe('createCapacitorPlatform — relay.logout', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    await secureStorage.remove('atterm.relay.password')
    vi.restoreAllMocks()
  })

  it('POSTs /api/auth/logout with Bearer, clears local token, preserves email + password', async () => {
    await secureStorage.set('atterm.relay.session', JSON.stringify({
      url: 'https://r.example.com',
      token: 'sess_old',
      session_expires_at: 99,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: 'me@example.com',
      connected: false,
    }))
    await secureStorage.set('atterm.relay.password', 'hunter2hunter2')
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    const p = createCapacitorPlatform()
    await p.relay.logout!()

    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/auth/logout')
    expect((init as RequestInit).method).toBe('POST')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer sess_old')
    expect((init as RequestInit).credentials).toBe('omit')

    const saved = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    expect(saved.token).toBe('')
    expect(saved.session_expires_at).toBe(0)
    expect(saved.url).toBe('https://r.example.com')
    expect(saved.last_email).toBe('me@example.com')
    expect(saved.allow_insecure_relay).toBe(false)
    expect(saved.remote_permission).toBe('full')
    expect(await secureStorage.get('atterm.relay.password')).toBe('hunter2hunter2')
  })

  it('swallows network errors but still clears the local token', async () => {
    await secureStorage.set('atterm.relay.session', JSON.stringify({
      url: 'https://r.example.com',
      token: 'sess_old',
      session_expires_at: 99,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: 'me@example.com',
      connected: false,
    }))
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')))

    const p = createCapacitorPlatform()
    await expect(p.relay.logout!()).resolves.toBeUndefined()
    const saved = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    expect(saved.token).toBe('')
  })

  it('is a no-op when no config is stored', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const p = createCapacitorPlatform()
    await expect(p.relay.logout!()).resolves.toBeUndefined()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: FAIL with `p.relay.logout is not a function`.

- [ ] **Step 3: Implement `relay.logout`**

In `desktop/frontend/src/platform/capacitor.ts`, inside the `relay:` block (after `login`), add:

```ts
      logout: async () => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg) return
        if (cfg.url && cfg.token) {
          const base = cfg.url.replace(/\/$/, '')
          try {
            await fetch(base + '/api/auth/logout', {
              method: 'POST',
              headers: { Authorization: `Bearer ${cfg.token}` },
              credentials: 'omit',
            })
          } catch {
            // Best-effort. Local clear still happens below.
          }
        }
        const cleared: RelayConfig = {
          ...cfg,
          token: '',
          session_expires_at: 0,
        }
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cleared))
      },
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(capacitor): implement relay.logout (server logout + local token clear)"
```

---

## Task 5: Capacitor `relay.loadSavedPassword` (TDD)

**Files:**
- Test: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`

- [ ] **Step 1: Add failing tests**

Append:

```ts
describe('createCapacitorPlatform — relay.loadSavedPassword', () => {
  beforeEach(async () => {
    await secureStorage.remove('atterm.relay.password')
  })

  it("returns '' when nothing is stored", async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.loadSavedPassword!()).toBe('')
  })

  it('returns the value previously written by login', async () => {
    await secureStorage.set('atterm.relay.password', 'hunter2hunter2')
    const p = createCapacitorPlatform()
    expect(await p.relay.loadSavedPassword!()).toBe('hunter2hunter2')
  })
})
```

- [ ] **Step 2: Run, expect FAIL**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: FAIL with `p.relay.loadSavedPassword is not a function`.

- [ ] **Step 3: Implement**

In `desktop/frontend/src/platform/capacitor.ts`, inside the `relay:` block (after `logout`), add:

```ts
      loadSavedPassword: async () => {
        const v = await secureStorage.get(PASSWORD_KEY)
        return v ?? ''
      },
```

- [ ] **Step 4: Run, expect PASS**

Run: `npm --prefix desktop/frontend test --run -- src/platform/__tests__/capacitor.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(capacitor): expose relay.loadSavedPassword for one-tap re-login"
```

---

## Task 6: Update i18n strings

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts:306-320`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` (the corresponding `mobile:` block)

- [ ] **Step 1: Add the new keys + revise the banner**

In `desktop/frontend/src/i18n/messages/en.ts`, find the `mobile:` block at line 306 and:

- Remove keys: `apiToken`, `apiTokenRequired`, `apiTokenInvalid`.
- Replace `tokenInvalidBanner` text with: `"Your session expired. Sign in again."`
- Add (anywhere inside the `mobile:` block):

```ts
    email: "Email",
    password: "Password",
    passwordShow: "Show password",
    passwordHide: "Hide password",
    emailRequired: "Email is required",
    passwordRequired: "Password is required",
    invalidCredentials: "Invalid email or password",
    rateLimited: "Too many attempts, please try again later",
    loginButton: "Log in",
```

- [ ] **Step 2: Mirror in zh-CN.ts**

Open `desktop/frontend/src/i18n/messages/zh-CN.ts`, find the `mobile:` block, and:

- Remove the three `apiToken*` keys.
- Replace `tokenInvalidBanner` text with: `"会话已过期，请重新登录。"`
- Add:

```ts
    email: "邮箱",
    password: "密码",
    passwordShow: "显示密码",
    passwordHide: "隐藏密码",
    emailRequired: "请输入邮箱",
    passwordRequired: "请输入密码",
    invalidCredentials: "邮箱或密码错误",
    rateLimited: "操作过于频繁，请稍后再试",
    loginButton: "登录",
```

- [ ] **Step 3: Run typecheck to confirm the i18n schema is in sync**

Run: `npm --prefix desktop/frontend run typecheck`
Expected: PASS. If a key-completeness check fires, add the missing key to whichever file is short.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(mobile): replace apiToken keys with email/password/login keys"
```

---

## Task 7: Rewrite `MobileSetup.vue` — script section (TDD)

**Files:**
- Test: `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts`
- Modify: `desktop/frontend/src/mobile/MobileSetup.vue`

This task converts the form's data model + submit handler. The template is updated in Task 8.

- [ ] **Step 1: Rewrite the test file**

Overwrite `desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts` with:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { initI18n, resetI18nForTest } from '../../i18n'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSetup from '../MobileSetup.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(async () => {
  vi.clearAllMocks()
  resetI18nForTest()
  await initI18n({
    getLanguages: () => ['en-US'],
    listenLanguageChange: () => () => undefined,
  })
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false, windowControls: false, autoUpdate: false, pluginHost: false, fileDialog: false }
  __setPlatformForTests(platform)
})
afterEach(() => {
  __setPlatformForTests(null)
  resetI18nForTest()
})

describe('MobileSetup — fields', () => {
  it('renders url, scheme dropdown, email, password, connect', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="relay-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-scheme"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-email"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-password"]').exists()).toBe(true)
    expect(w.find('[data-testid="connect"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-token"]').exists()).toBe(false)
  })

  it('password show/hide toggle flips the input type', async () => {
    const w = mount(MobileSetup)
    const input = w.find('[data-testid="relay-password"]').element as HTMLInputElement
    expect(input.type).toBe('password')
    await w.find('[data-testid="password-toggle"]').trigger('click')
    expect((w.find('[data-testid="relay-password"]').element as HTMLInputElement).type).toBe('text')
  })

  it('hides the insecure switch entirely when scheme is https', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(false)
  })

  it('pre-fills url, email, and password from saved config + Keychain', async () => {
    ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'http://localhost:8080', token: 'sess_x',
      session_expires_at: 0, allow_insecure_relay: true, remote_permission: 'full',
      last_email: 'me@example.com', connected: false,
    })
    ;(platform.relay.loadSavedPassword as ReturnType<typeof vi.fn>).mockResolvedValue('hunter2hunter2')
    const w = mount(MobileSetup)
    await flushPromises()
    expect((w.find('[data-testid="relay-scheme"]').element as HTMLSelectElement).value).toBe('http://')
    expect((w.find('[data-testid="relay-url"]').element as HTMLInputElement).value).toBe('localhost:8080')
    expect((w.find('[data-testid="relay-email"]').element as HTMLInputElement).value).toBe('me@example.com')
    expect((w.find('[data-testid="relay-password"]').element as HTMLInputElement).value).toBe('hunter2hunter2')
  })

  it('shows the token-invalid banner when reason prop is set', () => {
    const w = mount(MobileSetup, { props: { reason: 'token_invalid' } })
    expect(w.text()).toMatch(/session|expired|sign in|登录|过期/i)
  })
})

describe('MobileSetup — submit', () => {
  it('shows validation error for malformed url and does not call login', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('not a url')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|malformed/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('shows error when email is empty', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/email/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('shows error when password is empty', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/password/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('on success calls platform.relay.login, fetchMe, and emits connected', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(platform.relay.login).toHaveBeenCalledWith(
      'https://r.example.com', 'me@example.com', 'hunter2hunter2', false,
    )
    expect(platform.relay.fetchMe).toHaveBeenCalled()
    expect(w.emitted('connected')).toBeTruthy()
  })

  it('maps invalid_credentials to a friendly error', async () => {
    ;(platform.relay.login as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('invalid_credentials'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|incorrect|错误/i)
    expect(w.emitted('connected')).toBeFalsy()
  })

  it('maps rate_limited to a friendly error', async () => {
    ;(platform.relay.login as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('rate_limited'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/too many|频繁|later/i)
  })
})
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `npm --prefix desktop/frontend test --run -- src/mobile/__tests__/MobileSetup.test.ts`
Expected: FAIL (missing `relay-email`, `relay-password`, `password-toggle` selectors; `login` never called).

- [ ] **Step 3: Update `<script setup>` in `MobileSetup.vue`**

Open `desktop/frontend/src/mobile/MobileSetup.vue`. Replace lines 1–127 (the entire `<script setup>` block) with:

```vue
<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { type LocalePreference } from '../i18n'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { validateRelayBase } from './relay'
import PairingConsume from './PairingConsume.vue'
import { BarcodeScanner } from '@capacitor-mlkit/barcode-scanning'

const props = defineProps<{ reason?: 'token_invalid' | null }>()
const emit = defineEmits<{ (e: 'connected'): void }>()

const platform = usePlatform()
const { t, languageOptions, localePreference, setLocalePreference } = useI18n()
const scheme = ref<'https://' | 'http://'>('https://')
const host = ref('')
const url = computed(() => scheme.value + host.value.trim())
const email = ref('')
const password = ref('')
const showPassword = ref(false)
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)
const scannedUrl = ref<string | null>(null)

function applyUrl(raw: string): void {
  const m = /^\s*(https?:\/\/)(.*)$/i.exec(raw)
  if (m) {
    scheme.value = m[1].toLowerCase() as 'https://' | 'http://'
    host.value = m[2].trim()
  } else {
    host.value = raw.trim()
  }
}

function normalizeHost(): void {
  applyUrl(host.value)
}

watch(scheme, (s) => {
  if (s === 'https://') allowInsecure.value = false
})

const banner = computed(() =>
  props.reason === 'token_invalid'
    ? t('mobile.tokenInvalidBanner')
    : null,
)

const localizedLanguageOptions = computed(() =>
  languageOptions.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })),
)

onMounted(async () => {
  const cfg = await platform.relay.load()
  if (cfg) {
    applyUrl(cfg.url || '')
    email.value = cfg.last_email || ''
    allowInsecure.value = !!cfg.allow_insecure_relay
  }
  if (platform.relay.loadSavedPassword) {
    password.value = await platform.relay.loadSavedPassword()
  }
})

async function onScanQR(): Promise<void> {
  error.value = null
  try {
    const { camera } = await BarcodeScanner.requestPermissions()
    if (camera !== 'granted' && camera !== 'limited') {
      error.value = t('mobile.pairing.cameraDenied')
      return
    }
    const { barcodes } = await BarcodeScanner.scan({ formats: ['QR_CODE' as any] })
    const first = barcodes[0]
    if (!first?.rawValue) {
      error.value = t('mobile.pairing.noQrDetected')
      return
    }
    scannedUrl.value = first.rawValue
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

function onConsumeCancelled(): void {
  scannedUrl.value = null
}

async function onConnect(): Promise<void> {
  error.value = null
  const v = validateRelayBase(url.value, allowInsecure.value)
  if (v) { error.value = v; return }
  if (!email.value.trim()) { error.value = t('mobile.emailRequired'); return }
  if (!password.value) { error.value = t('mobile.passwordRequired'); return }
  if (!platform.relay.login) {
    error.value = t('mobile.cannotReachRelay', { message: 'login_unsupported' })
    return
  }
  submitting.value = true
  try {
    await platform.relay.login(
      url.value.replace(/\/$/, ''),
      email.value.trim(),
      password.value,
      allowInsecure.value,
    )
    await platform.relay.fetchMe()
    emit('connected')
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'invalid_credentials') error.value = t('mobile.invalidCredentials')
    else if (msg === 'rate_limited') error.value = t('mobile.rateLimited')
    else if (/403|origin/i.test(msg)) error.value = t('mobile.originRejected')
    else error.value = t('mobile.cannotReachRelay', { message: msg })
  } finally {
    submitting.value = false
  }
}

async function onLanguageChange(e: Event): Promise<void> {
  const target = e.target as HTMLSelectElement
  await setLocalePreference(target.value as LocalePreference)
}
</script>
```

- [ ] **Step 4: Run the tests again — most should now pass, those checking the template (`relay-email` / `relay-password` / `password-toggle` selectors) still fail**

Run: `npm --prefix desktop/frontend test --run -- src/mobile/__tests__/MobileSetup.test.ts`
Expected: Tests that touch only the script logic pass (login wiring, error mapping). Tests that find the new `data-testid` elements still FAIL — those go green in Task 8.

- [ ] **Step 5: Commit (template still unchanged — leave WIP)**

Skip the commit; bundle this with Task 8 so the working tree never has a half-broken file. Continue.

---

## Task 8: Rewrite `MobileSetup.vue` — template + style for email/password

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSetup.vue` (template + style sections)

- [ ] **Step 1: Replace the `<template>` section**

Edit `desktop/frontend/src/mobile/MobileSetup.vue`. Find the `<template>` block (currently lines 129–186 before edits, may have shifted) and replace its inner content with:

```vue
<template>
  <div class="setup">
    <PairingConsume
      v-if="scannedUrl"
      :scanned-url="scannedUrl"
      :allow-insecure="allowInsecure"
      @connected="emit('connected')"
      @cancel="onConsumeCancelled"
    />
    <template v-else>
      <h1>AT Term</h1>
      <p class="sub">{{ t('mobile.setupSubtitle') }}</p>
      <div v-if="banner" class="banner">{{ banner }}</div>

      <button data-testid="scan-qr" class="btn btn-primary" :disabled="submitting" @click="onScanQR">
        {{ t('mobile.pairing.scan') }}
      </button>
      <p class="or">{{ t('mobile.pairing.orManual') }}</p>

      <label class="field">
        <span>{{ t('settings.general.languageLabel') }}</span>
        <select data-testid="mobile-language" :value="localePreference" :disabled="submitting" @change="onLanguageChange">
          <option v-for="option in localizedLanguageOptions" :key="option.value" :value="option.value">
            {{ option.label }}
          </option>
        </select>
      </label>
      <label class="field">
        <span>{{ t('mobile.relayUrl') }}</span>
        <div class="url-input">
          <select data-testid="relay-scheme" v-model="scheme" :disabled="submitting" class="scheme-select" :aria-label="t('mobile.relayUrl')">
            <option value="https://">https://</option>
            <option value="http://">http://</option>
          </select>
          <input data-testid="relay-url" v-model="host" :disabled="submitting" placeholder="relay.example.com" autocomplete="off" autocapitalize="off" spellcheck="false" @input="normalizeHost" />
        </div>
      </label>
      <label class="field">
        <span>{{ t('mobile.email') }}</span>
        <input data-testid="relay-email" v-model="email" :disabled="submitting" type="email" autocomplete="username" autocapitalize="off" spellcheck="false" placeholder="me@example.com" />
      </label>
      <label class="field">
        <span>{{ t('mobile.password') }}</span>
        <div class="password-input">
          <input data-testid="relay-password" v-model="password" :disabled="submitting" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="••••••••" />
          <button
            type="button"
            data-testid="password-toggle"
            class="password-toggle"
            :aria-label="showPassword ? t('mobile.passwordHide') : t('mobile.passwordShow')"
            :aria-pressed="showPassword"
            :disabled="submitting"
            @click="showPassword = !showPassword"
          >
            {{ showPassword ? t('mobile.passwordHide') : t('mobile.passwordShow') }}
          </button>
        </div>
      </label>
      <label v-if="scheme === 'http://'" class="row">
        <span>{{ t('mobile.allowInsecure') }}</span>
        <input data-testid="allow-insecure" v-model="allowInsecure" :disabled="submitting" type="checkbox" />
      </label>
      <aside v-if="scheme === 'http://' && allowInsecure" class="warn-hint" data-state="warn" data-testid="insecure-hint">
        <svg class="warn-icon" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>
        <span>{{ t('mobile.insecure.setupHint') }}</span>
      </aside>
      <p v-if="error" class="error">{{ error }}</p>
      <button data-testid="connect" class="btn" :disabled="submitting" @click="onConnect">{{ t('mobile.loginButton') }}</button>
    </template>
  </div>
</template>
```

- [ ] **Step 2: Extend `<style scoped>` with the password-input layout**

In the same file, find the `<style scoped>` section. Append (just before the closing `</style>`):

```css
.password-input { display: flex; gap: 8px; align-items: stretch; }
.password-input input { flex: 1 1 auto; min-width: 0; }
.password-toggle { flex: 0 0 auto; height: 42px; padding: 0 12px; border-radius: 9px; border: 1px solid #1e2638; background: #11182b; color: #8d93a3; font-size: 0.8rem; font-family: var(--font-sans); }
.password-toggle:disabled { opacity: 0.6; }
```

- [ ] **Step 3: Run the MobileSetup tests, expect PASS**

Run: `npm --prefix desktop/frontend test --run -- src/mobile/__tests__/MobileSetup.test.ts`
Expected: All MobileSetup tests pass.

- [ ] **Step 4: Run the full frontend test suite + typecheck**

Run: `npm --prefix desktop/frontend run typecheck && npm --prefix desktop/frontend test --run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSetup.vue desktop/frontend/src/mobile/__tests__/MobileSetup.test.ts
git commit -m "feat(mobile): replace API token field with email + password login form"
```

---

## Task 9: Wire `MobileApp.onLogout` to `platform.relay.logout` (TDD)

**Files:**
- Test: `desktop/frontend/src/mobile/__tests__/MobileSettings.test.ts`
- Modify: `desktop/frontend/src/mobile/MobileApp.vue:95-106`

`MobileSettings` itself only emits `logout`; the side-effect lives on `MobileApp.onLogout`. The cleanest path is to add a test for the full flow at the `MobileApp` level OR to verify the platform call inside `MobileApp` directly. We'll extend the existing `MobileSettings.test.ts` with a small mount of `MobileApp` for the integration check.

- [ ] **Step 1: Add a failing test in `MobileSettings.test.ts`**

Append to `desktop/frontend/src/mobile/__tests__/MobileSettings.test.ts` (outside the existing `describe('MobileSettings')` block, at the bottom of the file):

```ts
import MobileApp from '../MobileApp.vue'

describe('MobileApp — hard logout', () => {
  it('calls platform.relay.logout when MobileSettings emits logout', async () => {
    ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'https://r.example.com',
      token: 'sess_x',
      session_expires_at: 0,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: 'me@example.com',
      connected: false,
    })
    const w = mount(MobileApp)
    await flushPromises()

    // Navigate to settings, then click logout.
    // MobileApp shows MobileSetup at boot if relay is configured, the user
    // hits connected, then sees the list. Simulate by emitting connected
    // directly from MobileSetup and then opening settings.
    // The simpler path: find the MobileSettings child once it is rendered.
    // To avoid a full nav harness, mount MobileSettings inside MobileApp's
    // settings view and trigger logout there. The MobileApp wrapper exposes
    // the handler bound to the @logout event, so we drive it via the child.
    // Cheapest: assert that the handler exists and calls platform.relay.logout.
    const vm = w.vm as unknown as { onLogout: () => Promise<void> | void }
    if (typeof vm.onLogout !== 'function') {
      throw new Error('MobileApp.onLogout not exposed for test — see plan Task 9')
    }
    await vm.onLogout()
    expect(platform.relay.logout).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run the test, expect FAIL**

Run: `npm --prefix desktop/frontend test --run -- src/mobile/__tests__/MobileSettings.test.ts`
Expected: FAIL because (a) `MobileApp.onLogout` is not exposed via `defineExpose`, and (b) it does not call `platform.relay.logout`.

- [ ] **Step 3: Update `MobileApp.vue`**

Edit `desktop/frontend/src/mobile/MobileApp.vue:95-106`. Replace the `onLogout` block and add a `defineExpose` for testability:

```ts
// Hard logout: revoke the server-side session via platform.relay.logout
// (which POSTs /api/auth/logout and clears the local token) and return to
// the setup screen. The saved URL + last_email + Keychain password are
// preserved so re-login is one tap.
async function onLogout(): Promise<void> {
  if (platform.relay.logout) {
    await platform.relay.logout()
  }
  openTerminals.value = []
  recency.value = []
  activeSessionId.value = ''
  reason.value = null
  view.value = 'setup'
}
```

Then, near the bottom of the `<script setup>` block (anywhere after `onLogout` is declared), add:

```ts
defineExpose({ onLogout })
```

- [ ] **Step 4: Run the test, expect PASS**

Run: `npm --prefix desktop/frontend test --run -- src/mobile/__tests__/MobileSettings.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileApp.vue desktop/frontend/src/mobile/__tests__/MobileSettings.test.ts
git commit -m "feat(mobile): hard logout calls platform.relay.logout (revoke server session)"
```

---

## Task 10: Full verification + iOS smoke check

**Files:** (no code changes — verification only)

- [ ] **Step 1: Run typecheck + the full vitest suite**

Run: `npm --prefix desktop/frontend run typecheck && npm --prefix desktop/frontend test --run`
Expected: PASS. If anything fails, fix in place and re-commit before moving on.

- [ ] **Step 2: Build the Capacitor bundle**

Run: `npm --prefix desktop/frontend run build:capacitor`
Expected: PASS, `desktop/frontend/dist-capacitor/` populated.

- [ ] **Step 3: iOS smoke check — manual**

If an iOS simulator or device is available, run `npx --prefix desktop/frontend cap sync ios && open mobile/ios/App/App.xcworkspace` and verify in the simulator:

1. First launch (or after clearing the app's Keychain): the manual form shows URL + Email + Password fields, no token field. QR scan button is still on top.
2. Enter relay URL + valid email + password → tap Log in → land on session list.
3. Settings → Log out → token cleared on server (no longer in `GET /api/me/sessions`); back on the setup screen, email + password still pre-filled. Tap Log in again → land on session list.
4. Force a 401 (e.g. expire the session in the relay DB) → app returns to setup with the `session expired` banner; email + password still pre-filled.

If iOS hardware isn't available this turn, record the check as "deferred — verify before ship".

- [ ] **Step 4: Final commit if any docs were updated**

No commit if Steps 1–3 produced no changes. Otherwise:

```bash
git add -A
git commit -m "chore: verification fixups for mobile email/password login"
```

---

## Self-Review Notes

**Spec coverage:**

- Spec § "RelayBridge interface changes" → Task 1.
- Spec § "Capacitor implementation" (login / logout / loadSavedPassword + PASSWORD_KEY) → Tasks 2, 3, 4, 5.
- Spec § "MobileSetup.vue changes" → Tasks 7 + 8.
- Spec § "MobileApp.vue / MobileSettings.vue changes" → Task 9.
- Spec § "i18n strings" → Task 6.
- Spec § "Tests" → covered alongside each implementation task (TDD).
- Spec § "Security notes" → behavior preserved by the test cases in Tasks 2, 4 (Keychain writes, network-failure tolerance, no auto-login on cold start).
- Spec § "What's not touched" — Wails, backend, PairingConsume, SettingsRelay — confirmed: none of the tasks edit those files.

**Placeholder scan:** No "TBD", "TODO", or "similar to Task N" references. Every code block is complete.

**Type consistency:** `loadSavedPassword` returns `Promise<string>` everywhere; `login` signature matches in interface, capacitor impl, fake platform, and `MobileSetup.onConnect`. `logout` returns `Promise<void>` everywhere. `PASSWORD_KEY = 'atterm.relay.password'` is the single source of truth, declared once in `capacitor.ts` and referenced literally in tests.

**Execution order:** Task 7's tests target template selectors added in Task 8 — Step 4 in Task 7 explicitly notes this and Step 5 defers the commit so the tree never lands half-broken. Tasks 7 + 8 are conceptually one PR-commit boundary but kept logically separate for review clarity.
