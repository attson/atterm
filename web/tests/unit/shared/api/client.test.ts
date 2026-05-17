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
import {
  ApiError,
  apiFetch,
  clearCsrfToken,
  setCsrfToken,
} from '@shared/api/client'

function makeResponse(status: number, body: unknown, contentType = 'application/json'): Response {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(text, { status, headers: { 'Content-Type': contentType } })
}

describe('apiFetch CSRF cache', () => {
  beforeEach(() => {
    clearCsrfToken()
    vi.restoreAllMocks()
  })

  it('omits X-CSRF-Token on GET requests even when cached', async () => {
    setCsrfToken('cached-secret')
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me')

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('injects X-CSRF-Token on POST requests when cache is populated', async () => {
    setCsrfToken('cached-secret')
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/me/password', { method: 'POST', body: JSON.stringify({ old: 'x', new: 'y' }) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('X-CSRF-Token')).toBe('cached-secret')
  })

  it('does not inject X-CSRF-Token when cache is empty (login flow)', async () => {
    clearCsrfToken()
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u', email: 'e' }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({ email: 'e', password: 'p' }) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('sets JSON Content-Type when body present and not pre-set', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { ok: true }))
    vi.stubGlobal('fetch', fetchMock)

    await apiFetch('/api/auth/login', { method: 'POST', body: JSON.stringify({}) })

    const [, init] = fetchMock.mock.calls[0]!
    const headers = new Headers((init as RequestInit).headers)
    expect(headers.get('Content-Type')).toBe('application/json')
  })
})

describe('apiFetch error handling', () => {
  beforeEach(() => {
    clearCsrfToken()
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
    clearCsrfToken()
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
})
