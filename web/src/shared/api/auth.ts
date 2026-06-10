import { apiFetch } from './client'
import { loadRelayConfig, saveRelayConfig, clearRelayConfig } from './relay-config'
import type { LoginResp } from './types'

// login submits credentials and returns the auth payload. Task 4.2
// adds session_token persistence: parses the new {session_token, expires_at, user}
// shape and persists sessionToken + expiresAt into localStorage via RelayConfig,
// preserving the baseURL and any legacy fields.
export async function login(email: string, password: string): Promise<LoginResp> {
  const { data } = await apiFetch<LoginResp>('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })

  // Persist session_token and expires_at to localStorage. Preserve
  // baseURL/allowInsecure if a prior RelayConfig exists (mobile remote
  // pairing), otherwise default to same-origin.
  const existing = loadRelayConfig()
  saveRelayConfig({
    baseURL: existing?.baseURL ?? '',
    allowInsecure: existing?.allowInsecure ?? false,
    sessionToken: data.session_token,
    expiresAt: data.expires_at,
  })

  return data
}

// signup mirrors login but adds the required invite_code field.
// It also persists the session_token and expires_at returned by the backend.
export async function signup(
  email: string,
  password: string,
  invite_code: string,
): Promise<LoginResp> {
  const { data } = await apiFetch<LoginResp>('/api/auth/signup', {
    method: 'POST',
    body: JSON.stringify({ email, password, invite_code }),
  })

  // Persist session_token and expires_at to localStorage. Preserve
  // baseURL/allowInsecure if a prior RelayConfig exists (mobile remote
  // pairing), otherwise default to same-origin.
  const existing = loadRelayConfig()
  saveRelayConfig({
    baseURL: existing?.baseURL ?? '',
    allowInsecure: existing?.allowInsecure ?? false,
    sessionToken: data.session_token,
    expiresAt: data.expires_at,
  })

  return data
}

// logout revokes the server-side session and clears the stored RelayConfig.
export async function logout(): Promise<void> {
  await apiFetch('/api/auth/logout', { method: 'POST' })
  clearRelayConfig()
}
