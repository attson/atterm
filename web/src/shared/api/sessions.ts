import { apiFetch } from './client'
import type { SessionInfo } from './types'

// listSessions returns the active PTY sessions visible to the caller.
// The relay scopes results by owner when a userstore is wired; otherwise
// (legacy/test setups) it returns every live session. Public read-only
// endpoint — no CSRF required, only the cookie session.
export async function listSessions(): Promise<SessionInfo[]> {
  const { data } = await apiFetch<SessionInfo[]>('/api/sessions')
  return data
}
