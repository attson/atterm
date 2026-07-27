import { describe, it, expect, vi, beforeEach } from 'vitest'

import {
  getMe,
  listSessions,
  revokeSession,
  signOutOthers,
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
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null, allowInsecure: false })
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
