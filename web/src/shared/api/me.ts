import { apiFetch } from './client'
import type {
  ApiTokenCreated,
  ApiTokenRow,
  MeResponse,
  SessionRow,
  SignOutOthersResponse,
} from './types'

// getMe fetches the current user. The relay no longer issues CSRF
// tokens (session_token in Authorization: Bearer is sufficient
// proof-of-intent), so the historical csrf_token round-trip is gone.
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
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

// Account deletion (settings → Danger zone tab).
export async function deleteMe(email: string, password: string): Promise<void> {
  await apiFetch('/api/me', {
    method: 'DELETE',
    body: JSON.stringify({ email, password }),
  })
}
