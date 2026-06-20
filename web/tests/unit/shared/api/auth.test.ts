import { describe, it, expect, vi, beforeEach } from 'vitest'

// The OPAQUE protocol runs in the bytemare WASM client (browser-only loader);
// stub it so these unit tests exercise auth.ts's HTTP/endpoint wiring without a
// real wasm instance. Real protocol interop is covered by opaque-interop.test.ts.
vi.mock('@shared/lib/opaqueWasm', () => ({
  opaqueRegisterInit: vi.fn(async () => ({ handle: 1, ke1: 'a2Ux' })),
  opaqueRegisterFinish: vi.fn(async () => ({ record: 'cmVj', exportKey: '' })),
  opaqueLoginInit: vi.fn(async () => ({ handle: 1, ke1: 'a2Ux' })),
  opaqueLoginFinish: vi.fn(async () => ({ ke3: 'a2Uz', exportKey: '', sessionKey: '' })),
}))

import { login, register, logout } from '@shared/api/auth'
import { clearRelayConfig, loadRelayConfig } from '@shared/api/relay-config'
import { clearAccountKey, hasAccountKey } from '@shared/api/account-key'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('OPAQUE auth helpers', () => {
  beforeEach(() => {
    clearRelayConfig()
    clearAccountKey()
    vi.restoreAllMocks()
  })

  // The OPAQUE protocol's protocol-bytes round-trip is exercised end to
  // end against the real Go relay in opaque-interop.test.ts. The unit
  // tests below focus on the auth.ts wrapper's local responsibilities:
  // logout wipes session + account_key; the two-stage POST shapes hit
  // the right endpoints; and failure paths don't leave the account_key
  // half-persisted.

  it('logout clears RelayConfig and account_key even when the relay returns 500', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(500, { error: 'down' }))
    vi.stubGlobal('fetch', fetchMock)

    await logout().catch(() => undefined)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/logout')
    expect(loadRelayConfig()).toBeNull()
    expect(hasAccountKey()).toBe(false)
  })

  it('register init wires the OPAQUE register endpoint, not the legacy /signup', async () => {
    // First fetch is /api/auth/register/init. Returning an invalid
    // base64 payload causes registerFinish to throw, which is fine —
    // the assertion is purely about the URL we hit on init.
    const fetchMock = vi.fn().mockResolvedValueOnce(
      jsonResponse(200, { registration_response: 'AAAA' }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await register('a@b', 'pw').catch(() => undefined)

    expect(fetchMock).toHaveBeenCalled()
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/register/init')
    const body = JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)
    expect(body.email).toBe('a@b')
    expect(typeof body.registration_ke).toBe('string')
    expect(body.registration_ke.length).toBeGreaterThan(0)
  })

  it('login init wires the OPAQUE login endpoint, not the legacy /login', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(200, { login_response: 'AAAA', session_id: 'sid' }))
    vi.stubGlobal('fetch', fetchMock)

    await login('a@b', 'pw').catch(() => undefined)

    expect(fetchMock).toHaveBeenCalled()
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login/init')
    const body = JSON.parse((fetchMock.mock.calls[0]![1] as RequestInit).body as string)
    expect(body.email).toBe('a@b')
    expect(typeof body.login_ke).toBe('string')
    expect(body.login_ke.length).toBeGreaterThan(0)
  })

  it('register surfaces an error and does not persist anything on init failure', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(500, { error: 'boom' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(register('a@b', 'pw')).rejects.toBeTruthy()
    expect(loadRelayConfig()).toBeNull()
    expect(hasAccountKey()).toBe(false)
  })

  it('login surfaces an error and does not persist anything on init failure', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(jsonResponse(500, { error: 'boom' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(login('a@b', 'pw')).rejects.toBeTruthy()
    expect(loadRelayConfig()).toBeNull()
    expect(hasAccountKey()).toBe(false)
  })
})
