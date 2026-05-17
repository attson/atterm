import { apiFetch, clearCsrfToken } from './client'
import { getMe } from './me'
import type { AuthSuccess } from './types'

// login submits credentials, then calls getMe to populate the CSRF
// cache before any subsequent mutating request fires.
export async function login(email: string, password: string): Promise<AuthSuccess> {
  const { data } = await apiFetch<AuthSuccess>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
  await getMe()
  return data
}

// signup mirrors login but adds the required invite_code field.
export async function signup(
  email: string,
  password: string,
  invite_code: string,
): Promise<AuthSuccess> {
  const { data } = await apiFetch<AuthSuccess>('/api/auth/signup', {
    method: 'POST',
    body: JSON.stringify({ email, password, invite_code }),
  })
  await getMe()
  return data
}

// logout invalidates the server session and drops the local CSRF cache.
export async function logout(): Promise<void> {
  await apiFetch('/api/auth/logout', { method: 'POST' })
  clearCsrfToken()
}
