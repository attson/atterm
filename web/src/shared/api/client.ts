import { clearRelayConfig, loadRelayConfig } from './relay-config'

// safeNext validates a post-login redirect target. Every consumer of
// the ?next= query param routes through this guard. See spec Sec-2.
export function safeNext(raw: string | null): string {
  if (!raw) return '/'
  if (!raw.startsWith('/')) return '/'
  if (raw.startsWith('//')) return '/'
  if (raw.startsWith('/\\')) return '/'
  if (typeof location === 'undefined') return '/'
  try {
    const u = new URL(raw, location.origin)
    if (u.origin !== location.origin) return '/'
    return u.pathname + u.search + u.hash
  } catch {
    return '/'
  }
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    public readonly response: Response | null,
  ) {
    super(`api error ${status} ${code}`)
    this.name = 'ApiError'
  }
}

export interface ApiResult<T> {
  data: T
  status: number
  headers: Headers
}

// apiFetch — single code path for every HTTP request issued by the web
// client. Phase 4 of the session-token migration removed the cookie /
// CSRF / mobile-vs-web split: the client now always carries the
// session_token as Authorization: Bearer, never sends cookies, and on
// 401 wipes localStorage and bounces to /login.html.
//
// baseURL on the stored RelayConfig is honoured when present so the
// same code works from a remote-paired mobile app talking to a relay on
// a different origin. Web users keep baseURL empty (same-origin).
export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<ApiResult<T>> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  const cfg = loadRelayConfig()

  if (cfg?.sessionToken) {
    headers.set('Authorization', `Bearer ${cfg.sessionToken}`)
  }
  if (!headers.has('Content-Type') && init.body !== undefined && method !== 'GET' && method !== 'HEAD') {
    headers.set('Content-Type', 'application/json')
  }

  const base = (cfg?.baseURL ?? '').replace(/\/$/, '')
  const url = base + path

  let res: Response
  try {
    res = await fetch(url, { ...init, headers, credentials: 'omit' })
  } catch {
    throw new ApiError(0, 'network_error', null)
  }

  if (res.status === 401) {
    clearRelayConfig()
    if (typeof location !== 'undefined') {
      const onAuthPage =
        location.pathname === '/login.html' || location.pathname === '/signup.html'
      if (!onAuthPage) {
        const next = safeNext(location.pathname + location.search + location.hash)
        location.assign('/login.html?next=' + encodeURIComponent(next))
      }
    }
  }

  if (!res.ok) {
    let code = 'http_error'
    const ct = res.headers.get('Content-Type') || ''
    if (ct.includes('application/json')) {
      try {
        const j = (await res.clone().json()) as { error?: unknown }
        if (j && typeof j.error === 'string') code = j.error
      } catch {
        /* malformed JSON, keep http_error */
      }
    }
    throw new ApiError(res.status, code, res)
  }

  const ct = res.headers.get('Content-Type') || ''
  let data: T
  if (ct.includes('application/json')) {
    data = (await res.json()) as T
  } else {
    data = undefined as unknown as T
  }
  return { data, status: res.status, headers: res.headers }
}
