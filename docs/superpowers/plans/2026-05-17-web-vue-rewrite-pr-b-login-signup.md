# Web Vue Rewrite — PR-B: Login + Signup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the vanilla `/login.html` and `/signup.html` pages with Vue 3 + Naive UI entries served from the relay's embedded FS, and land the full `apiFetch` + CSRF-cache + auth helpers that every later PR will build on.

**Architecture:** Two new Vite entries (`web/login.html`, `web/signup.html`) each mount a tiny Vue 3 + Naive UI app. The first real `apiFetch` implementation lands in `web/src/shared/api/client.ts` alongside an in-memory CSRF singleton (the relay returns the token in `/api/me`'s response body; double-submit cookie was incorrect — see spec Sec-4 amendment). After build, `scripts/build-web.sh` overlays the new HTML into `internal/relay/web-dist/`, replacing the legacy versions that PR-A copied over. The relay continues to gate `/` by cookie; nothing on the server changes.

**Tech Stack:** Vue 3, TypeScript, Naive UI, Vite 5, Vitest, `@vue/test-utils`, happy-dom. Node 20.

**Reference spec:** `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md` — note Sec-4 was amended at the start of PR-B to describe the real CSRF model.

**Pre-flight:**
- Branch: `web/vue-rewrite-pr-b-login-signup` (already cut from `origin/main` at the start of PR-B; this plan assumes you start with the spec-amend commit `8a718c4` as the tip).
- PR-A merged into `main` as `ca5a114` (#45). Vite scaffold and embed pipeline are live.
- Bootstrap admin envs still required to run the relay locally:
  ```
  ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com'
  ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!'
  ```

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `web/src/shared/api/client.ts` | Modify | Replace the stub with full `apiFetch` + `ApiError` + module-level CSRF cache (`setCsrfToken`, `clearCsrfToken`); `safeNext` is preserved verbatim |
| `web/src/shared/api/auth.ts` | Create | `login(email, password)`, `signup(email, password, invite_code)`, `logout()` — auth-side helpers, populate/clear the CSRF cache around `getMe` |
| `web/src/shared/api/me.ts` | Create | `getMe()` → `/api/me` → `{ user_id, email, csrf_token?, is_admin? }`; writes `csrf_token` into the cache when present |
| `web/src/shared/api/version.ts` | Create | `fetchVersionLabel()` → `/api/version` → string for the page footer |
| `web/src/shared/api/types.ts` | Create | DTO mirrors: `AuthSuccess`, `MeResponse`, `VersionResponse` |
| `web/src/login/main.ts` | Create | Mounts the Login app into `#app` |
| `web/src/login/App.vue` | Create | Email/password form, error handling, post-success redirect via `safeNext(?next=)` |
| `web/src/signup/main.ts` | Create | Mounts the Signup app into `#app` |
| `web/src/signup/App.vue` | Create | Email/password/invite_code form, same redirect logic |
| `web/login.html` | Create | Vite entry, replaces `web/legacy/login.html` |
| `web/signup.html` | Create | Vite entry, replaces `web/legacy/signup.html` |
| `web/src/_placeholder.html` | Delete | Replaced by real entries |
| `web/vite.config.ts` | Modify | Entries → `{ login, signup }`; add `server.proxy` for dev |
| `web/tests/unit/shared/api/client.test.ts` | Modify | Add CSRF cache + ApiError + 401-redirect tests (keep existing safeNext tests) |
| `web/tests/unit/shared/api/auth.test.ts` | Create | login/signup/logout integration with apiFetch (mocked) |
| `web/tests/unit/login/App.test.ts` | Create | Component test for Login.vue (mocked auth.login) |
| `web/tests/unit/signup/App.test.ts` | Create | Component test for Signup.vue (mocked auth.signup) |
| `web/tests/contract/auth-pages.test.mjs` | Create | Migrated from `web/legacy/`; asserts against `internal/relay/web-dist/login.html` + `signup.html` |
| `web/tests/contract/no-inline-script.test.mjs` | Create | Migrated; scans `internal/relay/web-dist/**/*.html` recursively |
| `web/legacy/login.html` | Delete | Replaced |
| `web/legacy/login.js` | Delete | Replaced |
| `web/legacy/signup.html` | Delete | Replaced |
| `web/legacy/signup.js` | Delete | Replaced |
| `web/legacy/auth-pages.test.mjs` | Delete | Replaced by contract test |
| `web/legacy/no-inline-script.test.mjs` | Delete | Replaced by contract test |
| `web/legacy/sw.js` | Modify | Drop `./login.js`, `./signup.js` from `ASSETS`; bump `CACHE` hash |
| `web/package.json` | Modify | `test:contract` script + scripts/build-web.sh integration |
| `.github/workflows/build.yml` | Modify | `web-vue-tests` job: add `npm run test:contract` step after build |
| `internal/relay/web-dist/**` | Regenerate | Via `./scripts/build-web.sh`; new login/signup overlay legacy |

---

## Pre-flight

- [ ] **Step 0.1: Confirm baseline**

Run: `git status --short && git rev-parse --abbrev-ref HEAD`
Expected: branch `web/vue-rewrite-pr-b-login-signup`, working tree only has the ignored untracked artifacts (`.claude/`, `.playwright-mcp/`, `atterm-relay`, two unrelated draft plans). Last commit should be `8a718c4` (spec CSRF model amendment).

- [ ] **Step 0.2: Confirm dependencies are installed**

Run: `[ -d web/node_modules ] && echo OK || echo "run: cd web && npm ci --ignore-scripts"`
If output is "run: ...", do that.

- [ ] **Step 0.3: Confirm legacy site still served from embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3
curl -sI http://127.0.0.1:18080/login.html | head -1
curl -sI http://127.0.0.1:18080/signup.html | head -1
kill $RELAY_PID 2>/dev/null
```
Expected: both `HTTP/1.1 200 OK`. Confirms PR-A baseline is intact.

---

## Task 1: Implement full `apiFetch` + CSRF cache + `ApiError` (TDD)

**Files:**
- Modify: `web/src/shared/api/client.ts`
- Modify: `web/tests/unit/shared/api/client.test.ts`

The current `apiFetch` is a stub that throws. We replace it with the real implementation: CSRF cache, header injection, 401 auto-redirect (skipping auth pages), `ApiError` class, content-type aware JSON parsing.

- [ ] **Step 1.1: Extend the existing test file (red phase) — CSRF cache**

Open `web/tests/unit/shared/api/client.test.ts`. Keep the existing `describe('safeNext', ...)` block unchanged. After it, add:

```ts
import { afterEach, beforeEach, vi } from 'vitest'
import {
  ApiError,
  apiFetch,
  clearCsrfToken,
  setCsrfToken,
} from '@shared/api/client'

function makeResponse(status: number, body: unknown, contentType = 'application/json'): Response {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(text, { status, headers: { 'Content-Type': contentType } })
}

describe('apiFetch CSRF cache', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('omits X-CSRF-Token on GET requests even when cached', async () => {
    setCsrfToken('cached-secret')
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('injects X-CSRF-Token on POST requests when cache is populated', async () => {
    setCsrfToken('cached-secret')
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me/password', { method: 'POST', body: JSON.stringify({ old: 'x', new: 'y' }) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('X-CSRF-Token')).toBe('cached-secret')
  })

  it('does not inject X-CSRF-Token when cache is empty (login flow)', async () => {
    clearCsrfToken()
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u', email: 'e' }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({ email: 'e', password: 'p' }) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('sets JSON Content-Type when body present and not pre-set', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({}) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('Content-Type')).toBe('application/json')
  })
})

describe('apiFetch error handling', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('throws ApiError with code from JSON body on 4xx', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(401, { error: 'invalid_credentials' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({}) }),
    ).rejects.toMatchObject({ status: 401, code: 'invalid_credentials' })
  })

  it('throws ApiError with code "http_error" when body is not JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('Bad Request', { status: 400 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiFetch('/api/me')).rejects.toMatchObject({ status: 400, code: 'http_error' })
  })

  it('throws ApiError with status 0 on network failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    await expect(apiFetch('/api/me')).rejects.toMatchObject({ status: 0, code: 'network_error' })
  })
})

describe('apiFetch 401 redirect', () => {
  let originalLocation: Location

  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
    originalLocation = window.location
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  function stubLocation(pathname: string, search = ''): { assign: ReturnType<typeof vi.fn> } {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: {
        origin: 'http://localhost',
        pathname,
        search,
        hash: '',
        assign,
      },
      writable: true,
    })
    return { assign }
  }

  it('redirects to /login.html?next= on 401 when current page is not auth', async () => {
    const { assign } = stubLocation('/settings.html', '?tab=tokens')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(401, { error: 'unauthenticated' })))

    await expect(apiFetch('/api/me')).rejects.toBeInstanceOf(ApiError)

    expect(assign).toHaveBeenCalledTimes(1)
    expect(assign.mock.calls[0]![0]).toBe(
      '/login.html?next=' + encodeURIComponent('/settings.html?tab=tokens'),
    )
  })

  it('does NOT redirect on 401 when current page is /login.html', async () => {
    const { assign } = stubLocation('/login.html')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(401, { error: 'invalid_credentials' })))

    await expect(
      apiFetch('/api/auth/login', { method: 'POST', body: '{}' }),
    ).rejects.toBeInstanceOf(ApiError)

    expect(assign).not.toHaveBeenCalled()
  })

  it('does NOT redirect on 401 when current page is /signup.html', async () => {
    const { assign } = stubLocation('/signup.html')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(401, { error: 'invalid_request' })))

    await expect(
      apiFetch('/api/auth/signup', { method: 'POST', body: '{}' }),
    ).rejects.toBeInstanceOf(ApiError)

    expect(assign).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 1.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -30
```
Expected: vitest fails on the new test cases (`apiFetch is not implemented` / undefined exports). The existing `safeNext` cases still pass.

- [ ] **Step 1.3: Implement the real `client.ts`**

Replace `web/src/shared/api/client.ts` entirely with:

```ts
// safeNext validates a post-login redirect target. Every consumer of
// the ?next= query param routes through this guard. See spec Sec-2.
export function safeNext(raw: string | null): string {
  if (!raw) return '/'
  if (!raw.startsWith('/')) return '/'
  if (raw.startsWith('//')) return '/'
  if (raw.startsWith('/\\')) return '/'
  if (typeof location === 'undefined') return '/'
  try {
    const u = new URL(raw, location.origin)
    if (u.origin !== location.origin) return '/'
    return u.pathname + u.search + u.hash
  } catch {
    return '/'
  }
}

// CSRF cache: relay derives the token from the session secret and
// returns it in /api/me's body. Frontend caches it here and adds it
// to every non-GET request. See spec Sec-4.
let cachedCsrf = ''
export function setCsrfToken(token: string): void {
  cachedCsrf = token
}
export function clearCsrfToken(): void {
  cachedCsrf = ''
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    public readonly response: Response | null,
  ) {
    super(`api error ${status} ${code}`)
    this.name = 'ApiError'
  }
}

export interface ApiResult<T> {
  data: T
  status: number
  headers: Headers
}

// apiFetch is the single network entry point for the browser client.
// All non-GET methods get the cached CSRF token automatically; 401s on
// non-auth pages redirect to /login.html?next=<safe>; non-2xx replies
// throw ApiError carrying the error code from the JSON body.
export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<ApiResult<T>> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)

  if (method !== 'GET' && method !== 'HEAD') {
    if (cachedCsrf) headers.set('X-CSRF-Token', cachedCsrf)
    if (!headers.has('Content-Type') && init.body !== undefined) {
      headers.set('Content-Type', 'application/json')
    }
  }

  let res: Response
  try {
    res = await fetch(path, { ...init, headers, credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, 'network_error', null)
  }

  if (res.status === 401) {
    const onAuthPage =
      typeof location !== 'undefined' &&
      (location.pathname === '/login.html' || location.pathname === '/signup.html')
    if (!onAuthPage && typeof location !== 'undefined') {
      const next = safeNext(location.pathname + location.search + location.hash)
      location.assign('/login.html?next=' + encodeURIComponent(next))
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

- [ ] **Step 1.4: Run tests to verify green**

```bash
cd web && npm test 2>&1 | tail -20
```
Expected: all tests pass, including the original 9 safeNext cases and the new CSRF/error/redirect cases. Count should be 9 + 4 + 3 + 3 = **19 passing**.

- [ ] **Step 1.5: Type-check**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: no errors.

- [ ] **Step 1.6: Commit**

```bash
git add web/src/shared/api/client.ts web/tests/unit/shared/api/client.test.ts
git commit -m "$(cat <<'EOF'
feat(web): full apiFetch with CSRF cache and 401 auto-redirect

Replaces the PR-A stub with the real client:
- module-level cachedCsrf populated by setCsrfToken / cleared by
  clearCsrfToken; X-CSRF-Token injected on non-GET/HEAD when present
- ApiError carries (status, code, response); code parsed from
  {error: "..."} JSON bodies on non-2xx replies
- 401 → location.assign('/login.html?next=<safe>') unless already on
  /login.html or /signup.html (avoids redirect loops)
- credentials: 'same-origin'; JSON Content-Type defaulted

Spec Sec-2 + Sec-4 enforced by 19 vitest cases.
EOF
)"
```

---

## Task 2: Auth helpers — `api/auth.ts`, `api/me.ts`, `api/version.ts`, `api/types.ts`

**Files:**
- Create: `web/src/shared/api/types.ts`
- Create: `web/src/shared/api/me.ts`
- Create: `web/src/shared/api/auth.ts`
- Create: `web/src/shared/api/version.ts`
- Create: `web/tests/unit/shared/api/auth.test.ts`

- [ ] **Step 2.1: Write DTO types**

Create `web/src/shared/api/types.ts`:

```ts
export interface MeResponse {
  user_id: string
  email: string
  is_admin?: boolean
  csrf_token?: string
}

export interface AuthSuccess {
  user_id: string
  email: string
}

export interface VersionResponse {
  version: string
}
```

- [ ] **Step 2.2: Write `me.ts`**

Create `web/src/shared/api/me.ts`:

```ts
import { apiFetch, setCsrfToken } from './client'
import type { MeResponse } from './types'

// getMe fetches the current user and refreshes the CSRF cache when the
// server returns a token (always present for cookie-authenticated calls).
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  if (data.csrf_token) setCsrfToken(data.csrf_token)
  return data
}
```

- [ ] **Step 2.3: Write `auth.ts`**

Create `web/src/shared/api/auth.ts`:

```ts
import { apiFetch, clearCsrfToken } from './client'
import { getMe } from './me'
import type { AuthSuccess } from './types'

// login submits credentials, then calls getMe to populate the CSRF
// cache before any subsequent mutating request fires.
export async function login(email: string, password: string): Promise<AuthSuccess> {
  const { data } = await apiFetch<AuthSuccess>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
  await getMe()
  return data
}

// signup mirrors login but adds the required invite_code field.
export async function signup(
  email: string,
  password: string,
  invite_code: string,
): Promise<AuthSuccess> {
  const { data } = await apiFetch<AuthSuccess>('/api/auth/signup', {
    method: 'POST',
    body: JSON.stringify({ email, password, invite_code }),
  })
  await getMe()
  return data
}

// logout invalidates the server session and drops the local CSRF cache.
export async function logout(): Promise<void> {
  await apiFetch('/api/auth/logout', { method: 'POST' })
  clearCsrfToken()
}
```

- [ ] **Step 2.4: Write `version.ts`**

Create `web/src/shared/api/version.ts`:

```ts
import { apiFetch, ApiError } from './client'
import type { VersionResponse } from './types'

// fetchVersionLabel returns the display string for the page footer.
// Falls back to "version dev" on any failure — version display is
// best-effort UI, not security-critical.
export async function fetchVersionLabel(): Promise<string> {
  try {
    const { data } = await apiFetch<VersionResponse>('/api/version')
    return `version ${data.version}`
  } catch (e) {
    if (!(e instanceof ApiError)) throw e
    return 'version dev'
  }
}
```

- [ ] **Step 2.5: Write the auth test (red)**

Create `web/tests/unit/shared/api/auth.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { login, signup, logout } from '@shared/api/auth'
import { clearCsrfToken } from '@shared/api/client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('auth helpers', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('login posts credentials and then refreshes /api/me to populate CSRF cache', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b' }))
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b', csrf_token: 'csrf-xyz' }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await login('a@b', 'password-1234')

    expect(result).toEqual({ user_id: 'u1', email: 'a@b' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login')
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/me')
  })

  it('signup posts the invite_code and then refreshes /api/me', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b' }))
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b', csrf_token: 'csrf-xyz' }))
    vi.stubGlobal('fetch', fetchMock)

    await signup('a@b', 'password-1234', 'invite-abc')

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [, init] = fetchMock.mock.calls[0]!
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'a@b',
      password: 'password-1234',
      invite_code: 'invite-abc',
    })
  })

  it('logout posts and clears the CSRF cache', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { status: 'logged_out' }))
    vi.stubGlobal('fetch', fetchMock)

    await logout()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/logout')
    // (We can't directly observe cachedCsrf, but a subsequent mutating
    // request should not carry X-CSRF-Token. The detailed coverage of
    // that lives in client.test.ts; here we just confirm logout runs.)
  })
})
```

- [ ] **Step 2.6: Run tests**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: 22 passing (19 from Task 1 + 3 new auth cases).

- [ ] **Step 2.7: Type-check**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: clean.

- [ ] **Step 2.8: Commit**

```bash
git add web/src/shared/api/types.ts web/src/shared/api/me.ts web/src/shared/api/auth.ts web/src/shared/api/version.ts web/tests/unit/shared/api/auth.test.ts
git commit -m "$(cat <<'EOF'
feat(web): auth helpers + /api/me + /api/version DTOs

login() and signup() POST credentials then refresh /api/me to populate
the CSRF cache before any later mutating request. logout() clears the
cache. fetchVersionLabel() reads /api/version with a "version dev"
fallback on failure. DTO interfaces mirror the relay's JSON shapes.
EOF
)"
```

---

## Task 3: Login entry — `src/login/{App.vue, main.ts}` + `web/login.html`

**Files:**
- Create: `web/src/login/main.ts`
- Create: `web/src/login/App.vue`
- Create: `web/login.html`

- [ ] **Step 3.1: Write `main.ts`**

Create `web/src/login/main.ts`:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'

createApp(App).mount('#app')
```

- [ ] **Step 3.2: Write `App.vue`**

Create `web/src/login/App.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NButton,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import { safeNext, ApiError } from '@shared/api/client'
import { login } from '@shared/api/auth'
import { fetchVersionLabel } from '@shared/api/version'

const email = ref('')
const password = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const versionLabel = ref('version dev')

onMounted(async () => {
  versionLabel.value = await fetchVersionLabel()
})

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'invalid_credentials') return 'Invalid email or password.'
    if (e.code === 'rate_limited') return 'Too many attempts. Please wait a few minutes.'
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Sign-in failed. Please try again.'
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await login(email.value, password.value)
    const nextParam = new URLSearchParams(location.search).get('next')
    location.assign(safeNext(nextParam))
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <main class="auth-page">
        <n-card class="auth-card" :bordered="false">
          <header class="auth-title">
            <h1>AT Term</h1>
            <p class="auth-subtitle">sign in</p>
          </header>
          <form @submit="onSubmit" autocomplete="on" novalidate>
            <n-form label-placement="top" require-mark-placement="right-hanging">
              <n-form-item label="Email" :show-feedback="false">
                <n-input
                  v-model:value="email"
                  type="text"
                  placeholder="you@example.com"
                  :input-props="{ type: 'email', required: true, autocomplete: 'username' }"
                />
              </n-form-item>
              <n-form-item label="Password" :show-feedback="false">
                <n-input
                  v-model:value="password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ required: true, autocomplete: 'current-password' }"
                />
              </n-form-item>
              <n-button
                type="primary"
                attr-type="submit"
                :loading="submitting"
                :disabled="submitting"
                block
              >
                Sign in
              </n-button>
              <p v-if="errorMsg" class="auth-error" role="alert">{{ errorMsg }}</p>
              <p class="auth-alt">
                Have an invite code?
                <a href="/signup.html">Sign up here</a>.
              </p>
            </n-form>
          </form>
        </n-card>
        <p class="auth-version">{{ versionLabel }}</p>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  background: var(--bg);
  color: var(--fg);
  display: grid;
  grid-template-rows: 1fr auto;
  place-items: center;
  padding: 2rem 1rem;
  gap: 1rem;
}
.auth-card {
  max-width: 420px;
  width: 100%;
  background: var(--panel);
}
.auth-title {
  text-align: center;
  margin-bottom: 1rem;
}
.auth-title h1 {
  margin: 0;
  font-size: 1.5rem;
  letter-spacing: 0.08em;
}
.auth-subtitle {
  margin: 0.25rem 0 0;
  color: var(--fg-dim);
  font-size: 0.875rem;
  text-transform: lowercase;
  letter-spacing: 0.1em;
}
.auth-error {
  color: var(--bad);
  margin: 0.75rem 0 0;
  font-size: 0.875rem;
}
.auth-alt {
  margin: 1rem 0 0;
  font-size: 0.875rem;
  color: var(--fg-dim);
  text-align: center;
}
.auth-alt a {
  color: var(--accent);
}
.auth-version {
  color: var(--fg-dim);
  font-size: 0.75rem;
  margin: 0;
}
</style>
```

- [ ] **Step 3.3: Write `web/login.html`**

Create `web/login.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="page" content="login" />
  <meta name="theme-color" content="#0b1020" />
  <title>AT Term · sign in</title>
  <link rel="icon" href="/icon.svg" type="image/svg+xml" />
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/login/main.ts"></script>
</body>
</html>
```

- [ ] **Step 3.4: Commit (no build yet)**

```bash
git add web/src/login web/login.html
git commit -m "feat(web): Login.vue + login.html entry (Vue 3 + Naive UI)"
```

Note: this commit will fail `npm run build` until Task 5 updates `vite.config.ts` to include the new entry. That's expected — Tasks 3, 4 land the source; Task 5 wires it up.

---

## Task 4: Signup entry — `src/signup/{App.vue, main.ts}` + `web/signup.html`

**Files:**
- Create: `web/src/signup/main.ts`
- Create: `web/src/signup/App.vue`
- Create: `web/signup.html`

- [ ] **Step 4.1: Write `main.ts`**

Create `web/src/signup/main.ts`:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'

createApp(App).mount('#app')
```

- [ ] **Step 4.2: Write `App.vue`**

Create `web/src/signup/App.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NButton,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import { safeNext, ApiError } from '@shared/api/client'
import { signup } from '@shared/api/auth'
import { fetchVersionLabel } from '@shared/api/version'

const email = ref('')
const password = ref('')
const inviteCode = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const versionLabel = ref('version dev')

onMounted(async () => {
  versionLabel.value = await fetchVersionLabel()
})

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_taken') return 'An account with that email already exists.'
    if (e.code === 'invite_invalid') return 'Invite code is invalid or already used.'
    if (e.code === 'password_weak') return 'Password must be at least 12 characters.'
    if (e.code === 'invalid_email') return 'Please enter a valid email.'
    if (e.code === 'rate_limited') return 'Too many attempts. Please wait.'
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Sign-up failed. Check your invite code and try again.'
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await signup(email.value, password.value, inviteCode.value)
    const nextParam = new URLSearchParams(location.search).get('next')
    location.assign(safeNext(nextParam))
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <main class="auth-page">
        <n-card class="auth-card" :bordered="false">
          <header class="auth-title">
            <h1>AT Term</h1>
            <p class="auth-subtitle">sign up</p>
          </header>
          <form @submit="onSubmit" autocomplete="on" novalidate>
            <n-form label-placement="top" require-mark-placement="right-hanging">
              <n-form-item label="Email" :show-feedback="false">
                <n-input
                  v-model:value="email"
                  type="text"
                  placeholder="you@example.com"
                  :input-props="{ type: 'email', required: true, autocomplete: 'username' }"
                />
              </n-form-item>
              <n-form-item label="Password" :show-feedback="false">
                <n-input
                  v-model:value="password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
                />
              </n-form-item>
              <n-form-item label="Invite code" :show-feedback="false">
                <n-input
                  v-model:value="inviteCode"
                  type="text"
                  :input-props="{ required: true, autocomplete: 'off' }"
                />
              </n-form-item>
              <n-button
                type="primary"
                attr-type="submit"
                :loading="submitting"
                :disabled="submitting"
                block
              >
                Create account
              </n-button>
              <p v-if="errorMsg" class="auth-error" role="alert">{{ errorMsg }}</p>
              <p class="auth-alt">
                Already have an account?
                <a href="/login.html">Sign in</a>.
              </p>
            </n-form>
          </form>
        </n-card>
        <p class="auth-version">{{ versionLabel }}</p>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  background: var(--bg);
  color: var(--fg);
  display: grid;
  grid-template-rows: 1fr auto;
  place-items: center;
  padding: 2rem 1rem;
  gap: 1rem;
}
.auth-card {
  max-width: 420px;
  width: 100%;
  background: var(--panel);
}
.auth-title {
  text-align: center;
  margin-bottom: 1rem;
}
.auth-title h1 {
  margin: 0;
  font-size: 1.5rem;
  letter-spacing: 0.08em;
}
.auth-subtitle {
  margin: 0.25rem 0 0;
  color: var(--fg-dim);
  font-size: 0.875rem;
  text-transform: lowercase;
  letter-spacing: 0.1em;
}
.auth-error {
  color: var(--bad);
  margin: 0.75rem 0 0;
  font-size: 0.875rem;
}
.auth-alt {
  margin: 1rem 0 0;
  font-size: 0.875rem;
  color: var(--fg-dim);
  text-align: center;
}
.auth-alt a {
  color: var(--accent);
}
.auth-version {
  color: var(--fg-dim);
  font-size: 0.75rem;
  margin: 0;
}
</style>
```

- [ ] **Step 4.3: Write `web/signup.html`**

Create `web/signup.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="page" content="signup" />
  <meta name="theme-color" content="#0b1020" />
  <title>AT Term · sign up</title>
  <link rel="icon" href="/icon.svg" type="image/svg+xml" />
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/signup/main.ts"></script>
</body>
</html>
```

- [ ] **Step 4.4: Commit**

```bash
git add web/src/signup web/signup.html
git commit -m "feat(web): Signup.vue + signup.html entry (Vue 3 + Naive UI)"
```

---

## Task 5: Vite config — swap placeholder for real entries, add dev proxy

**Files:**
- Modify: `web/vite.config.ts`
- Delete: `web/src/_placeholder.html`

- [ ] **Step 5.1: Update `vite.config.ts`**

Replace `web/vite.config.ts` with:

```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const RELAY_HTTP = 'http://127.0.0.1:8080'
const RELAY_WS = 'ws://127.0.0.1:8080'

// PR-B introduces the first real entries (login + signup). Index,
// settings, admin are still served from web/legacy/ via build-web.sh
// layer 1; they migrate in PR-C, PR-D, PR-E.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api':       { target: RELAY_HTTP, changeOrigin: false },
      '/sub':       { target: RELAY_HTTP, changeOrigin: false },
      '/admin/api': { target: RELAY_HTTP, changeOrigin: false },
      '/agent':     { target: RELAY_WS,   ws: true, changeOrigin: false },
      '/uplink':    { target: RELAY_WS,   ws: true, changeOrigin: false },
      '/client':    { target: RELAY_WS,   ws: true, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    rollupOptions: {
      input: {
        login:  fileURLToPath(new URL('./login.html',  import.meta.url)),
        signup: fileURLToPath(new URL('./signup.html', import.meta.url)),
      },
    },
  },
})
```

- [ ] **Step 5.2: Delete the placeholder entry**

```bash
git rm web/src/_placeholder.html
```

- [ ] **Step 5.3: Build and inspect output**

```bash
cd web && npm run build 2>&1 | tail -10
ls web/dist
```
Expected:
- `npm run build` succeeds (vue-tsc + vite). Vite reports two HTML inputs and emits hashed assets.
- `web/dist` contains `login.html`, `signup.html`, and an `assets/` subdirectory with hashed JS/CSS.
- No `_placeholder.html` anywhere.

If vue-tsc reports type errors, fix them before continuing. Common issue: Naive UI component prop type names — match the import casing exactly (e.g. `NConfigProvider` not `NConfigprovider`).

- [ ] **Step 5.4: Sync embed and inspect**

```bash
./scripts/build-web.sh
ls internal/relay/web-dist | head -20
ls internal/relay/web-dist/assets 2>/dev/null | head -5
```
Expected: `login.html` and `signup.html` are now the Vue-built versions (look for `<div id="app"></div>` inside them); `assets/` contains the hashed bundles; legacy `index.html`, `settings.html`, `admin/`, `style.css`, `app.js`, etc. all remain.

```bash
grep -l '<div id="app">' internal/relay/web-dist/*.html
```
Expected: `internal/relay/web-dist/login.html`, `internal/relay/web-dist/signup.html` (and not `index.html`, `settings.html`, which still use the legacy template).

- [ ] **Step 5.5: Verify the placeholder rsync filter is now unnecessary**

```bash
grep -- "--exclude='_placeholder" scripts/build-web.sh
```
Expected: one hit (the exclude pattern remains in the script). Leave it in place — it's a harmless no-op now that no `_placeholder.html` is produced, and it documents the PR-A migration step. We can delete it in PR-F cutover.

- [ ] **Step 5.6: Commit**

```bash
git add web/vite.config.ts web/src/_placeholder.html
git commit -m "$(cat <<'EOF'
build(web): vite entries → login + signup, add dev proxy

Replaces the PR-A _placeholder entry with the two real entries that
PR-B is shipping. server.proxy forwards /api, /sub, /admin/api to the
relay's HTTP port and /agent, /uplink, /client to its WS endpoint so
`npm run dev` (port 5173) can talk to a relay running on :8080.

The --exclude='_placeholder*' filter in scripts/build-web.sh becomes a
no-op but stays in place until PR-F cutover.
EOF
)"
```

Hint: `git add web/src/_placeholder.html` stages the deletion that `git rm` performed in Step 5.2.

---

## Task 6: Component tests — Login.vue and Signup.vue

**Files:**
- Create: `web/tests/unit/login/App.test.ts`
- Create: `web/tests/unit/signup/App.test.ts`

These tests mount the components with `@vue/test-utils`, mock the auth module, and assert the form-submit path.

- [ ] **Step 6.1: Write the Login component test**

Create `web/tests/unit/login/App.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/auth', () => ({
  login: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))

import App from '@/login/App.vue'
import { login } from '@shared/api/auth'
import { ApiError } from '@shared/api/client'

describe('Login App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/login.html', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('calls login() with the entered credentials and redirects to / on success', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(login).toHaveBeenCalledWith('a@b', 'password-1234')
    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('uses safeNext on ?next= when provided', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, search: '?next=%2Fsettings.html%3Ftab%3Dtokens' },
      writable: true,
    })
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/settings.html?tab=tokens')
  })

  it('rejects open-redirect ?next= values (//evil → /)', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, search: '?next=' + encodeURIComponent('//evil.example') },
      writable: true,
    })
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('shows "Invalid email or password." on 401 invalid_credentials', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'invalid_credentials', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Invalid email or password.')
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the rate-limit message on 429', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(429, 'rate_limited', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('p')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Too many attempts')
  })
})
```

Note the `@/login/App.vue` import — that's a new alias we need.

- [ ] **Step 6.2: Add the entry alias to `tsconfig.json` and `vitest.config.ts`**

In `web/tsconfig.json`, extend the `paths` map:

```json
    "paths": {
      "@shared/*": ["src/shared/*"],
      "@/*": ["src/*"]
    }
```

In `web/vitest.config.ts`, extend the `alias` map:

```ts
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
```

(`vite.config.ts` `resolve.alias` also gains the same `@` entry; do it now for symmetry.)

Update `web/vite.config.ts`:

```ts
  resolve: {
    alias: {
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
      '@':       fileURLToPath(new URL('./src',        import.meta.url)),
    },
  },
```

- [ ] **Step 6.3: Write the Signup component test**

Create `web/tests/unit/signup/App.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/auth', () => ({
  signup: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))

import App from '@/signup/App.vue'
import { signup } from '@shared/api/auth'
import { ApiError } from '@shared/api/client'

describe('Signup App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/signup.html', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('calls signup() with email + password + invite_code and redirects to /', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('invite-xyz')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(signup).toHaveBeenCalledWith('a@b', 'password-1234', 'invite-xyz')
    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('shows "An account with that email already exists." on 409 email_taken', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'email_taken', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('invite-xyz')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('An account with that email already exists.')
  })

  it('shows the invite-invalid message on 400 invite_invalid', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'invite_invalid', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('bad-invite')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Invite code is invalid or already used.')
  })
})
```

- [ ] **Step 6.4: Run all tests**

```bash
cd web && npm test 2>&1 | tail -15
```
Expected: 22 + 5 + 3 = **30 passing**. If a Naive UI form interaction selector doesn't match (e.g. there are multiple inputs sharing the same `autocomplete` value or the inputs are inside a shadow boundary), adjust the selector to a stable one — e.g. add a `data-testid` to the `<n-input>` in `App.vue` and target it via `[data-testid="email"]`.

- [ ] **Step 6.5: Type-check**

```bash
cd web && npx vue-tsc --noEmit
```
Expected: clean.

- [ ] **Step 6.6: Commit**

```bash
git add web/tsconfig.json web/vite.config.ts web/vitest.config.ts web/tests/unit/login web/tests/unit/signup
git commit -m "$(cat <<'EOF'
test(web): Login.vue + Signup.vue component coverage

@/ alias points at web/src/; tests mount with @vue/test-utils, mock
the auth module, and assert: credential round-trip, /api/version
side-effect, safeNext on ?next=, ApiError → user-facing message
mapping for invalid_credentials, rate_limited, email_taken,
invite_invalid.
EOF
)"
```

---

## Task 7: Migrate `auth-pages` + `no-inline-script` to `web/tests/contract/`

**Files:**
- Create: `web/tests/contract/auth-pages.test.mjs`
- Create: `web/tests/contract/no-inline-script.test.mjs`
- Delete: `web/legacy/auth-pages.test.mjs`
- Delete: `web/legacy/no-inline-script.test.mjs`

Contract tests live separately from vitest because they validate the *built artifacts* (post-build) rather than source. They run via plain `node --test` against `internal/relay/web-dist/`.

- [ ] **Step 7.1: Write `auth-pages.test.mjs`**

Create `web/tests/contract/auth-pages.test.mjs`:

```js
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";

const distRoot = path.join("internal", "relay", "web-dist");

function readDist(rel) {
  return readFileSync(path.join(distRoot, rel), "utf8");
}

test("login.html exists and mounts a Vue app", () => {
  const html = readDist("login.html");
  assert.match(html, /<div id="app">/, "login.html must mount #app");
  assert.match(html, /\/src\/login\/main\.ts|\/assets\/login.*\.js/, "login.html must reference the login entry script");
});

test("signup.html exists and mounts a Vue app", () => {
  const html = readDist("signup.html");
  assert.match(html, /<div id="app">/, "signup.html must mount #app");
  assert.match(html, /\/src\/signup\/main\.ts|\/assets\/signup.*\.js/, "signup.html must reference the signup entry script");
});

test("login.html does not leak any token in the URL", () => {
  const html = readDist("login.html");
  assert.doesNotMatch(html, /\?token=/, "login.html must not contain ?token= (red-line 9)");
});

test("signup.html does not leak any token in the URL", () => {
  const html = readDist("signup.html");
  assert.doesNotMatch(html, /\?token=/, "signup.html must not contain ?token=");
});

test("auth HTMLs reference /api/auth endpoints only via JS bundle", () => {
  // The HTML shell shouldn't hard-code form actions; submission goes
  // through apiFetch in the bundle.
  for (const name of ["login.html", "signup.html"]) {
    const html = readDist(name);
    assert.doesNotMatch(html, /<form[^>]+action=/i, `${name} must not declare a form action attribute`);
  }
});
```

- [ ] **Step 7.2: Write `no-inline-script.test.mjs`**

Create `web/tests/contract/no-inline-script.test.mjs`:

```js
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

const distRoot = path.join("internal", "relay", "web-dist");

function walkHtml(dir, acc = []) {
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) {
      walkHtml(full, acc);
    } else if (name.endsWith(".html")) {
      acc.push(full);
    }
  }
  return acc;
}

// Matches inline <script>...</script> with a non-empty body that is not
// just whitespace. External and module scripts (<script src=...> or
// <script type="module" src=...>) are fine; only the inline form is
// banned per CSP script-src 'self' policy.
const INLINE_SCRIPT_RE = /<script\b(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi;

for (const file of walkHtml(distRoot)) {
  test(`${path.relative(distRoot, file)} has no inline <script> content`, () => {
    const html = readFileSync(file, "utf8");
    let match;
    while ((match = INLINE_SCRIPT_RE.exec(html)) !== null) {
      const body = match[1].trim();
      if (body.length > 0) {
        assert.fail(
          `Inline <script> found in ${file}: ` +
          `${body.slice(0, 120)}${body.length > 120 ? "…" : ""}`,
        );
      }
    }
  });
}
```

- [ ] **Step 7.3: Delete the legacy copies**

```bash
git rm web/legacy/auth-pages.test.mjs
git rm web/legacy/no-inline-script.test.mjs
```

- [ ] **Step 7.4: Run the contract tests**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
./scripts/build-web.sh
node --test web/tests/contract/*.test.mjs
```
Expected: all contract tests pass. The `no-inline-script` test will produce one test case per `.html` file under `internal/relay/web-dist/` (legacy index/settings/admin plus the new login/signup).

- [ ] **Step 7.5: Run the trimmed legacy suite**

```bash
node --test web/legacy/*.test.mjs
```
Expected: still green (with `auth-pages.test.mjs` and `no-inline-script.test.mjs` removed, the count drops from 125 to whatever's left — confirm a non-zero count and that nothing went red).

- [ ] **Step 7.6: Commit**

```bash
git add web/tests/contract web/legacy/auth-pages.test.mjs web/legacy/no-inline-script.test.mjs
git commit -m "$(cat <<'EOF'
test(web): migrate auth-pages + no-inline-script to web/tests/contract

Contract tests now scan internal/relay/web-dist/ (the artifact the
relay actually serves), so they cover both the new Vue login/signup
HTML and any remaining legacy HTML in the embed. The legacy copies
under web/legacy/ are removed.
EOF
)"
```

---

## Task 8: Wire `test:contract` into package.json + CI

**Files:**
- Modify: `web/package.json`
- Modify: `.github/workflows/build.yml`

- [ ] **Step 8.1: Update `package.json`**

Find the `scripts` block in `web/package.json` and update `test:contract`:

```json
  "scripts": {
    "build": "vue-tsc --noEmit && vite build",
    "dev": "vite",
    "preview": "vite preview",
    "test": "vitest run",
    "test:contract": "node --test tests/contract/*.test.mjs"
  },
```

Note the glob path is relative to `web/`. The script runs from inside `web/`.

- [ ] **Step 8.2: Run the new script**

```bash
cd web && npm run test:contract
```
Expected: all contract tests pass when run from the `web/` directory. (Inside `web/`, `tests/contract/*.test.mjs` matches; the files read from `../internal/relay/web-dist/` via the `distRoot = path.join("internal", "relay", "web-dist")` path. **That path is relative to repo root, not `web/`.** Adjust accordingly — see Step 8.3.)

- [ ] **Step 8.3: Fix the contract tests to be CWD-independent**

The contract tests should work regardless of whether `node --test` is invoked from repo root (CI's old behavior) or from `web/` (npm script behavior). The fix is to resolve `distRoot` relative to the test file's own location.

Edit `web/tests/contract/auth-pages.test.mjs` — replace:

```js
const distRoot = path.join("internal", "relay", "web-dist");
```

with:

```js
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const distRoot = path.resolve(here, "..", "..", "..", "internal", "relay", "web-dist");
```

Do the same in `web/tests/contract/no-inline-script.test.mjs`.

- [ ] **Step 8.4: Re-run from both CWDs**

```bash
cd web && npm run test:contract && cd ..
node --test web/tests/contract/*.test.mjs
```
Both invocations should pass.

- [ ] **Step 8.5: Add the CI step**

In `.github/workflows/build.yml`, find the `web-vue-tests` job and insert a new step between `vitest` and `sync embed`:

```yaml
      - name: vitest
        working-directory: web
        run: npm test
      - name: contract tests
        working-directory: web
        run: npm run test:contract
      - name: sync embed
        run: ./scripts/build-web.sh
```

Note: `contract tests` runs AFTER `vitest` but BEFORE `sync embed`. The contract tests assert against the *current* state of `internal/relay/web-dist/`, which `build-web.sh` regenerates next. If a developer made a change that requires a sync, they're expected to commit it; CI catches the drift in the `verify embed has no drift` step that follows.

- [ ] **Step 8.6: Lint YAML**

```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/build.yml'))" && echo YAML_OK
```

- [ ] **Step 8.7: Commit**

```bash
git add web/package.json web/tests/contract .github/workflows/build.yml
git commit -m "$(cat <<'EOF'
ci(web): run contract tests against embedded FS

test:contract now resolves the dist path relative to its own location
so the npm script (run from web/) and a direct node --test invocation
(from repo root) both succeed. The new web-vue-tests step runs after
vitest and before the embed sync.
EOF
)"
```

---

## Task 9: Delete legacy login/signup files + bump SW cache

**Files:**
- Delete: `web/legacy/login.html`, `web/legacy/login.js`, `web/legacy/signup.html`, `web/legacy/signup.js`
- Modify: `web/legacy/sw.js`

After this task, the new Vue login/signup pages are the only versions in the embed (Vite overlay replaced legacy via build-web.sh layer 2 in Task 5; legacy copies become unreachable). We delete them from source to avoid orphan code.

- [ ] **Step 9.1: Delete the legacy auth files**

```bash
git rm web/legacy/login.html web/legacy/login.js web/legacy/signup.html web/legacy/signup.js
```

- [ ] **Step 9.2: Update `sw.js` ASSETS list**

Edit `web/legacy/sw.js`. Find the `ASSETS` array and remove the lines `"./login.js",` and `"./signup.js",`. The array becomes:

```js
const ASSETS = [
  "./",
  "./admin/admin-invitations.js",
  "./admin/admin-users.js",
  "./admin/admin.js",
  "./app-core.js",
  "./app.js",
  "./layout.js",
  "./settings.js",
  "./settings-danger.js",
  "./settings-sessions.js",
  "./style.css",
  "./vendor/xterm/xterm.css",
  "./vendor/xterm/xterm.js",
  "./vendor/xterm-addon-fit/xterm-addon-fit.js",
  "./manifest.webmanifest",
  "./icon.png",
  "./icon.svg",
];
```

- [ ] **Step 9.3: Compute the new CACHE hash**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
node --test web/legacy/sw-cache-bump.test.mjs
```
Expected: test fails with a message like `Replace it with "at-term-web-<hex>"`. Copy that 8-character hex.

Edit `web/legacy/sw.js`. Replace:

```js
const CACHE = "at-term-web-83c9916a";
```

with the new value (paste the hex from the previous step). Example:

```js
const CACHE = "at-term-web-NEWHEX99";
```

- [ ] **Step 9.4: Confirm the bump test passes**

```bash
node --test web/legacy/sw-cache-bump.test.mjs
```
Expected: passes.

- [ ] **Step 9.5: Run the full legacy suite**

```bash
node --test web/legacy/*.test.mjs
```
Expected: all green. The `auth-pages.test.mjs` and `no-inline-script.test.mjs` files were already removed in Task 7, so the count is lower than the PR-A baseline but everything that remains must pass.

- [ ] **Step 9.6: Commit**

```bash
git add web/legacy/sw.js web/legacy/login.html web/legacy/login.js web/legacy/signup.html web/legacy/signup.js
git commit -m "$(cat <<'EOF'
chore(web): drop legacy login/signup sources; bump SW cache name

The Vite-built login.html and signup.html land in web-dist via the
overlay in scripts/build-web.sh, so the legacy copies are now dead
code. sw.js's ASSETS no longer references login.js/signup.js, and the
CACHE hash bumps so installed PWA clients re-fetch on next activation.
EOF
)"
```

---

## Task 10: Final smoke + sync embed + PR

**Files:**
- Regenerate: `internal/relay/web-dist/**`

- [ ] **Step 10.1: Full test suite**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./... && go test ./...
(cd web && npm run build && npm test && npm run test:contract)
node --test web/legacy/*.test.mjs
```
Expected: all green.

- [ ] **Step 10.2: Sync the embed**

```bash
./scripts/build-web.sh
git status --short -- internal/relay/web-dist
```
Expected: a non-empty diff against the committed `internal/relay/web-dist/` — Tasks 3–5 produced new login.html / signup.html / assets bundles, and Task 9 removed legacy login/signup sources. Stage the changes:

```bash
git add internal/relay/web-dist
```

- [ ] **Step 10.3: Verify drift gate**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0. If the second `build-web.sh` invocation produces additional diff, the script is non-deterministic — investigate before committing. Vite hashed asset names should be content-derived and stable across runs of the same source.

- [ ] **Step 10.4: Commit the synced embed**

```bash
git commit -m "$(cat <<'EOF'
build(web): sync embed with PR-B vue login + signup

scripts/build-web.sh overlay output: vue-built login.html / signup.html
plus their hashed assets bundles replace the legacy copies in
internal/relay/web-dist/.
EOF
)"
```

- [ ] **Step 10.5: Smoke the relay end-to-end**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3

# Serves the Vue login page
curl -sI http://127.0.0.1:18080/login.html | head -1                 # 200
curl -s  http://127.0.0.1:18080/login.html | grep -c '<div id="app">' # ≥1
curl -s  http://127.0.0.1:18080/login.html | grep -c '<form id="login-form"'  # 0 (legacy gone)

# Serves the Vue signup page
curl -sI http://127.0.0.1:18080/signup.html | head -1                # 200
curl -s  http://127.0.0.1:18080/signup.html | grep -c '<div id="app">' # ≥1

# /api/version still works (Vue calls this)
curl -s http://127.0.0.1:18080/api/version

# Anonymous / and /admin/ still redirect
curl -sI http://127.0.0.1:18080/        | head -1 | grep -q '302'    || echo FAIL
curl -sI http://127.0.0.1:18080/admin/  | head -1 | grep -q '302'    || echo FAIL

kill $RELAY_PID 2>/dev/null
```

If any line outputs `FAIL` or the `grep -c` returns the wrong value, stop and investigate.

Optionally, also do a quick browser-driven check by running `cd web && npm run dev` (port 5173) in one terminal and `go run ./cmd/atterm-relay --addr 127.0.0.1:8080 --dev-insecure` in another, then opening `http://localhost:5173/login.html`. The proxy should forward the auth requests. This is optional for PR-B and required for PR-C+.

- [ ] **Step 10.6: Working tree clean check**

```bash
git status --short
```
Expected: only the pre-existing untracked artifacts (`.claude/`, `.playwright-mcp/`, `atterm-relay`, two unrelated `2026-05-14-*.md` plan drafts). Nothing modified.

- [ ] **Step 10.7: Push and open PR**

```bash
git push -u origin web/vue-rewrite-pr-b-login-signup
PATH=/opt/homebrew/bin:$PATH gh pr create --title "web: PR-B login + signup (vue + naive-ui)" --body "$(cat <<'EOF'
## Summary

PR-B of the web client rewrite. Replaces the vanilla `/login.html` and `/signup.html` with Vue 3 + Naive UI entries served from the relay's embedded FS. Lands the foundation modules every later PR will use.

- Full `apiFetch` with CSRF cache (`X-CSRF-Token` injected on non-GET when cache is populated), `ApiError` carrying code, 401 → `/login.html?next=<safe>` (skipped on auth pages, spec Sec-2 + Sec-4)
- Auth helpers (`login`, `signup`, `logout`) that bracket `getMe()` around login/signup so the CSRF cache is populated before any later mutating request fires
- New entries `web/login.html` + `web/signup.html` with `web/src/{login,signup}/{App.vue,main.ts}`; Naive UI dark theme bound to the existing CSS tokens
- `vite.config.ts` adds `server.proxy` so `npm run dev` can talk to a relay on :8080 (/api, /sub, /admin/api over HTTP; /agent, /uplink, /client over WS)
- Vitest coverage: 22 cases for apiFetch (CSRF, 401-redirect, ApiError, content-type), 5 for Login.vue, 3 for Signup.vue, 3 for the auth helpers
- Contract tests migrate to `web/tests/contract/`: `auth-pages` and `no-inline-script` now scan the embedded FS post-build
- Legacy `web/legacy/login*`, `signup*` deleted; `sw.js` ASSETS trimmed and CACHE name bumped
- Spec amendment: Sec-4 CSRF model corrected (in-memory cache reading from `/api/me` body, not double-submit cookie)

Spec: `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`
Plan: `docs/superpowers/plans/2026-05-17-web-vue-rewrite-pr-b-login-signup.md`

## Test Plan

- [x] `go vet ./... && go test ./...`
- [x] `cd web && npm run build && npm test && npm run test:contract`
- [x] `node --test web/legacy/*.test.mjs` (excluding the two migrated files)
- [x] Manual smoke: `curl /login.html` and `/signup.html` return the Vue shells with `<div id="app">`; `/api/version` reachable; `/` and `/admin/` still 302
- [x] `./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist` — exit 0
- [ ] Manual browser smoke: load /login.html and /signup.html, sign in with the bootstrap admin, check that next-page redirect lands on `/`
- [ ] CI green (web-tests, web-vue-tests with new test:contract step, build-linux etc.)
EOF
)"
```

---

## Self-Review Notes

**Spec coverage check:**

- Spec § Architecture / Auth & API → Tasks 1 + 2 (apiFetch, CSRF cache, auth helpers, /api/me)
- Spec § Architecture / Routing — MPA → Tasks 3 + 4 + 5 (two new HTML entries, vite multi-entry input)
- Spec § Architecture / UI theme → Tasks 3 + 4 (App.vue wraps NConfigProvider + NMessageProvider, uses getNaiveOverrides)
- Spec § Testing / Unit tests → Tasks 1, 2, 6 (vitest)
- Spec § Testing / Contract tests → Task 7
- Spec § Testing / Test commands → Task 8 (test:contract wired)
- Spec § Phasing — Phase B item 1 (login/signup) → entire plan
- Spec § Invariants — no `v-html` → not violated; lint rule (`vue/no-v-html`) will be added in a later PR when ESLint is introduced (deferred)
- Spec § Security / Sec-1 (CSP) → no change; existing `style-src 'self' 'unsafe-inline'` covers Naive UI runtime style injection
- Spec § Security / Sec-2 (safeNext) → Tasks 1, 3, 4, 6 (used in apiFetch + both App.vue + tested in component tests)
- Spec § Security / Sec-3 (CSWSH) → no change; PR-B introduces no WS code
- Spec § Security / Sec-4 (CSRF) → Tasks 1, 2 (model corrected pre-Task 1 in the spec amendment commit `8a718c4`)
- Spec § Security / Sec-5 (SW precache) → deferred (vite-plugin-pwa setup belongs to a later PR; PR-B only bumps the legacy SW)
- Spec § Security / Sec-6 (supply chain) → no new deps in PR-B; lockfile unchanged
- Spec § Security / Sec-7 (build determinism) → Task 10 verifies; CI gate inherited from PR-A
- Spec § Security / Sec-8 (paste image size) → deferred to PR-E

**Placeholder scan:** every step has concrete code or commands; no "TBD" / "implement later" / "add appropriate error handling" / unnamed types.

**Type consistency:**

- `safeNext(raw: string | null): string` — same signature across client.ts, component tests, Login.vue, Signup.vue
- `apiFetch<T>(path: string, init?: RequestInit): Promise<ApiResult<T>>` — same in client.ts, used identically in auth.ts, me.ts, version.ts, and tests
- `ApiError(status: number, code: string, response: Response | null)` — same shape across client.ts, auth.test.ts, App.test.ts files
- `setCsrfToken(token: string): void` / `clearCsrfToken(): void` — same in client.ts, me.ts, auth.ts
- `getMe(): Promise<MeResponse>` — same in me.ts, auth.ts
- `login(email: string, password: string): Promise<AuthSuccess>` — same in auth.ts and Login.test.ts
- `signup(email: string, password: string, invite_code: string): Promise<AuthSuccess>` — same in auth.ts and Signup.test.ts

No drift detected.
