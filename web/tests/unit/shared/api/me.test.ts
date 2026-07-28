import { describe, it, expect, vi, beforeEach } from 'vitest'

import { getMe } from '@shared/api/me'
import { clearRelayConfig } from '@shared/api/relay-config'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

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
