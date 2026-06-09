import { describe, it, expect, vi, beforeEach } from 'vitest'
import { login, signup, logout } from '@shared/api/auth'
import { clearRelayConfig } from '@shared/api/relay-config'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('auth helpers', () => {
  beforeEach(() => {
    clearRelayConfig()
    vi.restoreAllMocks()
  })

  it('login POSTs credentials to /api/auth/login and returns body', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b' }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await login('a@b', 'password-1234')

    expect(result).toEqual({ user_id: 'u1', email: 'a@b' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login')
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ email: 'a@b', password: 'password-1234' })
  })

  it('signup posts the invite_code and returns body', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { user_id: 'u1', email: 'a@b' }))
    vi.stubGlobal('fetch', fetchMock)

    await signup('a@b', 'password-1234', 'invite-abc')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/auth/signup')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'a@b',
      password: 'password-1234',
      invite_code: 'invite-abc',
    })
  })

  it('logout POSTs /api/auth/logout', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { status: 'logged_out' }))
    vi.stubGlobal('fetch', fetchMock)

    await logout()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/logout')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')
  })
})
