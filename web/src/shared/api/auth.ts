import { apiFetch } from './client'
import { loadRelayConfig, saveRelayConfig, clearRelayConfig } from './relay-config'
import { saveAccountKey, clearAccountKey } from './account-key'
import {
  RegistrationResponse,
  KE2,
} from '@cloudflare/opaque-ts'
import {
  SERVER_IDENTITY,
  defaultKDFParams,
  getOpaqueConfig,
  newOpaqueClient,
  unwrapWithPassword,
  wrapAccountKey,
  type AccountKeyWrap,
} from '@shared/lib/opaque'

// AuthResult is what login() / register() now hand back to the UI.
// The legacy {session_token, expires_at, user} envelope is gone — both
// session_token persistence and account_key unlock happen here, so the
// caller only needs the user identity for greeting / routing.
export interface AuthResult {
  user_id: string
  email: string
  // is_admin is set by register() (the finalize response carries it) so the
  // first-run setup page can confirm the account got the admin role.
  is_admin?: boolean
}

// BootstrapStatus is the public first-run signal (GET /api/bootstrap/status).
// admin_exists=false means the relay has no admin yet → show first-run setup.
export interface BootstrapStatus {
  admin_exists: boolean
}

// getBootstrapStatus is unauthenticated; used by the login + first-run pages
// before any credentials exist.
export async function getBootstrapStatus(): Promise<BootstrapStatus> {
  const { data } = await apiFetch<BootstrapStatus>('/api/bootstrap/status')
  return data
}

interface RegisterInitResp {
  registration_response: string
}
interface RegisterFinalizeResp {
  user_id: string
  session_token: string
  is_admin: boolean
}
interface LoginInitResp {
  login_response: string
  session_id: string
}
interface LoginFinalizeResp {
  user_id: string
  session_token: string
  account_key_wrap: AccountKeyWrap
}

function bytesToB64Std(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function b64StdToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function numberArrayToB64(b: number[]): string {
  return bytesToB64Std(new Uint8Array(b))
}

function persistSession(sessionToken: string): void {
  const existing = loadRelayConfig()
  saveRelayConfig({
    baseURL: existing?.baseURL ?? '',
    allowInsecure: existing?.allowInsecure ?? false,
    sessionToken,
    // OPAQUE login no longer returns an expiry; relay-side authoritative.
    expiresAt: null,
  })
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const { data } = await apiFetch<T>(path, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return data
}

// login completes a full OPAQUE login round-trip against the relay,
// unlocks the account_key locally from the wrap blob returned by the
// server, and persists both the session token and the unlocked key for
// follow-on requests.
export async function login(email: string, password: string): Promise<AuthResult> {
  const cfg = getOpaqueConfig()
  const client = newOpaqueClient()

  const ke1OrErr = await client.authInit(password)
  if (ke1OrErr instanceof Error) throw ke1OrErr
  const ke1 = ke1OrErr

  const init = await postJSON<LoginInitResp>('/api/auth/login/init', {
    email,
    login_ke: numberArrayToB64(ke1.serialize()),
  })

  const ke2Bytes = Array.from(b64StdToBytes(init.login_response))
  const ke2 = KE2.deserialize(cfg, ke2Bytes)

  const finishOrErr = await client.authFinish(ke2, SERVER_IDENTITY, email)
  if (finishOrErr instanceof Error) {
    // The TS lib detects wrong-password during the KE2 MAC check —
    // surface a stable error string the UI can match against.
    throw new Error('invalid credentials')
  }
  const { ke3 } = finishOrErr

  const final = await postJSON<LoginFinalizeResp>('/api/auth/login/finalize', {
    email,
    session_id: init.session_id,
    login_ke3: numberArrayToB64(ke3.serialize()),
  })

  const accountKey = unwrapWithPassword(password, final.account_key_wrap)
  saveAccountKey(accountKey)
  persistSession(final.session_token)

  return { user_id: final.user_id, email }
}

// register completes a fresh OPAQUE registration: mints a new
// account_key client-side, wraps it under the password, uploads the
// envelope and registration record, persists session + key.
// claim_token is the bootstrap admin token printed by atterm-relay on
// first boot; supply it to claim the admin account, or pass "" to
// register a regular user.
export async function register(
  email: string,
  password: string,
  claim_token = '',
): Promise<AuthResult> {
  const cfg = getOpaqueConfig()
  const client = newOpaqueClient()

  const ke1OrErr = await client.registerInit(password)
  if (ke1OrErr instanceof Error) throw ke1OrErr
  const ke1 = ke1OrErr

  const init = await postJSON<RegisterInitResp>('/api/auth/register/init', {
    email,
    registration_ke: numberArrayToB64(ke1.serialize()),
  })

  const ke2Bytes = Array.from(b64StdToBytes(init.registration_response))
  const ke2 = RegistrationResponse.deserialize(cfg, ke2Bytes)

  const finishOrErr = await client.registerFinish(ke2, SERVER_IDENTITY, email)
  if (finishOrErr instanceof Error) throw finishOrErr
  const { record } = finishOrErr

  // Mint the account_key + wrap it.
  const accountKey = new Uint8Array(32)
  crypto.getRandomValues(accountKey)
  const wrap = wrapAccountKey(password, accountKey, defaultKDFParams())

  const finBody: Record<string, unknown> = {
    email,
    registration_record: numberArrayToB64(record.serialize()),
    account_key_wrap: wrap,
  }
  if (claim_token) finBody.claim_token = claim_token

  const final = await postJSON<RegisterFinalizeResp>(
    '/api/auth/register/finalize',
    finBody,
  )

  saveAccountKey(accountKey)
  persistSession(final.session_token)

  return { user_id: final.user_id, email, is_admin: final.is_admin }
}

// signup is kept as a thin alias for callers that still expect the
// "register a new user" entry point under the old name. The legacy
// invite_code parameter is gone — the relay's claim-token flow
// replaces it; new callers should use register() directly.
export async function signup(
  email: string,
  password: string,
  claim_token = '',
): Promise<AuthResult> {
  return register(email, password, claim_token)
}

// logout revokes the server-side session and clears the stored
// RelayConfig + account_key.
export async function logout(): Promise<void> {
  try {
    await apiFetch('/api/auth/logout', { method: 'POST' })
  } finally {
    // Even if the network call fails the local state should still be
    // wiped — leaving an account_key on disk after the user clicked
    // logout is a worse outcome than a stale server session.
    clearAccountKey()
    clearRelayConfig()
  }
}

