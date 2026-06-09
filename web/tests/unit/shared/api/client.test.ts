import { describe, it, expect } from 'vitest'
import { safeNext } from '@shared/api/client'

describe('safeNext', () => {
  it('returns / when input is null', () => {
    expect(safeNext(null)).toBe('/')
  })

  it('returns / when input is empty', () => {
    expect(safeNext('')).toBe('/')
  })

  it('accepts a same-origin path', () => {
    expect(safeNext('/settings.html')).toBe('/settings.html')
  })

  it('preserves query and hash on same-origin paths', () => {
    expect(safeNext('/admin/?tab=users#row-7')).toBe('/admin/?tab=users#row-7')
  })

  it('rejects protocol-relative URLs', () => {
    expect(safeNext('//evil.example')).toBe('/')
  })

  it('rejects backslash quirk', () => {
    expect(safeNext('/\\evil.example')).toBe('/')
  })

  it('rejects absolute URLs to other origins', () => {
    expect(safeNext('https://evil.example/login')).toBe('/')
  })

  it('rejects javascript: URLs', () => {
    expect(safeNext('javascript:alert(1)')).toBe('/')
  })

  it('rejects non-leading-slash paths (relative)', () => {
    expect(safeNext('settings.html')).toBe('/')
  })
})

import { afterEach, beforeEach, vi } from 'vitest'
import { ApiError, apiFetch } from '@shared/api/client'
import {
  saveRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'

function makeResponse(status: number, body: unknown, contentType = 'application/json'): Response {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(text, { status, headers: { 'Content-Type': contentType } })
}

describe('apiFetch headers', () => {
  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    vi.restoreAllMocks()
  })

  it('does not send X-CSRF-Token (CSRF is gone from the wire)', async () => {
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_test', expiresAt: null })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me/password', { method: 'POST', body: JSON.stringify({ a: 'b' }) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('does not send Authorization when no sessionToken is stored', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({}) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('Authorization')).toBe(false)
  })

  it('sets JSON Content-Type when body present and not pre-set', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({}) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('Content-Type')).toBe('application/json')
  })

  it('always sends credentials: omit (no cookies)', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    expect((init as RequestInit).credentials).toBe('omit')
  })
})

describe('apiFetch Bearer (unified path)', () => {
  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    vi.restoreAllMocks()
  })

  it('attaches Authorization: Bearer <sessionToken> when stored', async () => {
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_abc', expiresAt: null })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('Authorization')).toBe('Bearer ses_abc')
  })

  it('prefixes path with baseURL when set (remote relay)', async () => {
    saveRelayConfig({
      baseURL: 'https://r.example.com',
      sessionToken: 'ses_t',
      expiresAt: null,
    })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    expect(fetchMock.mock.calls[0]![0]).toBe('https://r.example.com/api/me')
  })

  it('trims trailing slash from baseURL when prefixing', async () => {
    saveRelayConfig({
      baseURL: 'https://r.example.com/',
      sessionToken: 'ses_t',
      expiresAt: null,
    })
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    expect(fetchMock.mock.calls[0]![0]).toBe('https://r.example.com/api/me')
  })

  it('uses same-origin (empty baseURL) when no config is stored', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    expect(fetchMock.mock.calls[0]![0]).toBe('/api/me')
  })
})

describe('apiFetch error handling', () => {
  beforeEach(() => {
    clearRelayConfig()
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
    clearRelayConfig()
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

  it('clears relay config (and Authorization on next call) on 401', async () => {
    saveRelayConfig({ baseURL: '', sessionToken: 'ses_stale', expiresAt: null })
    const { assign } = stubLocation('/settings.html')
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(makeResponse(401, { error: 'unauthenticated' }))
      .mockResolvedValueOnce(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiFetch('/api/me')).rejects.toBeInstanceOf(ApiError)
    expect(assign).toHaveBeenCalledTimes(1)

    await apiFetch('/api/me')
    const headers = new Headers((fetchMock.mock.calls[1]![1] as RequestInit).headers)
    expect(headers.has('Authorization')).toBe(false)
  })
})
