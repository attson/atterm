// safeNext validates a post-login redirect target. The frontend never
// follows ?next= verbatim — every consumer routes through this guard.
// See spec Sec-2.
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

// apiFetch is the single network entry point for the browser client.
// PR-A only ships safeNext; the full implementation (401 redirect,
// CSRF header injection, ApiError) lands in PR-B alongside the first
// real consumer.
export async function apiFetch<T = unknown>(_path: string, _init?: RequestInit): Promise<{ data: T; status: number; headers: Headers }> {
  throw new Error('apiFetch not implemented yet; arrives in PR-B')
}
