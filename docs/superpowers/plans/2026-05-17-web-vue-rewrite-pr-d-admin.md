# Web Vue Rewrite — PR-D: Admin 3-Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the vanilla `/admin/index.html` (Invitations / Users / Config tabs) with a Vue 3 + Naive UI version mounted at the same path, served from the relay's embedded FS, gated by the existing `/admin/` cookie-redirect.

**Architecture:** A new Vite entry `web/admin/index.html` mounts a single Vue app whose `App.vue` provides the dark theme, the shared `Topbar`, and an `n-tabs` selector wired to `location.hash`. Each tab is a `.vue` component under `web/src/admin/tabs/`. All `/admin/api/*` endpoints get typed helpers in `web/src/shared/api/admin.ts`. Relay-side admin authentication is unchanged — server-side path-based redirect for `/admin/` continues to gate cookie-less requests, and the same admin-only handlers serve every action.

**Tech Stack:** Vue 3, TypeScript, Naive UI (NTabs, NCard, NDataTable, NForm, NInput, NInputNumber, NButton, NPopconfirm, NAlert, useMessage), Vitest, `@vue/test-utils`, happy-dom. Node 20.

**Reference spec:** `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`. Note the spec line for admin lists only `Users.vue` and `Invitations.vue`; this plan adds `Config.vue` so we don't drop the runtime-limits editor that legacy admin shipped (removing it would be a regression for admins who use it). Config is a Vue tab pointing at the same `GET / PUT /admin/api/config` endpoints.

**Pre-flight:**
- Branch: `web/vue-rewrite-pr-d-admin` (cut from `origin/main` at the start of PR-D).
- PR-C merged into `main` as `be8b4e2` (#47). Shared modules (`apiFetch`, `Topbar.vue`, `getNaiveOverrides`, etc.) are live.
- v0.1.79 desktop hot-fix released; settings entry already overlaid in embed.
- Bootstrap admin envs for local relay:
  ```
  ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com'
  ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!'
  ```

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `web/src/shared/api/types.ts` | Modify | Append admin DTOs (`AdminUserRow`, `InvitationRow`, `InvitationCreated`, `InvitationCreateResponse`, `AdminConfig`) |
| `web/src/shared/api/admin.ts` | Create | Typed helpers for every `/admin/api/*` endpoint (10 functions) |
| `web/tests/unit/shared/api/admin.test.ts` | Create | Vitest coverage for the helpers (URL/method/body/CSRF header) |
| `web/src/admin/main.ts` | Create | Creates the Vue app, imports tokens.css, mounts `#app` |
| `web/src/admin/App.vue` | Create | Wraps `n-config-provider` + `n-message-provider`; renders `<Topbar active="admin" />`, `<n-tabs>` with 3 panes; hash-routes the active tab |
| `web/src/admin/tabs/Invitations.vue` | Create | Create-invite form (note, count, expires_at) + plaintext-once display + list with refresh |
| `web/src/admin/tabs/Users.vue` | Create | List users + per-user actions (promote/demote/reset password/disable) + plaintext-once display for resets |
| `web/src/admin/tabs/Config.vue` | Create | Load + edit + save `/admin/api/config` runtime limits (rate, max conns); displays effective fallback values |
| `web/tests/unit/admin/App.test.ts` | Create | Hash-routing + four-tab-labels render (3 panes; verify Config is reachable) |
| `web/tests/unit/admin/tabs/Invitations.test.ts` | Create | List render, create flow (single + bulk), error mapping |
| `web/tests/unit/admin/tabs/Users.test.ts` | Create | List render, promote/demote/reset/disable, error mapping for `cannot_demote_self` and `last_admin` |
| `web/tests/unit/admin/tabs/Config.test.ts` | Create | Load + save round-trip; effective-value display when raw=0 |
| `web/admin/index.html` | Create | Vite entry HTML with `<meta name="page" content="admin">`, `<div id="app">`, module script |
| `web/vite.config.ts` | Modify | `rollupOptions.input` adds `admin` while preserving `login` + `signup` + `settings` |
| `web/legacy/admin/index.html` | Delete | Replaced by Vue build via embed overlay |
| `web/legacy/admin/admin.js` | Delete | Replaced |
| `web/legacy/admin/admin-invitations.js` | Delete | Replaced |
| `web/legacy/admin/admin-users.js` | Delete | Replaced |
| `web/legacy/admin/admin.test.mjs` | Delete | Replaced by vitest component coverage |
| `web/legacy/sw.js` | Modify | Drop `./admin/admin*.js` from `ASSETS`; bump `CACHE` hex |
| `web/legacy/no-raw-colors.test.mjs` | Modify | Remove `admin/index.html` allow-list entry (file no longer exists) |
| `web/legacy/terminal-fit.test.mjs` | Modify | Drop `./admin/admin.js` SW-precache assertions if present (mirrors PR-B/C pattern) |
| `internal/relay/web-dist/**` | Regenerate | Via `./scripts/build-web.sh`; new `admin/index.html` + assets overlay legacy |

---

## Pre-flight

- [ ] **Step 0.1: Confirm baseline**

```bash
git status --short && git rev-parse --abbrev-ref HEAD
```
Expected: branch `web/vue-rewrite-pr-d-admin`; working tree only contains pre-existing untracked artifacts (`.claude/`, `.playwright-mcp/`, `atterm-relay`, two `2026-05-14-*.md` plan drafts).

- [ ] **Step 0.2: Confirm dependencies installed**

```bash
[ -d web/node_modules ] && echo OK || (cd web && npm ci --ignore-scripts)
```

- [ ] **Step 0.3: Confirm legacy admin still served from embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3
curl -sI http://127.0.0.1:18080/admin/ | head -1     # expect 302 anon → /login.html
curl -sI http://127.0.0.1:18080/admin/index.html | head -1   # expect 302 anon
kill $RELAY_PID 2>/dev/null
```

---

## Task 1: `api/admin.ts` helpers + DTOs (TDD)

**Files:**
- Modify: `web/src/shared/api/types.ts` (append 5 interfaces)
- Create: `web/src/shared/api/admin.ts`
- Create: `web/tests/unit/shared/api/admin.test.ts`

- [ ] **Step 1.1: Append DTOs to `types.ts`**

Append to the bottom of `web/src/shared/api/types.ts`:

```ts
export interface AdminUserRow {
  id: string
  email: string
  created_at: string
  disabled_at?: string
  is_admin: boolean
}

export interface InvitationRow {
  code_prefix: string
  note: string
  created_at: string
  expires_at?: string
  consumed_at?: string
  consumed_by?: string
}

export interface InvitationCreated {
  plaintext: string
  code_prefix: string
  note: string
  expires_at?: string
  created_at: string
}

// /admin/api/invitations returns InvitationCreated when count == 1 and
// {invites: InvitationCreated[]} when count > 1. The helper normalises
// both shapes into an InvitationCreated[].
export interface InvitationCreateBatchResponse {
  invites: InvitationCreated[]
}

export interface AdminConfig {
  rate_limit_per_minute: number
  max_connections_per_key: number
  default_rate_limit_per_minute: number
  default_max_connections_per_key: number
  version: string
}

export interface AdminConfigUpdate {
  rate_limit_per_minute: number
  max_connections_per_key: number
}

export interface ResetPasswordResponse {
  plaintext: string
}
```

- [ ] **Step 1.2: Write the failing tests (TDD red)**

Create `web/tests/unit/shared/api/admin.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  listUsers,
  listInvitations,
  createInvitation,
  resetUserPassword,
  disableUser,
  promoteUser,
  demoteUser,
  getAdminConfig,
  setAdminConfig,
} from '@shared/api/admin'
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

describe('admin /admin/api/users', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('listUsers GETs /admin/api/users', async () => {
    const rows = [{ id: 'u1', email: 'a@b', created_at: '2026-01-01T00:00:00Z', is_admin: false }]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, rows))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listUsers()

    expect(result).toEqual(rows)
    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method || 'GET').toBe('GET')
  })

  it('resetUserPassword POSTs and returns plaintext', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { plaintext: 'tmp_secret' }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await resetUserPassword('u1')

    expect(result).toEqual({ plaintext: 'tmp_secret' })
    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users/u1/reset-password')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')
  })

  it('disableUser POSTs the disable endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { status: 'disabled' }))
    vi.stubGlobal('fetch', fetchMock)

    await disableUser('u1')

    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users/u1/disable')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')
  })

  it('promoteUser POSTs the admin endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await promoteUser('u1')

    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users/u1/admin')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')
    expect(new Headers((fetchMock.mock.calls[0]![1] as RequestInit).headers).get('X-CSRF-Token')).toBe('csrf-test')
  })

  it('demoteUser DELETEs the admin endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await demoteUser('u1')

    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users/u1/admin')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('DELETE')
  })

  it('demoteUser percent-encodes path segments', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await demoteUser('odd id/with slash')

    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/users/odd%20id%2Fwith%20slash/admin')
  })
})

describe('admin /admin/api/invitations', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('listInvitations GETs', async () => {
    const rows = [{ code_prefix: 'inv_abc', note: 'colleague', created_at: '2026-01-01T00:00:00Z' }]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, rows))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listInvitations()

    expect(result).toEqual(rows)
    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/invitations')
  })

  it('createInvitation count=1 unwraps the single-shape response to a 1-element array', async () => {
    const created = { plaintext: 'inv_full', code_prefix: 'inv_full_pfx', note: 'x', created_at: '2026-01-01T00:00:00Z' }
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, created))
    vi.stubGlobal('fetch', fetchMock)

    const result = await createInvitation({ note: 'x', count: 1 })

    expect(result).toEqual([created])
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({ note: 'x', count: 1 })
  })

  it('createInvitation count>1 returns the invites array', async () => {
    const invites = [
      { plaintext: 'inv_full_1', code_prefix: 'p1', note: 'x', created_at: '2026-01-01T00:00:00Z' },
      { plaintext: 'inv_full_2', code_prefix: 'p2', note: 'x', created_at: '2026-01-01T00:00:00Z' },
    ]
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, { invites }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await createInvitation({ note: 'x', count: 2 })

    expect(result).toEqual(invites)
  })

  it('createInvitation forwards expires_at when provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, { plaintext: 'p', code_prefix: 'c', note: '', created_at: '' }))
    vi.stubGlobal('fetch', fetchMock)

    await createInvitation({ count: 1, expires_at: '2026-02-01T00:00:00Z' })

    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({ count: 1, expires_at: '2026-02-01T00:00:00Z' })
  })
})

describe('admin /admin/api/config', () => {
  beforeEach(() => {
    clearCsrfToken()
    setCsrfToken('csrf-test')
    vi.restoreAllMocks()
  })

  it('getAdminConfig GETs and returns AdminConfig', async () => {
    const cfg = {
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    }
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, cfg))
    vi.stubGlobal('fetch', fetchMock)

    const result = await getAdminConfig()

    expect(result).toEqual(cfg)
    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/config')
  })

  it('setAdminConfig PUTs the body and returns the refreshed config', async () => {
    const cfg = {
      rate_limit_per_minute: 200,
      max_connections_per_key: 32,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    }
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, cfg))
    vi.stubGlobal('fetch', fetchMock)

    const result = await setAdminConfig({ rate_limit_per_minute: 200, max_connections_per_key: 32 })

    expect(result).toEqual(cfg)
    expect(fetchMock.mock.calls[0]![0]).toBe('/admin/api/config')
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body as string)).toEqual({ rate_limit_per_minute: 200, max_connections_per_key: 32 })
  })
})
```

- [ ] **Step 1.3: Run tests to verify they fail**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -25
```
Expected: import errors on `@shared/api/admin` (the file doesn't exist yet). The PR-C totals stay green.

- [ ] **Step 1.4: Implement `admin.ts`**

Create `web/src/shared/api/admin.ts`:

```ts
import { apiFetch } from './client'
import type {
  AdminConfig,
  AdminConfigUpdate,
  AdminUserRow,
  InvitationCreateBatchResponse,
  InvitationCreated,
  InvitationRow,
  ResetPasswordResponse,
} from './types'

// User listing + role + status (admin/api/users).
export async function listUsers(): Promise<AdminUserRow[]> {
  const { data } = await apiFetch<AdminUserRow[]>('/admin/api/users')
  return data
}

export async function resetUserPassword(id: string): Promise<ResetPasswordResponse> {
  const { data } = await apiFetch<ResetPasswordResponse>(
    `/admin/api/users/${encodeURIComponent(id)}/reset-password`,
    { method: 'POST' },
  )
  return data
}

export async function disableUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/disable`, { method: 'POST' })
}

export async function promoteUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/admin`, { method: 'POST' })
}

export async function demoteUser(id: string): Promise<void> {
  await apiFetch(`/admin/api/users/${encodeURIComponent(id)}/admin`, { method: 'DELETE' })
}

// Invitations (admin/api/invitations).
export async function listInvitations(): Promise<InvitationRow[]> {
  const { data } = await apiFetch<InvitationRow[]>('/admin/api/invitations')
  return data
}

export interface CreateInvitationRequest {
  count: number
  note?: string
  expires_at?: string  // RFC3339; omit to let server default to now + 7 days.
}

// createInvitation normalises both server response shapes (single object
// when count==1, {invites: [...]} when count>1) to a flat array. Callers
// always get InvitationCreated[].
export async function createInvitation(req: CreateInvitationRequest): Promise<InvitationCreated[]> {
  const { data } = await apiFetch<InvitationCreated | InvitationCreateBatchResponse>(
    '/admin/api/invitations',
    { method: 'POST', body: JSON.stringify(req) },
  )
  if (data && typeof data === 'object' && 'invites' in data) {
    return data.invites
  }
  return [data as InvitationCreated]
}

// Runtime limits (admin/api/config).
export async function getAdminConfig(): Promise<AdminConfig> {
  const { data } = await apiFetch<AdminConfig>('/admin/api/config')
  return data
}

export async function setAdminConfig(update: AdminConfigUpdate): Promise<AdminConfig> {
  const { data } = await apiFetch<AdminConfig>('/admin/api/config', {
    method: 'PUT',
    body: JSON.stringify(update),
  })
  return data
}
```

- [ ] **Step 1.5: Run tests to verify green**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: 62 (PR-C baseline) + 11 new admin = **73 passing**.

- [ ] **Step 1.6: Type-check + drift gate**

```bash
cd web && npx vue-tsc --noEmit
./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist
```
Both clean / exit 0.

- [ ] **Step 1.7: Commit**

```bash
git add web/src/shared/api/types.ts web/src/shared/api/admin.ts web/tests/unit/shared/api/admin.test.ts
git commit -m "$(cat <<'EOF'
feat(web): /admin/api/* helpers (users, invitations, config)

Layers admin helpers on top of PR-B's apiFetch:
- listUsers / promote / demote / reset-password / disable
- listInvitations / createInvitation (normalises single + batch shapes)
- getAdminConfig / setAdminConfig (runtime limits + effective defaults)

CSRF header is injected automatically by apiFetch on every mutating
call. 11 vitest cases verify HTTP method/path, body shape, and the
count==1 vs count>1 response unwrapping for createInvitation.
EOF
)"
```

---

## Task 2: Invitations tab component

**Files:**
- Create: `web/src/admin/tabs/Invitations.vue`
- Create: `web/tests/unit/admin/tabs/Invitations.test.ts`

- [ ] **Step 2.1: Write the failing test**

Create `web/tests/unit/admin/tabs/Invitations.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  listInvitations: vi.fn(),
  createInvitation: vi.fn(),
}))

import Invitations from '@/admin/tabs/Invitations.vue'
import { listInvitations, createInvitation } from '@shared/api/admin'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Invitations) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

describe('Invitations.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('lists invitations on mount', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>).mockResolvedValue([
      { code_prefix: 'inv_abc', note: 'colleague', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('inv_abc')
    expect(wrapper.text()).toContain('colleague')
  })

  it('shows an empty-state message when no invitations', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('No invitations yet')
  })

  it('creates a single invitation and shows the plaintext once', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { code_prefix: 'inv_xyz', note: 'laptop', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createInvitation as ReturnType<typeof vi.fn>).mockResolvedValue([
      { plaintext: 'inv_full_secret', code_prefix: 'inv_xyz', note: 'laptop', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="invite-note"]').setValue('laptop')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createInvitation).toHaveBeenCalledWith({ count: 1, note: 'laptop' })
    expect(wrapper.text()).toContain('inv_full_secret')
    expect(listInvitations).toHaveBeenCalledTimes(2)
  })

  it('creates a bulk batch and shows every plaintext', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { code_prefix: 'inv_a', note: 'team', created_at: '2026-01-01T00:00:00Z' },
        { code_prefix: 'inv_b', note: 'team', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createInvitation as ReturnType<typeof vi.fn>).mockResolvedValue([
      { plaintext: 'inv_p1', code_prefix: 'inv_a', note: 'team', created_at: '2026-01-01T00:00:00Z' },
      { plaintext: 'inv_p2', code_prefix: 'inv_b', note: 'team', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="invite-note"]').setValue('team')
    await wrapper.find('[data-testid="invite-count"]').setValue('2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createInvitation).toHaveBeenCalledWith({ count: 2, note: 'team' })
    expect(wrapper.text()).toContain('inv_p1')
    expect(wrapper.text()).toContain('inv_p2')
  })
})
```

- [ ] **Step 2.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```
Expected: import error (the component does not exist yet).

- [ ] **Step 2.3: Implement `Invitations.vue`**

Create `web/src/admin/tabs/Invitations.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NDataTable,
  NSpace,
  NInput,
  NInputNumber,
  NButton,
  NAlert,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listInvitations, createInvitation } from '@shared/api/admin'
import type { InvitationCreated, InvitationRow } from '@shared/api/types'

const rows = ref<InvitationRow[]>([])
const loading = ref(true)
const submitting = ref(false)
const noteInput = ref('')
const countInput = ref<number>(1)
const expiresInput = ref('')
const newSecrets = ref<InvitationCreated[]>([])
const message = useMessage()

function fmt(iso: string | undefined): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

const columns: DataTableColumns<InvitationRow> = [
  { title: 'Prefix', key: 'code_prefix', render: (r) => h('code', {}, r.code_prefix) },
  { title: 'Note', key: 'note' },
  { title: 'Created', key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: 'Expires', key: 'expires_at', render: (r) => fmt(r.expires_at) },
  {
    title: 'Consumed',
    key: 'consumed_at',
    render: (r) =>
      r.consumed_at
        ? h('span', {}, `${fmt(r.consumed_at)}${r.consumed_by ? ' · ' + r.consumed_by : ''}`)
        : h(NTag, { size: 'small', type: 'default' }, { default: () => 'unused' }),
  },
]

async function reload() {
  loading.value = true
  try {
    rows.value = await listInvitations()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load invitations.')
  } finally {
    loading.value = false
  }
}

async function onCreate(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  submitting.value = true
  try {
    const count = Math.max(1, Math.min(50, Math.round(countInput.value || 1)))
    const note = noteInput.value.trim()
    const req: { count: number; note?: string; expires_at?: string } = { count }
    if (note) req.note = note
    if (expiresInput.value) {
      // datetime-local control gives "YYYY-MM-DDTHH:mm"; pad to ISO with Z.
      req.expires_at = new Date(expiresInput.value).toISOString()
    }
    const created = await createInvitation(req)
    newSecrets.value = created
    noteInput.value = ''
    expiresInput.value = ''
    countInput.value = 1
    await reload()
  } catch (err) {
    if (err instanceof ApiError) message.error('Create failed: ' + err.code)
  } finally {
    submitting.value = false
  }
}

// h() import required for column render functions.
import { h } from 'vue'

onMounted(reload)
</script>

<template>
  <n-card title="Invitations" :bordered="false">
    <form @submit="onCreate" autocomplete="off" class="create-form">
      <n-space :wrap="false" align="end">
        <div class="field">
          <label class="field-label">Note</label>
          <n-input
            v-model:value="noteInput"
            type="text"
            placeholder="optional"
            :input-props="{ 'data-testid': 'invite-note', autocomplete: 'off' }"
          />
        </div>
        <div class="field">
          <label class="field-label">Count</label>
          <n-input-number
            v-model:value="countInput"
            :min="1"
            :max="50"
            :input-props="{ 'data-testid': 'invite-count' }"
          />
        </div>
        <div class="field">
          <label class="field-label">Expires</label>
          <n-input
            v-model:value="expiresInput"
            type="text"
            placeholder="YYYY-MM-DDTHH:mm"
            :input-props="{ type: 'datetime-local', 'data-testid': 'invite-expires' }"
          />
        </div>
        <n-button type="primary" attr-type="submit" :loading="submitting" :disabled="submitting">
          Create
        </n-button>
      </n-space>
    </form>

    <n-alert
      v-for="(s, i) in newSecrets"
      :key="i"
      type="success"
      :show-icon="false"
      class="secret-alert"
    >
      <div class="secret-msg">
        Copy this invitation now{{ s.note ? ` (${s.note})` : '' }} — it will not be shown again.
      </div>
      <code class="secret-display">{{ s.plaintext }}</code>
    </n-alert>

    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="loading"
      size="small"
      :bordered="false"
      :pagination="false"
    />
    <p v-if="!loading && rows.length === 0" class="empty">No invitations yet.</p>
  </n-card>
</template>

<style scoped>
.create-form { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.25rem; }
.field-label {
  font-size: 0.75rem;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.secret-alert { margin: 0.5rem 0; }
.secret-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.secret-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
```

- [ ] **Step 2.4: Run tests + typecheck + drift gate**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist
```
Expected: 73 + 4 = **77 passing**; vue-tsc clean; drift exit 0.

- [ ] **Step 2.5: Commit**

```bash
git add web/src/admin/tabs/Invitations.vue web/tests/unit/admin/tabs/Invitations.test.ts
git commit -m "feat(web): Invitations tab (list, create single/bulk, plaintext-once)"
```

---

## Task 3: Users tab component

**Files:**
- Create: `web/src/admin/tabs/Users.vue`
- Create: `web/tests/unit/admin/tabs/Users.test.ts`

- [ ] **Step 3.1: Write the failing test**

Create `web/tests/unit/admin/tabs/Users.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  listUsers: vi.fn(),
  promoteUser: vi.fn(),
  demoteUser: vi.fn(),
  resetUserPassword: vi.fn(),
  disableUser: vi.fn(),
}))

import Users from '@/admin/tabs/Users.vue'
import {
  listUsers,
  promoteUser,
  demoteUser,
  resetUserPassword,
  disableUser,
} from '@shared/api/admin'
import { ApiError } from '@shared/api/client'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Users) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

async function clickConfirm() {
  const confirmBtn = document.querySelector(
    '.n-popconfirm .n-button--primary-type',
  ) as HTMLButtonElement | null
  confirmBtn?.click()
  await flushPromises()
}

describe('Users.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('lists users with role + status labels', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
      { id: 'u2', email: 'plain@example', created_at: '2026-01-02T00:00:00Z', is_admin: false },
      { id: 'u3', email: 'gone@example',  created_at: '2026-01-03T00:00:00Z', is_admin: false, disabled_at: '2026-02-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('admin@example')
    expect(text).toContain('plain@example')
    expect(text).toContain('gone@example')
    expect(text).toContain('admin')
    expect(text).toContain('disabled')
  })

  it('promotes a user and reloads', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
      ])
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
      ])
    ;(promoteUser as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="promote-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(promoteUser).toHaveBeenCalledWith('u2')
    expect(listUsers).toHaveBeenCalledTimes(2)
  })

  it('reset-password shows the temporary plaintext', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
    ])
    ;(resetUserPassword as ReturnType<typeof vi.fn>).mockResolvedValue({ plaintext: 'tmp_super_secret' })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="reset-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(resetUserPassword).toHaveBeenCalledWith('u2')
    expect(wrapper.text()).toContain('tmp_super_secret')
  })

  it('disable user calls the endpoint and reloads', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
      ])
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false, disabled_at: '2026-03-01T00:00:00Z' },
      ])
    ;(disableUser as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="disable-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(disableUser).toHaveBeenCalledWith('u2')
    expect(listUsers).toHaveBeenCalledTimes(2)
  })

  it('demote on the last admin surfaces the last_admin message', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
    ])
    ;(demoteUser as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'last_admin', null),
    )
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="demote-u1"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(wrapper.text()).toContain('last admin')
  })

  it('cannot_demote_self surfaces a specific message', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
    ])
    ;(demoteUser as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'cannot_demote_self', null),
    )
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="demote-u1"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(wrapper.text()).toContain('demote yourself')
  })
})
```

- [ ] **Step 3.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```

- [ ] **Step 3.3: Implement `Users.vue`**

Create `web/src/admin/tabs/Users.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NCard,
  NDataTable,
  NButton,
  NPopconfirm,
  NTag,
  NAlert,
  NSpace,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import {
  listUsers,
  promoteUser,
  demoteUser,
  resetUserPassword,
  disableUser,
} from '@shared/api/admin'
import type { AdminUserRow } from '@shared/api/types'

const rows = ref<AdminUserRow[]>([])
const loading = ref(true)
const errorMsg = ref('')
const secrets = ref<{ label: string; plaintext: string }[]>([])
const message = useMessage()

function fmt(iso: string | undefined): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'last_admin') return "Can't demote the last admin — promote another user first."
    if (e.code === 'cannot_demote_self') return "You can't demote yourself."
  }
  return 'Action failed.'
}

async function reload() {
  loading.value = true
  errorMsg.value = ''
  try {
    rows.value = await listUsers()
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Failed to load users.'
  } finally {
    loading.value = false
  }
}

async function onPromote(id: string) {
  errorMsg.value = ''
  try {
    await promoteUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onDemote(id: string) {
  errorMsg.value = ''
  try {
    await demoteUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onResetPassword(id: string, email: string) {
  errorMsg.value = ''
  secrets.value = []
  try {
    const { plaintext } = await resetUserPassword(id)
    secrets.value = [{ label: `Temporary password for ${email}`, plaintext }]
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onDisable(id: string) {
  errorMsg.value = ''
  try {
    await disableUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

function statusCell(row: AdminUserRow) {
  if (row.disabled_at) {
    return h(NTag, { size: 'small', type: 'error' }, { default: () => `disabled ${fmt(row.disabled_at)}` })
  }
  if (row.is_admin) {
    return h(NTag, { size: 'small', type: 'success' }, { default: () => 'admin' })
  }
  return h(NTag, { size: 'small', type: 'default' }, { default: () => 'active' })
}

function actionsCell(row: AdminUserRow) {
  if (row.disabled_at) return null
  const adminBtn = row.is_admin
    ? h(
        NPopconfirm,
        { onPositiveClick: () => onDemote(row.id) },
        {
          trigger: () =>
            h(
              NButton,
              { size: 'small', 'data-testid': `demote-${row.id}` },
              { default: () => 'Demote' },
            ),
          default: () => 'Demote this admin?',
        },
      )
    : h(
        NPopconfirm,
        { onPositiveClick: () => onPromote(row.id) },
        {
          trigger: () =>
            h(
              NButton,
              { size: 'small', 'data-testid': `promote-${row.id}` },
              { default: () => 'Promote' },
            ),
          default: () => 'Promote this user to admin?',
        },
      )
  const resetBtn = h(
    NPopconfirm,
    { onPositiveClick: () => onResetPassword(row.id, row.email) },
    {
      trigger: () =>
        h(
          NButton,
          { size: 'small', 'data-testid': `reset-${row.id}` },
          { default: () => 'Reset password' },
        ),
      default: () => 'Reset password? A new temporary password is shown once.',
    },
  )
  const disableBtn = h(
    NPopconfirm,
    { onPositiveClick: () => onDisable(row.id) },
    {
      trigger: () =>
        h(
          NButton,
          { size: 'small', type: 'error', 'data-testid': `disable-${row.id}` },
          { default: () => 'Disable' },
        ),
      default: () => 'Disable this user? They are signed out and cannot log in.',
    },
  )
  return h(NSpace, {}, { default: () => [adminBtn, resetBtn, disableBtn] })
}

const columns: DataTableColumns<AdminUserRow> = [
  { title: 'Email', key: 'email' },
  { title: 'ID', key: 'id', render: (r) => h('code', {}, r.id) },
  { title: 'Created', key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: 'Status', key: 'status', render: statusCell },
  { title: 'Actions', key: 'actions', render: actionsCell },
]

onMounted(reload)
</script>

<template>
  <n-card title="Users" :bordered="false">
    <n-alert
      v-for="(s, i) in secrets"
      :key="i"
      type="success"
      :show-icon="false"
      class="secret-alert"
    >
      <div class="secret-msg">{{ s.label }} — copy it now, only shown once.</div>
      <code class="secret-display">{{ s.plaintext }}</code>
    </n-alert>
    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="loading"
      size="small"
      :bordered="false"
      :pagination="false"
    />
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </n-card>
</template>

<style scoped>
.secret-alert { margin-bottom: 0.5rem; }
.secret-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.secret-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
```

- [ ] **Step 3.4: Run tests + typecheck + drift gate**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist
```
Expected: 77 + 6 = **83 passing**; vue-tsc clean; drift exit 0.

- [ ] **Step 3.5: Commit**

```bash
git add web/src/admin/tabs/Users.vue web/tests/unit/admin/tabs/Users.test.ts
git commit -m "feat(web): Users tab (list, promote/demote/reset/disable)"
```

---

## Task 4: Config tab component

**Files:**
- Create: `web/src/admin/tabs/Config.vue`
- Create: `web/tests/unit/admin/tabs/Config.test.ts`

- [ ] **Step 4.1: Write the failing test**

Create `web/tests/unit/admin/tabs/Config.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  getAdminConfig: vi.fn(),
  setAdminConfig: vi.fn(),
}))

import Config from '@/admin/tabs/Config.vue'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Config) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

describe('Config.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('loads and shows the effective fallback when stored values are 0', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('effective: 120')
    expect(text).toContain('effective: 16')
    expect(text).toContain('v0.1.79')
  })

  it('PUTs the new values on save', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    })
    ;(setAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 240,
      max_connections_per_key: 32,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="cfg-rate"]').setValue('240')
    await wrapper.find('[data-testid="cfg-conn"]').setValue('32')
    await wrapper.find('[data-testid="cfg-save"]').trigger('click')
    await flushPromises()

    expect(setAdminConfig).toHaveBeenCalledWith({
      rate_limit_per_minute: 240,
      max_connections_per_key: 32,
    })
  })

  it('negative values disable the limit entirely (effective: disabled)', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: -1,
      max_connections_per_key: -1,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('effective: disabled')
  })
})
```

- [ ] **Step 4.2: Run tests to verify they fail**

```bash
cd web && npm test 2>&1 | tail -10
```

- [ ] **Step 4.3: Implement `Config.vue`**

Create `web/src/admin/tabs/Config.vue`:

```vue
<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NCard, NForm, NFormItem, NInputNumber, NButton, NSpace, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'
import type { AdminConfig } from '@shared/api/types'

const cfg = ref<AdminConfig | null>(null)
const rateInput = ref<number>(0)
const connInput = ref<number>(0)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const message = useMessage()

function effectiveLabel(stored: number, fallback: number): string {
  if (stored < 0) return 'effective: disabled'
  if (stored === 0) return `effective: ${fallback}`
  return `effective: ${stored}`
}

const rateEffective = computed(() =>
  cfg.value ? effectiveLabel(rateInput.value, cfg.value.default_rate_limit_per_minute) : '',
)
const connEffective = computed(() =>
  cfg.value ? effectiveLabel(connInput.value, cfg.value.default_max_connections_per_key) : '',
)

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const c = await getAdminConfig()
    cfg.value = c
    rateInput.value = c.rate_limit_per_minute
    connInput.value = c.max_connections_per_key
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Failed to load config.'
  } finally {
    loading.value = false
  }
}

async function onSave() {
  if (!cfg.value || saving.value) return
  errorMsg.value = ''
  saving.value = true
  try {
    const updated = await setAdminConfig({
      rate_limit_per_minute: Math.round(rateInput.value),
      max_connections_per_key: Math.round(connInput.value),
    })
    cfg.value = updated
    rateInput.value = updated.rate_limit_per_minute
    connInput.value = updated.max_connections_per_key
    message.success('Saved.')
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Save failed.'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <n-card title="Runtime limits" :bordered="false">
    <p class="hint">
      <strong>0</strong> means "use the built-in default"; <strong>negative</strong> disables
      the limit entirely. Changes apply immediately and persist to the admin config file.
    </p>
    <n-form v-if="cfg" label-placement="top" require-mark-placement="right-hanging">
      <n-form-item label="Rate limit (requests/min per IP+token)" :show-feedback="false">
        <n-space :wrap="false" align="center">
          <n-input-number
            v-model:value="rateInput"
            :show-button="false"
            :input-props="{ 'data-testid': 'cfg-rate' }"
          />
          <span class="muted">{{ rateEffective }}</span>
        </n-space>
      </n-form-item>
      <n-form-item label="Max WS connections (per IP+token)" :show-feedback="false">
        <n-space :wrap="false" align="center">
          <n-input-number
            v-model:value="connInput"
            :show-button="false"
            :input-props="{ 'data-testid': 'cfg-conn' }"
          />
          <span class="muted">{{ connEffective }}</span>
        </n-space>
      </n-form-item>
      <n-space>
        <n-button
          type="primary"
          :loading="saving"
          :disabled="saving"
          data-testid="cfg-save"
          @click="onSave"
        >
          Save
        </n-button>
        <span class="muted version">Version: <code>{{ cfg.version }}</code></span>
      </n-space>
    </n-form>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </n-card>
</template>

<style scoped>
.hint { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.muted { color: var(--fg-dim); font-size: 0.875rem; }
.version code { color: var(--fg); }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
```

- [ ] **Step 4.4: Run tests + typecheck + drift gate**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -10
cd web && npx vue-tsc --noEmit
./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist
```
Expected: 83 + 3 = **86 passing**; vue-tsc clean; drift exit 0.

- [ ] **Step 4.5: Commit**

```bash
git add web/src/admin/tabs/Config.vue web/tests/unit/admin/tabs/Config.test.ts
git commit -m "feat(web): Config tab (runtime limits with effective-value display)"
```

---

## Task 5: Admin App.vue + entry HTML + vite.config.ts + embed sync

**Files:**
- Create: `web/src/admin/main.ts`
- Create: `web/src/admin/App.vue`
- Create: `web/admin/index.html`
- Modify: `web/vite.config.ts`
- Create: `web/tests/unit/admin/App.test.ts`
- Regenerate: `internal/relay/web-dist/**`

- [ ] **Step 5.1: Write `main.ts`**

Create `web/src/admin/main.ts`:

```ts
import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'

createApp(App).mount('#app')
```

- [ ] **Step 5.2: Write `App.vue`**

Create `web/src/admin/App.vue`:

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
import Invitations from './tabs/Invitations.vue'
import Users from './tabs/Users.vue'
import Config from './tabs/Config.vue'

const TAB_NAMES = ['invitations', 'users', 'config'] as const
type TabName = (typeof TAB_NAMES)[number]

function nameFromHash(): TabName {
  const h = location.hash.replace(/^#/, '')
  return TAB_NAMES.includes(h as TabName) ? (h as TabName) : 'invitations'
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
      <Topbar active="admin" />
      <main class="admin-page">
        <n-tabs
          :value="activeTab"
          type="line"
          animated
          @update:value="onTabChange"
        >
          <n-tab-pane name="invitations" tab="Invitations">
            <Invitations />
          </n-tab-pane>
          <n-tab-pane name="users" tab="Users">
            <Users />
          </n-tab-pane>
          <n-tab-pane name="config" tab="Config">
            <Config />
          </n-tab-pane>
        </n-tabs>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.admin-page {
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  background: var(--bg);
  color: var(--fg);
  min-height: calc(100vh - 80px);
}
</style>
```

- [ ] **Step 5.3: Write `web/admin/index.html`**

Create `web/admin/index.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <meta name="page" content="admin" />
  <meta name="theme-color" content="#0b1020" />
  <title>AT Term · admin</title>
  <link rel="icon" href="/icon.svg" type="image/svg+xml" />
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/admin/main.ts"></script>
</body>
</html>
```

- [ ] **Step 5.4: Add the admin entry to `vite.config.ts`**

Update `rollupOptions.input` in `web/vite.config.ts`:

```ts
    rollupOptions: {
      input: {
        login:    fileURLToPath(new URL('./login.html',        import.meta.url)),
        signup:   fileURLToPath(new URL('./signup.html',       import.meta.url)),
        settings: fileURLToPath(new URL('./settings.html',     import.meta.url)),
        admin:    fileURLToPath(new URL('./admin/index.html',  import.meta.url)),
      },
    },
```

Leave the rest of the config alone.

- [ ] **Step 5.5: Write the App component test**

Create `web/tests/unit/admin/App.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: true }),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))
vi.mock('@shared/api/admin', () => ({
  listInvitations: vi.fn().mockResolvedValue([]),
  createInvitation: vi.fn(),
  listUsers: vi.fn().mockResolvedValue([]),
  promoteUser: vi.fn(),
  demoteUser: vi.fn(),
  resetUserPassword: vi.fn(),
  disableUser: vi.fn(),
  getAdminConfig: vi.fn().mockResolvedValue({
    rate_limit_per_minute: 0,
    max_connections_per_key: 0,
    default_rate_limit_per_minute: 120,
    default_max_connections_per_key: 16,
    version: 'v0.1.79',
  }),
  setAdminConfig: vi.fn(),
}))

import App from '@/admin/App.vue'

describe('Admin App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    document.body.innerHTML = ''
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/admin/', search: '', hash: '', assign: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders all three tab labels', async () => {
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Invitations')
    expect(text).toContain('Users')
    expect(text).toContain('Config')
  })

  it('opens the tab indicated by the hash on first paint', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#users' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Users panel triggers listUsers; empty list renders its container.
    expect(wrapper.html()).toContain('n-data-table')
  })

  it('falls back to the invitations tab when hash is invalid', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#bogus' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Invitations panel's empty state message proves we landed on the
    // fallback tab. Naive UI's tab content stays mounted by default.
    expect(wrapper.text()).toContain('No invitations yet.')
  })
})
```

- [ ] **Step 5.6: Run tests + build**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
cd web && npm test 2>&1 | tail -10
cd web && npm run build 2>&1 | tail -15
cd ..
ls web/dist
ls web/dist/admin 2>/dev/null
```
Expected:
- vitest: 86 + 3 = **89 passing**
- `npm run build` succeeds; `web/dist/` contains `login.html`, `signup.html`, `settings.html`, `admin/index.html`, and `assets/`

- [ ] **Step 5.7: Sync embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
./scripts/build-web.sh
grep -l '<div id="app">' internal/relay/web-dist/*.html internal/relay/web-dist/admin/*.html
```
Expected: `internal/relay/web-dist/{login,signup,settings,admin/index}.html` all contain `<div id="app">`; `internal/relay/web-dist/index.html` does NOT (still legacy).

- [ ] **Step 5.8: Determinism re-run**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 5.9: Commit (Vite entry + embed regen in same commit)**

```bash
git add web/src/admin web/admin/index.html web/vite.config.ts web/tests/unit/admin/App.test.ts internal/relay/web-dist
git commit -m "$(cat <<'EOF'
feat(web): admin/index.html Vue entry with 3 tabs (NTabs + hash routing)

App.vue wraps the dark Naive UI theme, the shared Topbar, and an
NTabs binding active value to location.hash (so /admin/#users
deep-links to the Users tab). The 3 tab components render inside
NTabPane panels; each one already has its own unit tests from prior
tasks. Vite multi-entry input gains the admin entry under web/admin/.

The embed is regenerated in the same commit so the drift gate stays
green and the new HTML/assets are byte-identical to the build output.
EOF
)"
```

---

## Task 6: Delete legacy admin sources + SW cache bump + legacy test trim

**Files:**
- Delete: `web/legacy/admin/index.html`
- Delete: `web/legacy/admin/admin.js`
- Delete: `web/legacy/admin/admin-invitations.js`
- Delete: `web/legacy/admin/admin-users.js`
- Delete: `web/legacy/admin/admin.test.mjs`
- Modify: `web/legacy/sw.js`
- Modify: `web/legacy/no-raw-colors.test.mjs`
- Modify: `web/legacy/terminal-fit.test.mjs` (if it asserts on `./admin/...`)
- Regenerate: `internal/relay/web-dist/**`

- [ ] **Step 6.1: Delete the legacy admin sources**

```bash
git rm web/legacy/admin/index.html web/legacy/admin/admin.js web/legacy/admin/admin-invitations.js web/legacy/admin/admin-users.js web/legacy/admin/admin.test.mjs
# If the directory becomes empty, drop it too:
rmdir web/legacy/admin 2>/dev/null || true
```

- [ ] **Step 6.2: Update `web/legacy/sw.js` ASSETS**

Edit `web/legacy/sw.js`. Remove these three entries from the `ASSETS` array:

```js
  "./admin/admin-invitations.js",
  "./admin/admin-users.js",
  "./admin/admin.js",
```

The resulting `ASSETS` array becomes:

```js
const ASSETS = [
  "./",
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

- [ ] **Step 6.3: Compute and apply the new CACHE hex**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
node --test web/legacy/sw-cache-bump.test.mjs 2>&1 | tail -10
```
Expected: test fails with `Replace it with "at-term-web-<NEWHEX>"`. Copy the hex.

Edit `web/legacy/sw.js`. Replace `at-term-web-803ed9bd` (the value PR-C set) with the new hex.

- [ ] **Step 6.4: Confirm the bump test passes**

```bash
node --test web/legacy/sw-cache-bump.test.mjs 2>&1 | tail -5
```
Expected: passes.

- [ ] **Step 6.5: Trim other legacy tests if they reference the deleted files**

```bash
grep -rn 'admin/' web/legacy/no-raw-colors.test.mjs web/legacy/terminal-fit.test.mjs web/legacy/nav.test.mjs 2>/dev/null
```

For each match where the entry is a filesystem-path assertion (e.g. ALLOWED map keyed by `web/legacy/admin/index.html`, an SW precache assertion on `./admin/admin.js`, or a PAGES entry in nav), remove that single entry. Keep the broader invariant (e.g. style.css color discipline; SW precache atomicity for the remaining bootstrap files).

After edits:

```bash
node --test web/legacy/*.test.mjs 2>&1 | tail -5
```
Expected: green. Count drops further (the admin-related cases gone).

- [ ] **Step 6.6: Full legacy + contract + vitest sweep**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
node --test web/legacy/*.test.mjs 2>&1 | tail -5
node --test web/tests/contract/*.test.mjs 2>&1 | tail -5
(cd web && npm test 2>&1) | tail -5
```
All green.

- [ ] **Step 6.7: Sync embed**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
./scripts/build-web.sh
git status --short -- internal/relay/web-dist | head -10
```
Expected diff:
- `internal/relay/web-dist/sw.js` updated (new CACHE + trimmed ASSETS)
- `internal/relay/web-dist/admin/admin.js`, `admin-invitations.js`, `admin-users.js` deleted (legacy source removed)
- `internal/relay/web-dist/admin/index.html` unchanged — still the Vue version from Task 5

Stage and verify determinism:

```bash
git add internal/relay/web-dist
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 6.8: Commit**

```bash
git add web/legacy/sw.js web/legacy/no-raw-colors.test.mjs web/legacy/terminal-fit.test.mjs web/legacy/nav.test.mjs \
        web/legacy/admin internal/relay/web-dist
# git rm already staged the legacy deletions
git commit -m "$(cat <<'EOF'
chore(web): drop legacy admin sources; bump SW cache name

Vite-built admin/index.html lands in web-dist via the overlay in
scripts/build-web.sh, so the legacy copies are dead code. sw.js's
ASSETS no longer references admin/admin*.js, and the CACHE hash bumps
so installed PWA clients re-fetch on next activation. Legacy contract
tests that named those files in their allow lists are trimmed
accordingly.
EOF
)"
```

---

## Task 7: Final smoke + PR

- [ ] **Step 7.1: Full test matrix**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet ./...
go test ./...
(cd web && npm run build && npm test && npm run test:contract)
node --test web/legacy/*.test.mjs
```
All green.

- [ ] **Step 7.2: Drift gate**

```bash
./scripts/build-web.sh
git diff --exit-code -- internal/relay/web-dist
```
Expected: exit 0.

- [ ] **Step 7.3: Working tree clean check**

```bash
git status --short
```
Expected: only the pre-existing untracked artifacts.

- [ ] **Step 7.4: End-to-end relay smoke**

```bash
PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
ATTERM_BOOTSTRAP_ADMIN_EMAIL='you@example.com' \
ATTERM_BOOTSTRAP_ADMIN_PASSWORD='Bootstrap-Pass-2026!' \
go run ./cmd/atterm-relay --addr 127.0.0.1:18080 --dev-insecure &
RELAY_PID=$!
sleep 3

echo '--- /admin/ anon redirect ---'
curl -sI http://127.0.0.1:18080/admin/        | head -1   # expect 302 → /login.html
curl -sI http://127.0.0.1:18080/admin/index.html | head -1  # expect 302 anon

echo '--- /admin/api/* anon 401 ---'
curl -sI http://127.0.0.1:18080/admin/api/users        | head -1  # 401
curl -sI http://127.0.0.1:18080/admin/api/invitations  | head -1  # 401
curl -sI http://127.0.0.1:18080/admin/api/config       | head -1  # 401

echo '--- previous Vue pages still serve ---'
curl -sI http://127.0.0.1:18080/login.html    | head -1   # 200
curl -sI http://127.0.0.1:18080/signup.html   | head -1   # 200
curl -sI http://127.0.0.1:18080/settings.html | head -1   # 200

kill $RELAY_PID 2>/dev/null
wait $RELAY_PID 2>/dev/null
```

Each line must match. Stop the relay before continuing.

- [ ] **Step 7.5: Push and open PR**

```bash
git push -u origin web/vue-rewrite-pr-d-admin
PATH=/opt/homebrew/bin:$PATH gh pr create --title "web: PR-D admin 3 tabs (vue + naive-ui)" --body "$(cat <<'EOF'
## Summary

PR-D of the web client rewrite. Replaces `/admin/index.html` with a Vue 3 + Naive UI version. Three tabs (Invitations, Users, Config) wired via NTabs with hash-synced active state.

- `api/admin.ts` adds the 10 `/admin/api/*` helpers (list users + promote/demote/reset/disable; list/create invitations including the single-vs-batch shape unwrap; get/set runtime config). Full vitest coverage.
- Three tab components under `web/src/admin/tabs/`:
  - `Invitations.vue`: create (single + bulk + optional expires_at) with plaintext-once display; list with refresh.
  - `Users.vue`: list with status/role tags; per-row promote/demote/reset/disable wrapped in NPopconfirm; surfaces `last_admin` and `cannot_demote_self` server errors.
  - `Config.vue`: load + edit + save runtime limits; effective-value display (raw 0 → fallback default; raw <0 → disabled).
- Admin `App.vue` wraps the dark theme + NMessageProvider + NTabs; `location.hash` drives active tab so `/admin/#users` deep-links.
- `vite.config.ts` adds the `admin` entry; build emits `web/dist/admin/index.html` plus hashed bundles.
- Legacy `web/legacy/admin/**` deleted; `sw.js` ASSETS trimmed and CACHE bumped; `no-raw-colors` / `terminal-fit` / `nav` allow lists trimmed.

Note: spec listed two tabs (Users + Invitations); this PR keeps Config too because removing the runtime-limits editor would be a regression for admins who use it.

Spec: `docs/superpowers/specs/2026-05-17-web-vue-typescript-rewrite-design.md`
Plan: `docs/superpowers/plans/2026-05-17-web-vue-rewrite-pr-d-admin.md`

## Test Plan

- [x] `go vet ./... && go test ./...`
- [x] `cd web && npm run build && npm test && npm run test:contract` — vue-tsc clean
- [x] `node --test web/legacy/*.test.mjs`
- [x] Manual smoke: `/admin/` 302 → `/login.html` anon; `/admin/api/*` all 401 anon; `/login.html`, `/signup.html`, `/settings.html` still match prior behaviour
- [x] `./scripts/build-web.sh && git diff --exit-code -- internal/relay/web-dist` — exit 0
- [ ] Manual browser smoke: sign in with admin, walk through all 3 tabs, create + view invitations, promote/demote/reset/disable a test user, change a runtime limit
- [ ] CI green (web-tests, web-vue-tests with contract step, build-linux, etc.)
EOF
)"
```

---

## Self-Review Notes

**Spec coverage check:**

- Spec § Architecture / Routing — MPA: admin entry added (Task 5)
- Spec § Architecture / UI theme: App.vue wraps `<n-config-provider>` + `<n-message-provider>` (Task 5)
- Spec § Architecture / State: per-component `ref`/`reactive`, no Pinia (Tasks 2-4)
- Spec § Architecture / Auth & API: new `/admin/api/*` helpers ride apiFetch + CSRF cache from PR-B (Task 1)
- Spec § Architecture / WebSocket / proto: no WS work in PR-D — deferred to PR-E
- Spec § Testing / Unit tests: 27 new vitest cases (11 admin.ts + 4 Invitations + 6 Users + 3 Config + 3 App)
- Spec § Testing / Contract tests: existing `no-inline-script` walker picks up the new `admin/index.html` automatically
- Spec § Phasing — Phase B item 3 (admin): entire plan. Config tab is added on top because dropping it would be a regression; documented in the plan header and the PR body.
- Spec § Invariants — no `v-html`: every tab component avoids it
- Spec § Security / Sec-1 (CSP): no change; existing `style-src 'self' 'unsafe-inline'` covers Naive UI
- Spec § Security / Sec-2 (safeNext): not applicable (no auth flow here)
- Spec § Security / Sec-3 (CSWSH): no WS in PR-D
- Spec § Security / Sec-4 (CSRF): every mutating helper rides apiFetch (verified by admin.test.ts)
- Spec § Security / Sec-5 (SW precache): unchanged
- Spec § Security / Sec-6 (supply chain): no new deps
- Spec § Security / Sec-7 (build determinism): Task 5 drift gate exercises it
- Spec § Security / Sec-8 (paste image size): deferred to PR-E

**Placeholder scan:** every step has concrete code or commands; no "TBD" / "implement later" / "add appropriate error handling" / unnamed types.

**Type consistency:**

- `apiFetch<T>`, `ApiError`, `getNaiveOverrides`, `Topbar` — reused unchanged from earlier PRs
- `AdminUserRow { id, email, created_at, disabled_at?, is_admin }` — same in types.ts, admin.ts, Users.vue, App.test mock
- `InvitationRow { code_prefix, note, created_at, expires_at?, consumed_at?, consumed_by? }` — same in types.ts, admin.ts, Invitations.vue
- `InvitationCreated { plaintext, code_prefix, note, expires_at?, created_at }` — same everywhere
- `AdminConfig { rate_limit_per_minute, max_connections_per_key, default_*, version }` — same in types.ts, admin.ts, Config.vue
- `listUsers(): Promise<AdminUserRow[]>`, `promoteUser(id: string): Promise<void>`, `demoteUser(id: string): Promise<void>`, `resetUserPassword(id: string): Promise<ResetPasswordResponse>`, `disableUser(id: string): Promise<void>` — same signatures across files
- `listInvitations(): Promise<InvitationRow[]>`, `createInvitation(req: CreateInvitationRequest): Promise<InvitationCreated[]>` — same
- `getAdminConfig(): Promise<AdminConfig>`, `setAdminConfig(update: AdminConfigUpdate): Promise<AdminConfig>` — same
- Tab name union `'invitations' | 'users' | 'config'` — same in App.vue and `n-tab-pane name=` attributes

No drift detected.
