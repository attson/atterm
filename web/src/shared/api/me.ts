import { apiFetch, setCsrfToken } from './client'
import type { MeResponse } from './types'

// getMe fetches the current user and refreshes the CSRF cache when the
// server returns a token (always present for cookie-authenticated calls).
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  if (data.csrf_token) setCsrfToken(data.csrf_token)
  return data
}
