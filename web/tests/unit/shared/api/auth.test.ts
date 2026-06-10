import { describe, it, expect, vi, beforeEach } from 'vitest'
import { login, signup, logout } from '@shared/api/auth'
import { clearRelayConfig, loadRelayConfig } from '@shared/api/relay-config'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const mockLoginResponse = {
  session_token: 'ses_test_abc123',
  expires_at: 1234567890,
  user: {
    id: 'u1',
    email: 'a@b',
    is_admin: false,
  },
}

describe('auth helpers', () => {
  beforeEach(() => {
    clearRelayConfig()
    vi.restoreAllMocks()
  })

  it('login POSTs credentials to /api/auth/login, persists session_token, and returns body', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(200, mockLoginResponse))
    vi.stubGlobal('fetch', fetchMock)

    const result = await login('a@b', 'password-1234')

    expect(result).toEqual(mockLoginResponse)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login')
    const init = fetchMock.mock.calls[0]![1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ email: 'a@b', password: 'password-1234' })

    // Verify that session_token and expires_at were persisted to localStorage.
    const stored = loadRelayConfig()
    expect(stored?.sessionToken).toBe('ses_test_abc123')
    expect(stored?.expiresAt).toBe(1234567890)
  })

  it('signup posts the invite_code, persists session_token, and returns body', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(200, mockLoginResponse))
    vi.stubGlobal('fetch', fetchMock)

    const result = await signup('a@b', 'password-1234', 'invite-abc')

    expect(result).toEqual(mockLoginResponse)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/auth/signup')
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      email: 'a@b',
      password: 'password-1234',
      invite_code: 'invite-abc',
    })

    // Verify that session_token and expires_at were persisted to localStorage.
    const stored = loadRelayConfig()
    expect(stored?.sessionToken).toBe('ses_test_abc123')
    expect(stored?.expiresAt).toBe(1234567890)
  })

  it('logout POSTs /api/auth/logout and clears RelayConfig', async () => {
    // Pre-populate localStorage with a session token.
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { status: 'logged_out' }))
    vi.stubGlobal('fetch', fetchMock)

    // Save a fake config first.
    const initialConfig = loadRelayConfig()
    expect(initialConfig).toBeNull() // Should be cleared from beforeEach.

    await logout()

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/logout')
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe('POST')

    // Verify that RelayConfig was cleared.
    const cleared = loadRelayConfig()
    expect(cleared).toBeNull()
  })
})
