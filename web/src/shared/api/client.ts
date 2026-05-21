import { isMobileApp, loadRelayConfig } from './relay-config'

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

// CSRF cache: relay derives the token from the session secret and
// returns it in /api/me's body. Frontend caches it here and adds it
// to every non-GET request. See spec Sec-4.
let cachedCsrf = ''
export function setCsrfToken(token: string): void {
  cachedCsrf = token
}
export function clearCsrfToken(): void {
  cachedCsrf = ''
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

export async function apiFetch<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<ApiResult<T>> {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)

  let url = path
  let credentials: RequestCredentials = 'same-origin'

  if (isMobileApp()) {
    const cfg = loadRelayConfig()
    if (!cfg) throw new ApiError(0, 'relay_not_configured', null)
    url = cfg.base.replace(/\/$/, '') + path
    headers.set('Authorization', `Bearer ${cfg.token}`)
    credentials = 'omit'
    if (!headers.has('Content-Type') && init.body !== undefined && method !== 'GET' && method !== 'HEAD') {
      headers.set('Content-Type', 'application/json')
    }
  } else {
    if (method !== 'GET' && method !== 'HEAD') {
      if (cachedCsrf) headers.set('X-CSRF-Token', cachedCsrf)
      if (!headers.has('Content-Type') && init.body !== undefined) {
        headers.set('Content-Type', 'application/json')
      }
    }
  }

  let res: Response
  try {
    res = await fetch(url, { ...init, headers, credentials })
  } catch {
    throw new ApiError(0, 'network_error', null)
  }

  if (res.status === 401) {
    if (isMobileApp()) {
      if (typeof location !== 'undefined') {
        location.replace('/setup.html?reason=token_invalid')
      }
    } else {
      const onAuthPage =
        typeof location !== 'undefined' &&
        (location.pathname === '/login.html' || location.pathname === '/signup.html')
      if (!onAuthPage && typeof location !== 'undefined') {
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
