import { apiFetch } from './client'
import type { AuthSuccess } from './types'

// login submits credentials and returns the auth payload. Task 4.2
// adds session_token persistence; for now this just preserves the
// existing contract minus the CSRF refresh round-trip (CSRF is gone
// from the relay).
export async function login(email: string, password: string): Promise<AuthSuccess> {
  const { data } = await apiFetch<AuthSuccess>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
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
  return data
}

// logout revokes the server-side session.
export async function logout(): Promise<void> {
  await apiFetch('/api/auth/logout', { method: 'POST' })
}
