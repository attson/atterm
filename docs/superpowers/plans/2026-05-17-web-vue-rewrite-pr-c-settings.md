# Web Vue Rewrite — PR-C: Settings 4-Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the vanilla `/settings.html` (API Tokens / Change Password / Sessions / Danger Zone tabs) with a Vue 3 + Naive UI version mounted at the same path, served from the relay's embedded FS.

**Architecture:** A new Vite entry `web/settings.html` mounts a single Vue app whose `App.vue` provides the dark theme, the shared `Topbar`, and an `n-tabs` selector wired to `location.hash` (so deep links like `/settings.html#sessions` keep working). Each tab is its own `.vue` component under `web/src/settings/tabs/`. All `/api/me/*` endpoints get typed helpers added to `web/src/shared/api/me.ts`. The relay continues to gate access via cookie; nothing on the server changes.

**Tech Stack:** Vue 3, TypeScript, Naive UI (NTabs, NCard, NForm, NInput, NButton, NPopconfirm, NUseMessage), Vitest, `@vue/test-utils`, happy-dom. Node 20.

**Reference spec:** `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`.

**Pre-flight:**
- Branch: `web/vue-rewrite-pr-c-settings` (cut from `origin/main` at the start of PR-C).
- PR-B merged into `main` as `ba61ac2` (#46). Foundation modules (`apiFetch`, `safeNext`, `setCsrfToken`, `clearCsrfToken`, `ApiError`, `getMe`, `login`, `signup`, `logout`, `fetchVersionLabel`) are live.
- Bootstrap admin envs for local relay:
  ```
  ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com'
  ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!'
  ```

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `web/src/shared/api/types.ts` | Modify | Add `ApiTokenRow`, `ApiTokenCreated`, `SessionRow`, `SignOutOthersResponse` DTOs |
| `web/src/shared/api/me.ts` | Modify | Add `listTokens`, `createToken`, `revokeToken`, `listSessions`, `revokeSession`, `signOutOthers`, `changePassword`, `deleteMe` alongside the existing `getMe` |
| `web/tests/unit/shared/api/me.test.ts` | Create | Vitest coverage for the 8 new helpers + fetch-mock CSRF assertions |
| `web/src/shared/components/Topbar.vue` | Create | Shared layout shell (brand + nav + sign-out); reads `getMe()` once to decide whether to render the Admin tab; loads version label |
| `web/tests/unit/shared/components/Topbar.test.ts` | Create | Component test: renders nav, shows Admin link only when `is_admin`, sign-out calls logout |
| `web/src/settings/main.ts` | Create | Creates the Vue app, imports tokens.css, mounts `#app` |
| `web/src/settings/App.vue` | Create | Wraps `n-config-provider` + `n-message-provider`; renders `<Topbar active="settings" />`, `<n-tabs>` with 4 panes; binds active value to `location.hash` |
| `web/src/settings/tabs/ApiTokens.vue` | Create | Lists tokens, creates new (shows plaintext once), revokes existing |
| `web/src/settings/tabs/ChangePassword.vue` | Create | Two-input form; on success redirects to `/login.html` (cookie was invalidated server-side) |
| `web/src/settings/tabs/Sessions.vue` | Create | Lists web sessions, marks current, revokes other sessions, "Sign out everywhere except this device" |
| `web/src/settings/tabs/DangerZone.vue` | Create | Email + password confirmation form; calls DELETE /api/me; redirects to `/login.html` on 204 |
| `web/tests/unit/settings/App.test.ts` | Create | Verifies tab routing via hash, four panes mount |
| `web/tests/unit/settings/tabs/ApiTokens.test.ts` | Create | List render, create flow (plaintext appears), revoke clears row |
| `web/tests/unit/settings/tabs/ChangePassword.test.ts` | Create | Submit success → redirect to /login.html; password_weak / current_password_wrong → error message |
| `web/tests/unit/settings/tabs/Sessions.test.ts` | Create | List render with `is_current` mark; revoke triggers DELETE; sign-out-others triggers POST |
| `web/tests/unit/settings/tabs/DangerZone.test.ts` | Create | Submit 204 → redirect; `email_mismatch`/`password_incorrect`/`last_admin` → error message |
| `web/settings.html` | Create | Vite entry HTML with `<meta name="page" content="settings">`, `<div id="app">`, `<script type="module" src="/src/settings/main.ts">` |
| `web/vite.config.ts` | Modify | `rollupOptions.input` adds `settings` |
| `web/legacy/settings.html` | Delete | Replaced by Vue build via embed overlay |
| `web/legacy/settings.js` | Delete | Replaced |
| `web/legacy/settings-sessions.js` | Delete | Replaced |
| `web/legacy/settings-danger.js` | Delete | Replaced |
| `web/legacy/settings.test.mjs` | Delete | Replaced by vitest component tests; contract test added if any HTML-level invariant remains |
| `web/legacy/sw.js` | Modify | Drop `./settings.js`, `./settings-sessions.js`, `./settings-danger.js` from `ASSETS`; bump `CACHE` hex |
| `web/legacy/no-raw-colors.test.mjs` | Modify | Remove `settings.html` allow-list entry (file no longer exists) |
| `internal/relay/web-dist/**` | Regenerate | Via `./scripts/build-web.sh`; new `settings.html` + assets overlay legacy |

---

## Pre-flight

- [ ] **Step 0.1: Confirm baseline**

```bash
git status --short && git rev-parse --abbrev-ref HEAD
```
Expected: branch `web/vue-rewrite-pr-c-settings`; working tree only contains the pre-existing untracked artifacts (`.claude/`, `.playwright-mcp/`, `atterm-relay`, two unrelated `2026-05-14-*.md` plan drafts).

- [ ] **Step 0.2: Confirm dependencies installed**

```bash
[ -d web/node_modules ] && echo OK || (cd web && npm ci --ignore-scripts)
```

- [ ] **Step 0.3: Confirm legacy settings still served from embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3
curl -sI http://127.0.0.1:18080/settings.html | head -1
kill $RELAY_PID 2>/dev/null
```
Expected: 200 (legacy `settings.html` still served until PR-C overlay replaces it).

---

## Task 1: Extend `api/me.ts` with the 8 new helpers + DTOs + tests (TDD)

**Files:**
- Modify: `web/src/shared/api/types.ts` (append 4 interfaces)
- Modify: `web/src/shared/api/me.ts` (append 8 functions)
- Create: `web/tests/unit/shared/api/me.test.ts`

- [ ] **Step 1.1: Add the new DTOs to `types.ts`**

Append to the bottom of `web/src/shared/api/types.ts`:

```ts
export interface ApiTokenRow {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface ApiTokenCreated {
  id: string
  plaintext: string
  prefix: string
  created_at: string
}

export interface SessionRow {
  id_hash: string
  user_agent: string
  ip_prefix: string
  created_at: number  // unix ms
  expires_at: number  // unix ms
  is_current: boolean
}

export interface SignOutOthersResponse {
  deleted: number
}
```

- [ ] **Step 1.2: Write the failing tests (TDD red)**

Create `web/tests/unit/shared/api/me.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  getMe,
  listTokens,
  createToken,
  revokeToken,
  listSessions,
  revokeSession,
  signOutOthers,
  changePassword,
  deleteMe,
} from '@shared/api/me'
import { clearCsrfToken, setCsrfToken } from '@shared/api/client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function emptyResponse(status: number): Response {
  return new Response(null, { status })
}

describe('me.ts /api/me/tokens', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('listTokens GETs /api/me/tokens and returns the array', async () => {
    const tokens = [{ id: 't1', name: 'laptop', prefix: 'atk_abc', created_at: '2026-01-01T00:00:00Z' }]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, tokens))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listTokens()

    expect(result).toEqual(tokens)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me/tokens')
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect((init.method || 'GET').toUpperCase()).toBe('GET')
  })

  it('createToken POSTs the name and returns the plaintext payload', async () => {
    const created = { id: 't1', plaintext: 'atk_secret', prefix: 'atk_secret_pfx', created_at: '2026-01-01T00:00:00Z' }
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, created))
    vi.stubGlobal('fetch', fetchMock)

    const result = await createToken('laptop')

    expect(result).toEqual(created)
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/me/tokens')
    expect((init as RequestInit).method).toBe('POST')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({ name: 'laptop' })
    expect(new Headers((init as RequestInit).headers).get('X-CSRF-Token')).toBe('csrf-test')
  })

  it('revokeToken DELETEs the id-encoded URL and resolves on 204', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await revokeToken('tok 123/special')

    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/me/tokens/tok%20123%2Fspecial')
    expect((init as RequestInit).method).toBe('DELETE')
  })
})

describe('me.ts /api/me/sessions', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('listSessions GETs /api/me/sessions and returns rows', async () => {
    const rows = [{
      id_hash: 'h1',
      user_agent: 'Chrome',
      ip_prefix: '10.0.0',
      created_at: 1700000000000,
      expires_at: 1702000000000,
      is_current: true,
    }]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, rows))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listSessions()

    expect(result).toEqual(rows)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me/sessions')
  })

  it('revokeSession DELETEs the id-hash-encoded URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await revokeSession('hash/special')

    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me/sessions/hash%2Fspecial')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('DELETE')
  })

  it('signOutOthers POSTs and returns deleted count', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { deleted: 3 }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await signOutOthers()

    expect(result).toEqual({ deleted: 3 })
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me/sessions/sign-out-others')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')
  })
})

describe('me.ts /api/me/password', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('changePassword POSTs current_password + new_password, resolves on 204', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await changePassword('oldpw-1234', 'newpw-12345')

    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me/password')
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      current_password: 'oldpw-1234',
      new_password: 'newpw-12345',
    })
  })
})

describe('me.ts /api/me delete', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('deleteMe DELETEs /api/me with email + password body, clears CSRF cache', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(emptyResponse(204))
      // After deleteMe clears the cache, a subsequent POST should not
      // include X-CSRF-Token. We exercise that here.
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await deleteMe('a@b.example', 'password-1234')

    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/me')
    expect((init as RequestInit).method).toBe('DELETE')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'a@b.example',
      password: 'password-1234',
    })

    // verify cache cleared by issuing an unrelated mutating call
    const { apiFetch } = await import('@shared/api/client')
    await apiFetch('/api/me/tokens', { method: 'POST', body: '{}' })
    const headers = new Headers((fetchMock.mock.calls[1]![1] as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })
})

describe('me.ts /api/me get (regression of PR-B getMe)', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('getMe still populates the cache when csrf_token is present', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b', csrf_token: 'fresh-csrf' }))
      .mockResolvedValueOnce(jsonResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await getMe()
    const { apiFetch } = await import('@shared/api/client')
    await apiFetch('/api/me/password', { method: 'POST', body: '{}' })

    const headers = new Headers((fetchMock.mock.calls[1]![1] as RequestInit).headers)
    expect(headers.get('X-CSRF-Token')).toBe('fresh-csrf')
  })
})
```

- [ ] **Step 1.3: Run tests to verify they fail**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -30
```
Expected: import errors for the new exports (`listTokens`, etc.). The 30 cases from PR-B still pass.

- [ ] **Step 1.4: Add the helpers to `me.ts`**

Open `web/src/shared/api/me.ts`. The current file only exports `getMe`. Append the following so the whole file becomes:

```ts
import { apiFetch, clearCsrfToken, setCsrfToken } from './client'
import type {
  ApiTokenCreated,
  ApiTokenRow,
  MeResponse,
  SessionRow,
  SignOutOthersResponse,
} from './types'

// getMe fetches the current user and refreshes the CSRF cache when the
// server returns a token (always present for cookie-authenticated calls).
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  if (data.csrf_token) setCsrfToken(data.csrf_token)
  return data
}

// API token helpers (settings → API Tokens tab).
export async function listTokens(): Promise<ApiTokenRow[]> {
  const { data } = await apiFetch<ApiTokenRow[]>('/api/me/tokens')
  return data
}

export async function createToken(name: string): Promise<ApiTokenCreated> {
  const { data } = await apiFetch<ApiTokenCreated>('/api/me/tokens', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
  return data
}

export async function revokeToken(id: string): Promise<void> {
  await apiFetch(`/api/me/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// Web session helpers (settings → Signed-in devices tab).
export async function listSessions(): Promise<SessionRow[]> {
  const { data } = await apiFetch<SessionRow[]>('/api/me/sessions')
  return data
}

export async function revokeSession(idHash: string): Promise<void> {
  await apiFetch(`/api/me/sessions/${encodeURIComponent(idHash)}`, { method: 'DELETE' })
}

export async function signOutOthers(): Promise<SignOutOthersResponse> {
  const { data } = await apiFetch<SignOutOthersResponse>(
    '/api/me/sessions/sign-out-others',
    { method: 'POST' },
  )
  return data
}

// Password (settings → Change Password tab).
export async function changePassword(
  current_password: string,
  new_password: string,
): Promise<void> {
  await apiFetch('/api/me/password', {
    method: 'POST',
    body: JSON.stringify({ current_password, new_password }),
  })
}

// Account deletion (settings → Danger zone tab). Server clears the
// session cookie; we also drop the local CSRF cache because the
// account no longer exists to derive a new token.
export async function deleteMe(email: string, password: string): Promise<void> {
  await apiFetch('/api/me', {
    method: 'DELETE',
    body: JSON.stringify({ email, password }),
  })
  clearCsrfToken()
}
```

- [ ] **Step 1.5: Run tests to verify green**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: 30 (PR-B) + 12 new me.ts cases = **42 passing**.

- [ ] **Step 1.6: Type-check**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: clean.

- [ ] **Step 1.7: Embed drift gate (sanity)**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0 (source-only change; embed unaffected).

- [ ] **Step 1.8: Commit**

```bash
git add web/src/shared/api/types.ts web/src/shared/api/me.ts web/tests/unit/shared/api/me.test.ts
git commit -m "$(cat <<'EOF'
feat(web): /api/me/* helpers (tokens, sessions, password, delete)

Layers token CRUD, session list/revoke/sign-out-others, password
change, and account-delete on top of PR-B's apiFetch. deleteMe clears
the local CSRF cache after the request resolves. 12 new vitest cases
verify HTTP method/path, body shape, CSRF header injection, and cache
state across the cleared-then-reused path.
EOF
)"
```

---

## Task 2: Shared `Topbar.vue` component + test

**Files:**
- Create: `web/src/shared/components/Topbar.vue`
- Create: `web/tests/unit/shared/components/Topbar.test.ts`

- [ ] **Step 2.1: Write the failing test**

Create `web/tests/unit/shared/components/Topbar.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn(),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))

import Topbar from '@shared/components/Topbar.vue'
import { getMe } from '@shared/api/me'
import { logout } from '@shared/api/auth'

describe('Topbar.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders Home and Settings nav links unconditionally', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false })
    const wrapper = mount(Topbar, { props: { active: 'settings' } })
    await flushPromises()

    const links = wrapper.findAll('nav a')
    const labels = links.map((l) => l.text())
    expect(labels).toContain('Home')
    expect(labels).toContain('Settings')
  })

  it('hides Admin link when is_admin is false', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false })
    const wrapper = mount(Topbar, { props: { active: 'settings' } })
    await flushPromises()

    expect(wrapper.findAll('nav a').map((l) => l.text())).not.toContain('Admin')
  })

  it('shows Admin link when is_admin is true', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: true })
    const wrapper = mount(Topbar, { props: { active: 'admin' } })
    await flushPromises()

    expect(wrapper.findAll('nav a').map((l) => l.text())).toContain('Admin')
  })

  it('marks the active link with aria-current=page', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(Topbar, { props: { active: 'settings' } })
    await flushPromises()

    const settingsLink = wrapper.findAll('nav a').find((l) => l.text() === 'Settings')
    expect(settingsLink?.attributes('aria-current')).toBe('page')
  })

  it('Sign-out triggers logout() and navigates to /login.html', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(Topbar, { props: { active: 'settings' } })
    await flushPromises()

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(logout).toHaveBeenCalled()
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('still navigates to /login.html when logout throws (offline)', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    ;(logout as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('offline'))
    const wrapper = mount(Topbar, { props: { active: 'settings' } })
    await flushPromises()

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })
})
```

- [ ] **Step 2.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: tests fail because `@shared/components/Topbar.vue` does not exist.

- [ ] **Step 2.3: Implement `Topbar.vue`**

Create `web/src/shared/components/Topbar.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import type { MeResponse } from '@shared/api/types'
import { getMe } from '@shared/api/me'
import { logout } from '@shared/api/auth'
import { fetchVersionLabel } from '@shared/api/version'

defineProps<{ active: 'home' | 'settings' | 'admin' }>()

const me = ref<MeResponse | null>(null)
const versionLabel = ref('version dev')

onMounted(async () => {
  try {
    me.value = await getMe()
  } catch {
    // apiFetch handles 401 by redirecting; nothing else to do here.
  }
  try {
    versionLabel.value = await fetchVersionLabel()
  } catch {
    // Keep the fallback label.
  }
})

async function onLogout() {
  try {
    await logout()
  } catch {
    // The session may already be gone; still navigate to login.
  } finally {
    location.assign('/login.html')
  }
}
</script>

<template>
  <header class="topbar">
    <div class="brand-block">
      <div class="brand">AT Term</div>
      <div class="version">{{ versionLabel }}</div>
    </div>
    <nav class="topnav" aria-label="Primary">
      <a
        href="/"
        :class="{ active: active === 'home' }"
        :aria-current="active === 'home' ? 'page' : undefined"
      >Home</a>
      <a
        href="/settings.html"
        :class="{ active: active === 'settings' }"
        :aria-current="active === 'settings' ? 'page' : undefined"
      >Settings</a>
      <a
        v-if="me?.is_admin"
        href="/admin/"
        :class="{ active: active === 'admin' }"
        :aria-current="active === 'admin' ? 'page' : undefined"
      >Admin</a>
    </nav>
    <button type="button" class="ghost-btn" @click="onLogout">Sign out</button>
  </header>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.5rem;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  color: var(--fg);
}
.brand-block { display: flex; flex-direction: column; gap: 0.125rem; }
.brand { font-weight: 700; letter-spacing: 0.08em; font-size: 1rem; }
.version { color: var(--fg-dim); font-size: 0.75rem; }
.topnav { display: flex; gap: 1.25rem; }
.topnav a {
  color: var(--fg-dim);
  text-decoration: none;
  font-weight: 600;
  font-size: 0.875rem;
  letter-spacing: 0.04em;
}
.topnav a.active { color: var(--accent); }
.topnav a:hover { color: var(--accent); }
.ghost-btn {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0.5rem 1rem;
  cursor: pointer;
  font: inherit;
  font-size: 0.875rem;
  font-weight: 600;
}
.ghost-btn:hover { border-color: var(--accent); color: var(--accent); }
</style>
```

- [ ] **Step 2.4: Run tests**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: 42 + 6 = **48 passing**.

- [ ] **Step 2.5: Type-check + drift gate**

```bash
cd web && npx vue-tsc --noEmit
git diff --exit-code -- internal/relay/web-dist
```
Both clean / exit 0.

- [ ] **Step 2.6: Commit**

```bash
git add web/src/shared/components web/tests/unit/shared/components
git commit -m "$(cat <<'EOF'
feat(web): shared Topbar.vue (brand + nav + sign-out)

Renders the brand block, primary nav (Home / Settings / Admin), and a
sign-out button. Admin link appears only when GET /api/me reports
is_admin. Sign-out calls logout() and unconditionally navigates to
/login.html so the user lands on the auth shell even when the request
fails. 6 vitest cases cover the nav variations + the sign-out flow.
EOF
)"
```

---

## Task 3: ApiTokens tab component

**Files:**
- Create: `web/src/settings/tabs/ApiTokens.vue`
- Create: `web/tests/unit/settings/tabs/ApiTokens.test.ts`

- [ ] **Step 3.1: Write the failing test**

Create `web/tests/unit/settings/tabs/ApiTokens.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  listTokens: vi.fn(),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
}))

import ApiTokens from '@/settings/tabs/ApiTokens.vue'
import { listTokens, createToken, revokeToken } from '@shared/api/me'

describe('ApiTokens.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists active tokens (revoked rows are hidden)', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 't1', name: 'laptop',  prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      { id: 't2', name: 'desktop', prefix: 'atk_bbb', created_at: '2026-01-02T00:00:00Z', revoked_at: '2026-01-05T00:00:00Z' },
    ])
    const wrapper = mount(ApiTokens)
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('laptop')
    expect(text).toContain('atk_aaa')
    expect(text).not.toContain('desktop')
  })

  it('shows an empty-state message when no active tokens', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mount(ApiTokens)
    await flushPromises()

    expect(wrapper.text()).toContain('No tokens yet')
  })

  it('creates a token, shows the plaintext once, and reloads the list', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { id: 't1', name: 'laptop', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createToken as ReturnType<typeof vi.fn>).mockResolvedValue({
      id: 't1', plaintext: 'atk_full_secret_xyz', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z',
    })
    const wrapper = mount(ApiTokens)
    await flushPromises()

    await wrapper.find('input[type="text"]').setValue('laptop')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createToken).toHaveBeenCalledWith('laptop')
    expect(wrapper.text()).toContain('atk_full_secret_xyz')
    expect(listTokens).toHaveBeenCalledTimes(2)
  })

  it('revokes a token and reloads the list', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 't1', name: 'laptop', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      ])
      .mockResolvedValueOnce([])
    ;(revokeToken as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(ApiTokens)
    await flushPromises()

    await wrapper.find('[data-testid="revoke-t1"]').trigger('click')
    await flushPromises()

    expect(revokeToken).toHaveBeenCalledWith('t1')
    expect(listTokens).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('No tokens yet')
  })
})
```

- [ ] **Step 3.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: import error on `@/settings/tabs/ApiTokens.vue` (does not exist).

- [ ] **Step 3.3: Implement `ApiTokens.vue`**

Create `web/src/settings/tabs/ApiTokens.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NList,
  NListItem,
  NThing,
  NInput,
  NButton,
  NSpace,
  NAlert,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listTokens, createToken, revokeToken } from '@shared/api/me'
import type { ApiTokenRow } from '@shared/api/types'

const tokens = ref<ApiTokenRow[]>([])
const newName = ref('')
const creating = ref(false)
const plaintext = ref('')
const loading = ref(true)
const message = useMessage()

function shortDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

function isActive(t: ApiTokenRow): boolean {
  return !t.revoked_at
}

async function reload() {
  loading.value = true
  try {
    tokens.value = await listTokens()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load tokens.')
  } finally {
    loading.value = false
  }
}

async function onCreate(e: Event) {
  e.preventDefault()
  const name = newName.value.trim()
  if (!name || creating.value) return
  creating.value = true
  try {
    const created = await createToken(name)
    plaintext.value = created.plaintext
    newName.value = ''
    await reload()
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'name_required') message.error('Token name is required.')
      else if (e.code === 'invalid_request') message.error('Please enter a valid name.')
      else message.error('Failed to create token.')
    }
  } finally {
    creating.value = false
  }
}

async function onRevoke(id: string) {
  try {
    await revokeToken(id)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to revoke token.')
  }
}

async function copyPlaintext() {
  try {
    await navigator.clipboard.writeText(plaintext.value)
    message.success('Token copied to clipboard.')
  } catch {
    message.warning('Clipboard not available — select and copy manually.')
  }
}

onMounted(reload)
</script>

<template>
  <n-card title="API Tokens" :bordered="false">
    <n-alert v-if="plaintext" type="success" :show-icon="false" class="plaintext-alert">
      <div class="plaintext-msg">Copy this token now — it will not be shown again.</div>
      <code class="plaintext-display">{{ plaintext }}</code>
      <n-button size="small" tertiary class="plaintext-copy" @click="copyPlaintext">Copy</n-button>
    </n-alert>

    <n-list v-if="tokens.filter(isActive).length > 0" bordered>
      <n-list-item v-for="t in tokens.filter(isActive)" :key="t.id">
        <n-thing>
          <template #header>{{ t.name }}</template>
          <template #description>
            <code>{{ t.prefix }}…</code> · created {{ shortDate(t.created_at) }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm @positive-click="onRevoke(t.id)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-${t.id}`">
                Revoke
              </n-button>
            </template>
            Revoke this token? This cannot be undone.
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">No tokens yet.</p>

    <form class="create-form" @submit="onCreate" autocomplete="off">
      <n-space :wrap="false">
        <n-input
          v-model:value="newName"
          type="text"
          placeholder="e.g. my-laptop"
          :input-props="{ required: true, autocomplete: 'off' }"
        />
        <n-button type="primary" attr-type="submit" :loading="creating" :disabled="creating">
          Create
        </n-button>
      </n-space>
    </form>
  </n-card>
</template>

<style scoped>
.plaintext-alert { margin-bottom: 1rem; }
.plaintext-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.plaintext-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.plaintext-copy { margin-top: 0.5rem; }
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.create-form { margin-top: 1rem; }
</style>
```

- [ ] **Step 3.4: Run tests + typecheck**

```bash
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
```
Expected: 48 + 4 = **52 passing**; vue-tsc clean.

- [ ] **Step 3.5: Commit**

```bash
git add web/src/settings/tabs/ApiTokens.vue web/tests/unit/settings/tabs/ApiTokens.test.ts
git commit -m "feat(web): ApiTokens tab (list/create/revoke with plaintext-once display)"
```

---

## Task 4: ChangePassword tab component

**Files:**
- Create: `web/src/settings/tabs/ChangePassword.vue`
- Create: `web/tests/unit/settings/tabs/ChangePassword.test.ts`

- [ ] **Step 4.1: Write the failing test**

Create `web/tests/unit/settings/tabs/ChangePassword.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  changePassword: vi.fn(),
}))

import ChangePassword from '@/settings/tabs/ChangePassword.vue'
import { changePassword } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

describe('ChangePassword.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '#change-password', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('submits current + new password and redirects to /login.html on success', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('old-password-1')
    await wrapper.find('input[autocomplete="new-password"]').setValue('new-password-12')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(changePassword).toHaveBeenCalledWith('old-password-1', 'new-password-12')
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('shows the current-password-wrong message on 401', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'current_password_wrong', null),
    )
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('wrong')
    await wrapper.find('input[autocomplete="new-password"]').setValue('new-password-12')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Current password is incorrect.')
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the password-weak message on 400', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'password_weak', null),
    )
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('old-password-1')
    await wrapper.find('input[autocomplete="new-password"]').setValue('short')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('at least 12 characters')
  })
})
```

- [ ] **Step 4.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: import error.

- [ ] **Step 4.3: Implement `ChangePassword.vue`**

Create `web/src/settings/tabs/ChangePassword.vue`:

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton } from 'naive-ui'
import { changePassword } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

const current = ref('')
const next = ref('')
const submitting = ref(false)
const errorMsg = ref('')

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'current_password_wrong') return 'Current password is incorrect.'
    if (e.code === 'password_weak') return 'New password must be at least 12 characters.'
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Password change failed. Please try again.'
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await changePassword(current.value, next.value)
    // Server invalidated all sessions including ours; the relay just
    // issued a fresh cookie, but the safest UX is to bounce through
    // /login.html so the new credentials are exercised explicitly.
    location.assign('/login.html')
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <n-card title="Change Password" :bordered="false">
    <form @submit="onSubmit" autocomplete="off" novalidate>
      <n-form label-placement="top" require-mark-placement="right-hanging">
        <n-form-item label="Current password" :show-feedback="false">
          <n-input
            v-model:value="current"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'current-password' }"
          />
        </n-form-item>
        <n-form-item label="New password (min 12 characters)" :show-feedback="false">
          <n-input
            v-model:value="next"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
          />
        </n-form-item>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="submitting"
          :disabled="submitting"
        >
          Update password
        </n-button>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
      </n-form>
    </form>
  </n-card>
</template>

<style scoped>
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
```

- [ ] **Step 4.4: Run tests + typecheck**

```bash
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
```
Expected: 52 + 3 = **55 passing**; vue-tsc clean.

- [ ] **Step 4.5: Commit**

```bash
git add web/src/settings/tabs/ChangePassword.vue web/tests/unit/settings/tabs/ChangePassword.test.ts
git commit -m "feat(web): ChangePassword tab (current+new, redirect on success)"
```

---

## Task 5: Sessions tab component

**Files:**
- Create: `web/src/settings/tabs/Sessions.vue`
- Create: `web/tests/unit/settings/tabs/Sessions.test.ts`

- [ ] **Step 5.1: Write the failing test**

Create `web/tests/unit/settings/tabs/Sessions.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
  signOutOthers: vi.fn(),
}))

import Sessions from '@/settings/tabs/Sessions.vue'
import { listSessions, revokeSession, signOutOthers } from '@shared/api/me'

const baseTime = Date.UTC(2026, 0, 1)

describe('Sessions.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists sessions and marks the current one', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id_hash: 'h-current', user_agent: 'Chrome/120',  ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      { id_hash: 'h-other',   user_agent: 'Firefox/125', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
    ])
    const wrapper = mount(Sessions)
    await flushPromises()

    expect(wrapper.text()).toContain('Chrome')
    expect(wrapper.text()).toContain('Firefox')
    expect(wrapper.text()).toContain('this device')
    // Only the non-current row should have a Revoke button.
    expect(wrapper.findAll('[data-testid^="revoke-session-"]').length).toBe(1)
  })

  it('revokes a single session and reloads the list', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
        { id_hash: 'h-other',   user_agent: 'Firefox', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
      ])
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      ])
    ;(revokeSession as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(Sessions)
    await flushPromises()

    await wrapper.find('[data-testid="revoke-session-h-other"]').trigger('click')
    await flushPromises()

    expect(revokeSession).toHaveBeenCalledWith('h-other')
    expect(listSessions).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('Firefox')
  })

  it('sign-out-others POSTs and reloads', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
        { id_hash: 'h-other',   user_agent: 'Firefox', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
      ])
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      ])
    ;(signOutOthers as ReturnType<typeof vi.fn>).mockResolvedValue({ deleted: 1 })
    const wrapper = mount(Sessions)
    await flushPromises()

    await wrapper.find('[data-testid="sign-out-others"]').trigger('click')
    await flushPromises()

    expect(signOutOthers).toHaveBeenCalled()
    expect(listSessions).toHaveBeenCalledTimes(2)
  })
})
```

- [ ] **Step 5.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```

- [ ] **Step 5.3: Implement `Sessions.vue`**

Create `web/src/settings/tabs/Sessions.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NList,
  NListItem,
  NThing,
  NButton,
  NTag,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listSessions, revokeSession, signOutOthers } from '@shared/api/me'
import type { SessionRow } from '@shared/api/types'

const rows = ref<SessionRow[]>([])
const loading = ref(true)
const signingOut = ref(false)
const message = useMessage()

function describeUA(ua: string): string {
  if (!ua) return 'Unknown device'
  if (ua.includes('Firefox')) return 'Firefox'
  if (ua.includes('Edg/')) return 'Edge'
  if (ua.includes('Chrome')) return 'Chrome'
  if (ua.includes('Safari')) return 'Safari'
  return ua.length > 40 ? ua.slice(0, 40) + '…' : ua
}

function describeWhen(ms: number): string {
  try {
    return new Date(ms).toLocaleString()
  } catch {
    return ''
  }
}

async function reload() {
  loading.value = true
  try {
    rows.value = await listSessions()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load sessions.')
  } finally {
    loading.value = false
  }
}

async function onRevoke(idHash: string) {
  try {
    await revokeSession(idHash)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Revoke failed.')
  }
}

async function onSignOutOthers() {
  if (signingOut.value) return
  signingOut.value = true
  try {
    const result = await signOutOthers()
    message.success(`Signed out ${result.deleted} other device${result.deleted === 1 ? '' : 's'}.`)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Sign-out-others failed.')
  } finally {
    signingOut.value = false
  }
}

onMounted(reload)
</script>

<template>
  <n-card title="Signed-in devices" :bordered="false">
    <p class="muted">Each row is a browser or PWA where this account is signed in.</p>
    <n-list v-if="rows.length > 0" bordered>
      <n-list-item v-for="row in rows" :key="row.id_hash">
        <n-thing>
          <template #header>
            {{ describeUA(row.user_agent) }}
            <n-tag v-if="row.is_current" type="success" size="small" round style="margin-left: 0.5rem;">
              this device
            </n-tag>
          </template>
          <template #description>
            signed in {{ describeWhen(row.created_at) }} · {{ row.ip_prefix || 'ip unknown' }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm v-if="!row.is_current" @positive-click="onRevoke(row.id_hash)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-session-${row.id_hash}`">
                Revoke
              </n-button>
            </template>
            Revoke this device? You'll need to sign in again on it.
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">No active sessions.</p>

    <div class="actions">
      <n-popconfirm @positive-click="onSignOutOthers">
        <template #trigger>
          <n-button
            type="error"
            :loading="signingOut"
            :disabled="signingOut"
            data-testid="sign-out-others"
          >
            Sign out everywhere except this device
          </n-button>
        </template>
        Sign out every other device? They'll all need to sign in again.
      </n-popconfirm>
    </div>
  </n-card>
</template>

<style scoped>
.muted { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.actions { margin-top: 1rem; }
</style>
```

- [ ] **Step 5.4: Run tests + typecheck**

```bash
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
```
Expected: 55 + 3 = **58 passing**; vue-tsc clean.

- [ ] **Step 5.5: Commit**

```bash
git add web/src/settings/tabs/Sessions.vue web/tests/unit/settings/tabs/Sessions.test.ts
git commit -m "feat(web): Sessions tab (list, revoke single, sign-out-others)"
```

---

## Task 6: DangerZone tab component

**Files:**
- Create: `web/src/settings/tabs/DangerZone.vue`
- Create: `web/tests/unit/settings/tabs/DangerZone.test.ts`

- [ ] **Step 6.1: Write the failing test**

Create `web/tests/unit/settings/tabs/DangerZone.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  deleteMe: vi.fn(),
}))

import DangerZone from '@/settings/tabs/DangerZone.vue'
import { deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

describe('DangerZone.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '#danger', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('submits email + password and redirects to /login.html on success', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(DangerZone)

    await wrapper.find('input[type="email"]').setValue('a@b.example')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(deleteMe).toHaveBeenCalledWith('a@b.example', 'password-1234')
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('shows the email-mismatch message on 400', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'email_mismatch', null),
    )
    const wrapper = mount(DangerZone)

    await wrapper.find('input[type="email"]').setValue('wrong@b.example')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain("Email doesn't match")
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the password-incorrect message on 401', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'password_incorrect', null),
    )
    const wrapper = mount(DangerZone)

    await wrapper.find('input[type="email"]').setValue('a@b.example')
    await wrapper.find('input[autocomplete="current-password"]').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Password is incorrect.')
  })

  it('shows the last-admin guard message on 409', async () => {
    ;(deleteMe as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'last_admin', null),
    )
    const wrapper = mount(DangerZone)

    await wrapper.find('input[type="email"]').setValue('a@b.example')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('last admin')
  })
})
```

- [ ] **Step 6.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```

- [ ] **Step 6.3: Implement `DangerZone.vue`**

Create `web/src/settings/tabs/DangerZone.vue`:

```vue
<script setup lang="ts">
import { ref } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton, NPopconfirm } from 'naive-ui'
import { deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

const email = ref('')
const password = ref('')
const submitting = ref(false)
const errorMsg = ref('')

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_mismatch') return "Email doesn't match — type your exact email."
    if (e.code === 'password_incorrect') return 'Password is incorrect.'
    if (e.code === 'last_admin') return "You're the last admin — promote another user first."
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Delete failed. Please try again.'
}

async function performDelete() {
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await deleteMe(email.value.trim(), password.value)
    location.assign('/login.html')
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

function onSubmit(e: Event) {
  // Form submit is intercepted by the popconfirm; this handler only
  // exists to call preventDefault() when the browser dispatches the
  // submit event from pressing Enter inside an <input>.
  e.preventDefault()
}
</script>

<template>
  <n-card title="Danger zone" :bordered="false" class="danger-card">
    <p>
      Permanently delete this account. This cannot be undone. API tokens, web
      sessions, and account data are removed. Invitations you've consumed
      stay (their "consumed by" field is cleared).
    </p>
    <form @submit="onSubmit" autocomplete="off" novalidate>
      <n-form label-placement="top" require-mark-placement="right-hanging">
        <n-form-item label="Confirm by typing your full email" :show-feedback="false">
          <n-input
            v-model:value="email"
            type="text"
            :input-props="{ type: 'email', required: true, autocomplete: 'off' }"
          />
        </n-form-item>
        <n-form-item label="Current password" :show-feedback="false">
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'current-password' }"
          />
        </n-form-item>
        <n-popconfirm @positive-click="performDelete">
          <template #trigger>
            <n-button
              type="error"
              attr-type="submit"
              :loading="submitting"
              :disabled="submitting"
            >
              Delete my account
            </n-button>
          </template>
          Permanently delete this account? This cannot be undone.
        </n-popconfirm>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
      </n-form>
    </form>
  </n-card>
</template>

<style scoped>
.danger-card :deep(.n-card-header__main) { color: var(--bad); }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
```

- [ ] **Step 6.4: Run tests + typecheck**

```bash
cd web && npm test 2>&1 | tail -15
cd web && npx vue-tsc --noEmit
```
Expected: 58 + 4 = **62 passing**; vue-tsc clean.

If the Naive UI Popconfirm intercepts the submit and the test ends up clicking the *trigger* button without confirming, switch the test to drive the confirm flow:

```ts
await wrapper.find('form').trigger('submit')   // popconfirm pops
await wrapper.find('.n-popconfirm__action .n-button--primary-type').trigger('click')
await flushPromises()
```

Apply this adjustment uniformly across the 4 DangerZone tests if Naive UI's popconfirm DOM differs from what you see at first run.

- [ ] **Step 6.5: Commit**

```bash
git add web/src/settings/tabs/DangerZone.vue web/tests/unit/settings/tabs/DangerZone.test.ts
git commit -m "feat(web): DangerZone tab (delete account with email + password confirm)"
```

---

## Task 7: Settings App.vue + entry HTML + vite.config.ts + embed sync

**Files:**
- Create: `web/src/settings/main.ts`
- Create: `web/src/settings/App.vue`
- Create: `web/settings.html`
- Modify: `web/vite.config.ts`
- Create: `web/tests/unit/settings/App.test.ts`
- Regenerate: `internal/relay/web-dist/**`

- [ ] **Step 7.1: Write `main.ts`**

Create `web/src/settings/main.ts`:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'

createApp(App).mount('#app')
```

- [ ] **Step 7.2: Write `App.vue`**

Create `web/src/settings/App.vue`:

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
import ApiTokens from './tabs/ApiTokens.vue'
import ChangePassword from './tabs/ChangePassword.vue'
import Sessions from './tabs/Sessions.vue'
import DangerZone from './tabs/DangerZone.vue'

const TAB_NAMES = ['api-tokens', 'change-password', 'sessions', 'danger'] as const
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return TAB_NAMES.includes(h as TabName) ? (h as TabName) : 'api-tokens'
}

const activeTab = ref<TabName>(nameFromHash())

function onHashChange() {
  activeTab.value = nameFromHash()
}

onMounted(() => window.addEventListener('hashchange', onHashChange))
onUnmounted(() => window.removeEventListener('hashchange', onHashChange))

function onTabChange(name: string) {
  if (!TAB_NAMES.includes(name as TabName)) return
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
          <n-tab-pane name="api-tokens" tab="API Tokens">
            <ApiTokens />
          </n-tab-pane>
          <n-tab-pane name="change-password" tab="Change Password">
            <ChangePassword />
          </n-tab-pane>
          <n-tab-pane name="sessions" tab="Signed-in devices">
            <Sessions />
          </n-tab-pane>
          <n-tab-pane name="danger" tab="Danger zone">
            <DangerZone />
          </n-tab-pane>
        </n-tabs>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.settings-page {
  max-width: 720px;
  margin: 0 auto;
  padding: 2rem 1rem;
  background: var(--bg);
  color: var(--fg);
  min-height: calc(100vh - 80px);
}
</style>
```

- [ ] **Step 7.3: Write `web/settings.html`**

Create `web/settings.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="page" content="settings" />
  <meta name="theme-color" content="#0b1020" />
  <title>AT Term · settings</title>
  <link rel="icon" href="/icon.svg" type="image/svg+xml" />
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/settings/main.ts"></script>
</body>
</html>
```

- [ ] **Step 7.4: Add the settings entry to `vite.config.ts`**

Update the `rollupOptions.input` block in `web/vite.config.ts`:

```ts
    rollupOptions: {
      input: {
        login:    fileURLToPath(new URL('./login.html',    import.meta.url)),
        signup:   fileURLToPath(new URL('./signup.html',   import.meta.url)),
        settings: fileURLToPath(new URL('./settings.html', import.meta.url)),
      },
    },
```

Leave the rest of the config alone.

- [ ] **Step 7.5: Write the Settings App component test**

Create `web/tests/unit/settings/App.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false }),
  listTokens: vi.fn().mockResolvedValue([]),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  listSessions: vi.fn().mockResolvedValue([]),
  revokeSession: vi.fn(),
  signOutOthers: vi.fn(),
  changePassword: vi.fn(),
  deleteMe: vi.fn(),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))

import App from '@/settings/App.vue'

describe('Settings App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '', assign: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders all four tab labels', async () => {
    const wrapper = mount(App)
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('API Tokens')
    expect(text).toContain('Change Password')
    expect(text).toContain('Signed-in devices')
    expect(text).toContain('Danger zone')
  })

  it('opens the tab indicated by the hash on first paint', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#sessions' },
      writable: true,
    })
    const wrapper = mount(App)
    await flushPromises()
    // Naive UI marks the active pane with role=tabpanel; verify the
    // Sessions tab content (which calls listSessions) mounted.
    expect(wrapper.text()).toContain('Each row is a browser')
  })

  it('falls back to the api-tokens tab when hash is missing or invalid', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#bogus-tab' },
      writable: true,
    })
    const wrapper = mount(App)
    await flushPromises()
    // ApiTokens panel renders the empty-state message.
    expect(wrapper.text()).toContain('No tokens yet.')
  })
})
```

- [ ] **Step 7.6: Run tests + build**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -10
cd web && npm run build 2>&1 | tail -15
cd ..
ls web/dist
```
Expected:
- vitest: 62 + 3 = **65 passing**
- `npm run build` succeeds; `web/dist/` contains `login.html`, `signup.html`, `settings.html` and `assets/`

If the App.test.ts cases for tab routing struggle because Naive UI's `<n-tabs>` lazy-mounts panes, drive interaction via the hash-change event instead:

```ts
Object.defineProperty(window, 'location', {
  value: { ...window.location, hash: '#sessions' },
  writable: true,
})
window.dispatchEvent(new HashChangeEvent('hashchange'))
await flushPromises()
```

- [ ] **Step 7.7: Sync embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
./scripts/build-web.sh
git status --short -- internal/relay/web-dist | head -10
```
Expected: `internal/relay/web-dist/settings.html` now contains `<div id="app">` (Vue version), and `internal/relay/web-dist/assets/` gains new hashed bundles for the settings entry.

```bash
grep -l '<div id="app">' internal/relay/web-dist/*.html
```
Expected: login.html, signup.html, settings.html (and NOT index.html or admin/index.html — those are still legacy).

- [ ] **Step 7.8: Determinism re-run**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 7.9: Commit (Vite entry + embed regen in same commit)**

```bash
git add web/src/settings web/settings.html web/vite.config.ts web/tests/unit/settings/App.test.ts internal/relay/web-dist
git commit -m "$(cat <<'EOF'
feat(web): settings.html Vue entry with 4 tabs (NTabs + hash routing)

App.vue wraps the dark Naive UI theme, the shared Topbar, and an
NTabs binding active value to location.hash (so /settings.html#sessions
deep-links to the Signed-in devices tab). The 4 tab components render
inside NTabPane panels; each one already has its own unit tests from
prior tasks. Vite multi-entry input gains the settings entry.

The embed is regenerated in the same commit so the drift gate stays
green and the new HTML/assets are byte-identical to the build output.
EOF
)"
```

---

## Task 8: Delete legacy settings sources + SW cache bump + legacy test trim

**Files:**
- Delete: `web/legacy/settings.html`
- Delete: `web/legacy/settings.js`
- Delete: `web/legacy/settings-sessions.js`
- Delete: `web/legacy/settings-danger.js`
- Delete: `web/legacy/settings.test.mjs`
- Modify: `web/legacy/sw.js`
- Modify: `web/legacy/no-raw-colors.test.mjs`
- Modify: `web/legacy/terminal-fit.test.mjs` (if it asserts on `./settings.js`)
- Regenerate: `internal/relay/web-dist/**`

- [ ] **Step 8.1: Delete the legacy settings sources**

```bash
git rm web/legacy/settings.html web/legacy/settings.js web/legacy/settings-sessions.js web/legacy/settings-danger.js
test -f web/legacy/settings.test.mjs && git rm web/legacy/settings.test.mjs || echo "no settings.test.mjs to delete"
```

- [ ] **Step 8.2: Update `web/legacy/sw.js` ASSETS**

Edit `web/legacy/sw.js`. Remove these three entries from the `ASSETS` array:

```js
  "./settings.js",
  "./settings-danger.js",
  "./settings-sessions.js",
```

The resulting `ASSETS` array becomes:

```js
const ASSETS = [
  "./",
  "./admin/admin-invitations.js",
  "./admin/admin-users.js",
  "./admin/admin.js",
  "./app-core.js",
  "./app.js",
  "./layout.js",
  "./style.css",
  "./vendor/xterm/xterm.css",
  "./vendor/xterm/xterm.js",
  "./vendor/xterm-addon-fit/xterm-addon-fit.js",
  "./manifest.webmanifest",
  "./icon.png",
  "./icon.svg",
];
```

- [ ] **Step 8.3: Compute the new SW cache hash**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
node --test web/legacy/sw-cache-bump.test.mjs 2>&1 | tail -10
```
Expected: test fails with `Replace it with "at-term-web-<NEWHEX>"`. Copy that hex.

Edit `web/legacy/sw.js`. Replace:

```js
const CACHE = "at-term-web-fa11adad";
```

with the new value, e.g.:

```js
const CACHE = "at-term-web-NEWHEX99";
```

- [ ] **Step 8.4: Confirm the bump test passes**

```bash
node --test web/legacy/sw-cache-bump.test.mjs 2>&1 | tail -5
```
Expected: passes.

- [ ] **Step 8.5: Trim `no-raw-colors.test.mjs`**

```bash
grep -n '"web/legacy/settings.html"\|"web/legacy/settings-' web/legacy/no-raw-colors.test.mjs
```

If any matches appear, remove the corresponding entries from the `ALLOWED` map (whose values are arrays of acceptable color literals). After editing, run:

```bash
node --test web/legacy/no-raw-colors.test.mjs 2>&1 | tail -5
```
Expected: passes.

- [ ] **Step 8.6: Trim `terminal-fit.test.mjs` if it references settings**

```bash
grep -n 'settings\.js\|settings-' web/legacy/terminal-fit.test.mjs
```

If any matches appear, remove the matching assertions following the same pattern as PR-B Task 9 (preserve the spirit of the assertion: the SW must precache *some* bootstrap module for the remaining legacy entries). After editing:

```bash
node --test web/legacy/terminal-fit.test.mjs 2>&1 | tail -5
```
Expected: passes.

- [ ] **Step 8.7: Full legacy + contract + vitest sweep**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
node --test web/legacy/*.test.mjs 2>&1 | tail -5
node --test web/tests/contract/*.test.mjs 2>&1 | tail -5
(cd web && npm test 2>&1) | tail -5
```
All green. Legacy count drops further (settings-related tests gone); contract stays 10; vitest stays 65.

- [ ] **Step 8.8: Sync embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
./scripts/build-web.sh
git status --short -- internal/relay/web-dist | head
```
Expected: diff shows `internal/relay/web-dist/sw.js` updated AND `internal/relay/web-dist/settings.js`, `settings-sessions.js`, `settings-danger.js` deleted (Layer 1 rsync no longer copies them; Layer 2 didn't replace those filenames — they don't exist in the Vite build).

Stage the diff:

```bash
git add internal/relay/web-dist
```

Verify determinism:

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 8.9: Commit**

```bash
git add web/legacy/sw.js web/legacy/no-raw-colors.test.mjs web/legacy/terminal-fit.test.mjs web/legacy/settings.html web/legacy/settings.js web/legacy/settings-sessions.js web/legacy/settings-danger.js internal/relay/web-dist
# (settings.test.mjs only if it existed and was deleted)
git commit -m "$(cat <<'EOF'
chore(web): drop legacy settings sources; bump SW cache name

Vite-built settings.html lands in web-dist via the overlay in
scripts/build-web.sh, so the legacy copies are dead code. sw.js's
ASSETS no longer references settings.js / settings-sessions.js /
settings-danger.js, and the CACHE hash bumps so installed PWA clients
re-fetch on next activation. Legacy contract tests that named those
files in their allow lists are trimmed accordingly.
EOF
)"
```

---

## Task 9: Final smoke + PR

**Files:**
- (No file changes; runs the full gate and opens the PR.)

- [ ] **Step 9.1: Full test matrix**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./...
go test ./...
(cd web && npm run build && npm test && npm run test:contract)
node --test web/legacy/*.test.mjs
```
All must be green.

- [ ] **Step 9.2: Drift gate**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 9.3: Working-tree clean check**

```bash
git status --short
```
Expected: only the pre-existing untracked artifacts.

- [ ] **Step 9.4: End-to-end relay smoke**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3

echo '--- settings.html ---'
curl -sI http://127.0.0.1:18080/settings.html | head -1                          # expect 200
curl -s  http://127.0.0.1:18080/settings.html | grep -c '<div id="app">'         # expect 1+
curl -s  http://127.0.0.1:18080/settings.html | grep -c 'id="login-form"'        # expect 0

echo '--- /api/me/tokens unauthenticated 401 ---'
curl -sI http://127.0.0.1:18080/api/me/tokens | head -1                          # expect 401

echo '--- /api/me/sessions unauthenticated 401 ---'
curl -sI http://127.0.0.1:18080/api/me/sessions | head -1                        # expect 401

echo '--- / and /admin/ still redirect ---'
curl -sI http://127.0.0.1:18080/        | head -1                                # 302
curl -sI http://127.0.0.1:18080/admin/  | head -1                                # 302

echo '--- previous PR-B Vue pages still serve ---'
curl -sI http://127.0.0.1:18080/login.html  | head -1                            # 200
curl -sI http://127.0.0.1:18080/signup.html | head -1                            # 200

kill $RELAY_PID 2>/dev/null
wait $RELAY_PID 2>/dev/null
```

Each line must produce the expected HTTP status. Stop the relay before continuing.

- [ ] **Step 9.5: Push and open PR**

```bash
git push -u origin web/vue-rewrite-pr-c-settings
PATH=/opt/homebrew/bin:$PATH gh pr create --title "web: PR-C settings 4 tabs (vue + naive-ui)" --body "$(cat <<'EOF'
## Summary

PR-C of the web client rewrite. Replaces `/settings.html` with a Vue 3 + Naive UI version. Four tabs (API Tokens, Change Password, Signed-in devices, Danger zone) wired via NTabs with hash-synced active state.

- `api/me.ts` gains the eight `/api/me/*` helpers (token CRUD, session list/revoke/sign-out-others, password change, account delete) with full vitest coverage (12 cases)
- Shared `Topbar.vue` (brand + Home / Settings / Admin nav + sign-out) with 6-case test coverage; admin link is conditional on `is_admin`
- Four tab components, each isolated under `web/src/settings/tabs/` with their own vitest coverage (`ApiTokens` 4, `ChangePassword` 3, `Sessions` 3, `DangerZone` 4)
- Settings `App.vue` wraps the dark theme + NMessageProvider + NTabs; `location.hash` drives the active tab so `/settings.html#sessions` still deep-links
- `vite.config.ts` adds the `settings` entry; build emits hashed bundles in `web/dist/assets/`
- Legacy `web/legacy/settings*` and `settings*.{js,html}` deleted; `sw.js` ASSETS trimmed and CACHE bumped; `no-raw-colors` / `terminal-fit` allow lists trimmed

Spec: `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`
Plan: `docs/superpowers/plans/2026-05-17-web-vue-rewrite-pr-c-settings.md`

## Test Plan

- [x] `go vet ./... && go test ./...`
- [x] `cd web && npm run build && npm test && npm run test:contract`
- [x] `node --test web/legacy/*.test.mjs`
- [x] Manual smoke: `curl /settings.html` returns Vue shell with `<div id="app">`; `/api/me/tokens` and `/api/me/sessions` return 401 anonymously; `/login.html`, `/signup.html`, `/`, `/admin/` still match PR-B's behaviour
- [x] `./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist` — exit 0
- [ ] Manual browser smoke: sign in with the bootstrap admin, switch through all four tabs, create + revoke a token, change password, sign out everywhere
- [ ] CI green (web-tests, web-vue-tests with contract step, build-linux, etc.)
EOF
)"
```

---

## Self-Review Notes

**Spec coverage check:**

- Spec § Architecture / Routing — MPA: settings entry added (Task 7), other entries unchanged
- Spec § Architecture / UI theme: App.vue wraps `<n-config-provider>` + `<n-message-provider>` using `getNaiveOverrides()` (Task 7)
- Spec § Architecture / State: per-component `ref`/`reactive`, no Pinia — followed across Tasks 3-6
- Spec § Architecture / Auth & API: new `/api/me/*` helpers ride apiFetch + CSRF cache from PR-B (Task 1)
- Spec § Architecture / WebSocket / proto: no WS work in PR-C — deferred
- Spec § Testing / Unit tests: 35 new vitest cases (12 me.ts + 6 Topbar + 4 ApiTokens + 3 ChangePassword + 3 Sessions + 4 DangerZone + 3 App)
- Spec § Testing / Contract tests: existing contract suite picks up the new settings.html automatically via `no-inline-script` walker
- Spec § Phasing — Phase B item 2 (settings): entire plan
- Spec § Invariants — no `v-html`: every tab component avoids it
- Spec § Security / Sec-1 (CSP): no change; existing `style-src 'self' 'unsafe-inline'` covers Naive UI
- Spec § Security / Sec-2 (safeNext): no new consumer in PR-C (settings doesn't take `?next=` because users are already authenticated to be there)
- Spec § Security / Sec-3 (CSWSH): not exercised in PR-C
- Spec § Security / Sec-4 (CSRF): all 8 new helpers use `apiFetch` so the cached `X-CSRF-Token` is injected on non-GET (verified by me.test.ts)
- Spec § Security / Sec-5 (SW precache): deferred (vite-plugin-pwa setup belongs to later PR; PR-C only bumps the legacy SW like PR-B did)
- Spec § Security / Sec-6 (supply chain): no new deps in PR-C
- Spec § Security / Sec-7 (build determinism): Task 7's drift gate exercises it; CI gate inherited
- Spec § Security / Sec-8 (paste image size): deferred to PR-E

**Placeholder scan:** every step has concrete code or commands; no "TBD" / "implement later" / "add appropriate error handling" / unnamed types.

**Type consistency:**

- `apiFetch<T>` and `ApiError` reused unchanged from PR-B
- `ApiTokenRow`, `ApiTokenCreated`, `SessionRow`, `SignOutOthersResponse` all defined in `types.ts` (Task 1) and consumed identically in `me.ts`, tab components, and tests
- `listTokens(): Promise<ApiTokenRow[]>`, `createToken(name: string): Promise<ApiTokenCreated>`, `revokeToken(id: string): Promise<void>` — same signatures everywhere they appear
- `listSessions`, `revokeSession`, `signOutOthers` — same
- `changePassword(current_password: string, new_password: string): Promise<void>` — same in me.ts, ChangePassword.vue, and test
- `deleteMe(email: string, password: string): Promise<void>` — same in me.ts, DangerZone.vue, and test
- `Topbar` prop `active: 'home' | 'settings' | 'admin'` — same in declaration, settings/App.vue use, and Topbar.test.ts
- Tab name union `'api-tokens' | 'change-password' | 'sessions' | 'danger'` — same in App.vue and `n-tab-pane name=` attributes

No drift detected.
