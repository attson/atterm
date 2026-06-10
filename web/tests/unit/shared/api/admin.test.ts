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
import { clearRelayConfig, saveRelayConfig } from '@shared/api/relay-config'

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
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null, allowInsecure: false })
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
    expect(new Headers((fetchMock.mock.calls[0]![1] as RequestInit).headers).get('Authorization')).toBe('Bearer ses_test')
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
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null, allowInsecure: false })
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
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null, allowInsecure: false })
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
