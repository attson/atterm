import { apiFetch } from './client'
import { loadAccountKey } from './account-key'
import { openSessionFields } from '@shared/lib/opaque'
import type { SessionInfo } from './types'

// listSessions returns the active PTY sessions visible to the caller.
// The relay scopes results by owner when a userstore is wired; otherwise
// (legacy/test setups) it returns every live session. Public read-only
// endpoint — no CSRF required, only the cookie session.
//
// When the local account_key is unlocked and a session carries an M3b
// sealed envelope (title/cwd/command/current_command encrypted under
// the per-session HKDF key), this wrapper overlays the decrypted
// values on top of the (currently still populated, additive-rollout)
// plaintext fields. Decryption failures are silent — the relay-side
// plaintext fields remain visible to the caller.
export async function listSessions(): Promise<SessionInfo[]> {
  const { data } = await apiFetch<SessionInfo[]>('/api/sessions')
  return decryptSessionInfoBatch(data)
}

// decryptSessionInfoBatch is exported for tests. When the local
// account_key is missing or all sessions are plaintext, this is a
// no-op.
export function decryptSessionInfoBatch(sessions: SessionInfo[]): SessionInfo[] {
  const accountKey = loadAccountKey()
  if (!accountKey) return sessions
  return sessions.map((s) => decryptSessionInfo(s, accountKey))
}

function decryptSessionInfo(s: SessionInfo, accountKey: Uint8Array): SessionInfo {
  if (!s.sealed) return s
  let envelope: Uint8Array
  try {
    envelope = base64ToBytes(s.sealed)
  } catch {
    return s
  }
  const fields = openSessionFields(envelope, accountKey, s.id)
  if (!fields) return s
  const out: SessionInfo = { ...s }
  if (fields.title !== undefined) out.title = fields.title
  if (fields.cwd !== undefined) out.cwd = fields.cwd
  if (fields.command !== undefined) out.command = fields.command
  if (fields.current_command !== undefined) out.current_command = fields.current_command
  return out
}

function base64ToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}
