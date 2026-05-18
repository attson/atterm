import { apiFetch, clearCsrfToken, setCsrfToken } from './client'
import type {
  ApiTokenCreated,
  ApiTokenRow,
  MeResponse,
  SessionRow,
  SignOutOthersResponse,
} from './types'

// getMe fetches the current user and refreshes the CSRF cache when the
// server returns a token (always present for cookie-authenticated calls).
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  if (data.csrf_token) setCsrfToken(data.csrf_token)
  return data
}

// API token helpers (settings → API Tokens tab).
export async function listTokens(): Promise<ApiTokenRow[]> {
  const { data } = await apiFetch<ApiTokenRow[]>('/api/me/tokens')
  return data
}

export async function createToken(name: string): Promise<ApiTokenCreated> {
  const { data } = await apiFetch<ApiTokenCreated>('/api/me/tokens', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
  return data
}

export async function revokeToken(id: string): Promise<void> {
  await apiFetch(`/api/me/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// Web session helpers (settings → Signed-in devices tab).
export async function listSessions(): Promise<SessionRow[]> {
  const { data } = await apiFetch<SessionRow[]>('/api/me/sessions')
  return data
}

export async function revokeSession(idHash: string): Promise<void> {
  await apiFetch(`/api/me/sessions/${encodeURIComponent(idHash)}`, { method: 'DELETE' })
}

export async function signOutOthers(): Promise<SignOutOthersResponse> {
  const { data } = await apiFetch<SignOutOthersResponse>(
    '/api/me/sessions/sign-out-others',
    { method: 'POST' },
  )
  return data
}

// Password (settings → Change Password tab).
export async function changePassword(
  current_password: string,
  new_password: string,
): Promise<void> {
  await apiFetch('/api/me/password', {
    method: 'POST',
    body: JSON.stringify({ current_password, new_password }),
  })
}

// Account deletion (settings → Danger zone tab). Server clears the
// session cookie; we also drop the local CSRF cache because the
// account no longer exists to derive a new token.
export async function deleteMe(email: string, password: string): Promise<void> {
  await apiFetch('/api/me', {
    method: 'DELETE',
    body: JSON.stringify({ email, password }),
  })
  clearCsrfToken()
}
