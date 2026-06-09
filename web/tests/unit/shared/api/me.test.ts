import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  getMe,
  listSessions,
  revokeSession,
  signOutOthers,
  changePassword,
  deleteMe,
} from '@shared/api/me'
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

describe('me.ts /api/me/sessions', () => {
  beforeEach(() => {
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null })
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
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null })
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
    clearRelayConfig()
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null })
    vi.restoreAllMocks()
  })

  it('deleteMe DELETEs /api/me with email + password body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(emptyResponse(204))
    vi.stubGlobal('fetch', fetchMock)

    await deleteMe('a@b.example', 'password-1234')

    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/me')
    expect((init as RequestInit).method).toBe('DELETE')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'a@b.example',
      password: 'password-1234',
    })
  })
})

describe('me.ts /api/me get', () => {
  beforeEach(() => {
    clearRelayConfig()
    vi.restoreAllMocks()
  })

  it('getMe GETs /api/me and returns the body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { user_id: 'u1', email: 'a@b' }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await getMe()

    expect(result).toEqual({ user_id: 'u1', email: 'a@b' })
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me')
  })
})
