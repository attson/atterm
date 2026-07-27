import { apiFetch } from './client'
import type { MeResponse } from './types'

// getMe fetches the current user. The relay no longer issues CSRF
// tokens (session_token in Authorization: Bearer is sufficient
// proof-of-intent), so the historical csrf_token round-trip is gone.
export async function getMe(): Promise<MeResponse> {
  const { data } = await apiFetch<MeResponse>('/api/me')
  return data
}
