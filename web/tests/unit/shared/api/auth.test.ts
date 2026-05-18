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
