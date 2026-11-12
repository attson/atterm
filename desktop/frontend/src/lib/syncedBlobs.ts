// Mobile reader for the desktop-sealed SSH hosts prefs-sync blob.
// Golden vectors in syncedBlobs.test.ts pin the Go/TypeScript envelope contract.

import { openUnsequencedFrame, b64ToBytes } from './opaque'

// Must match sshHostsSyncSessionID (desktop/ssh_sync.go).
const SSH_HOSTS_SYNC_SESSION_ID = '55480000-0000-4000-8000-000000000001'

// Must match e2eecrypto.AADTagSSHHosts (internal/e2eecrypto/aadtags.go).
const AAD_TAG_SSH_HOSTS = 0xf0

/** Mobile-facing view of one SSHHost (desktop/ssh_hosts_store.go). Never
 *  carries a credential field — see the module doc and
 *  syncedBlobs.test.ts's credential-leak assertion. */
export interface SSHHostView {
  id: string
  alias: string
  host: string
  port: string
  user: string
  authKind: string
  keyId: string
  tags: string[]
  note: string
  /** ProxyJump is non-empty: reaching this host requires dialing a chain. */
  hasJumpChain: boolean
  /** ProxyCommand is non-empty: nothing dials this host directly. */
  isProxyCommandHost: boolean
}

/** Mobile-facing view of one SSHKey (desktop/ssh_keys_store.go). Never
 *  carries the private key or passphrase — those live in sshKeySecret on
 *  the wire and are never read here. */
export interface SSHKeyView {
  id: string
  name: string
  keyType: string
}

// ---- wire shapes (snake_case, exactly the Go JSON tags) ----

interface RawSSHHost {
  id: string
  alias?: string
  host: string
  port?: string
  user: string
  auth_kind: string
  key_id?: string
  tags?: string[]
  note?: string
  proxy_jump?: string
  proxy_command?: string
}

interface RawSSHKey {
  id: string
  name: string
  key_type?: string
}

// sshSyncHost/sshSyncKey (desktop/ssh_sync.go) also carry `cred` /
// `private_key` / `passphrase` alongside `host` / `key`. Those fields are
// deliberately absent from the two interfaces below: the mapping code only
// ever destructures `host` / `key` off the raw entry, so there is nothing
// here that could accidentally forward a credential into a view.
interface RawSSHSyncHostEntry {
  host: RawSSHHost
}
interface RawSSHSyncKeyEntry {
  key: RawSSHKey
}

interface RawSSHSyncPayload {
  hosts?: RawSSHSyncHostEntry[]
  keys?: RawSSHSyncKeyEntry[]
}

/** Shared open: base64-decode `value`, open the envelope for `sessionUUID`
 *  / `aadTag`, JSON.parse the plaintext. Throws on any failure — a wrong
 *  account key or a corrupted/foreign blob must fail loudly rather than
 *  hand the caller garbage it might render or a bare TypeError from the
 *  first place a caller happens to dereference a property. */
function openBlob(accountKey: Uint8Array, value: string, sessionUUID: string, aadTag: number, what: string): Record<string, unknown> {
  let ciphertext: Uint8Array
  try {
    ciphertext = b64ToBytes(value)
  } catch {
    throw new Error(`${what}: value is not valid base64`)
  }
  const plaintext = openUnsequencedFrame(accountKey, sessionUUID, aadTag, ciphertext)
  if (!plaintext) {
    throw new Error(`${what}: could not decrypt (wrong account key or corrupted blob)`)
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(new TextDecoder().decode(plaintext))
  } catch {
    throw new Error(`${what}: decrypted payload is not valid JSON`)
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error(`${what}: decrypted payload is not a JSON object`)
  }
  return parsed as Record<string, unknown>
}

/** Opens an `ssh_hosts_encrypted` prefs-sync value into the host list and
 *  key list, with every credential/secret field dropped on the way out.
 *  Inverse of desktop/ssh_sync.go's sealSSHHosts/openSSHHosts (minus the
 *  credential maps, which this never surfaces). Throws on a wrong account
 *  key or a malformed/foreign blob. */
export function openSSHHostsBlob(
  accountKey: Uint8Array,
  value: string,
): { hosts: SSHHostView[]; keys: SSHKeyView[] } {
  const payload = openBlob(
    accountKey,
    value,
    SSH_HOSTS_SYNC_SESSION_ID,
    AAD_TAG_SSH_HOSTS,
    'openSSHHostsBlob',
  ) as RawSSHSyncPayload
  const hosts = (payload.hosts ?? []).map(
    ({ host: h }): SSHHostView => ({
      id: h.id,
      alias: h.alias ?? '',
      host: h.host,
      port: h.port ?? '',
      user: h.user,
      authKind: h.auth_kind,
      keyId: h.key_id ?? '',
      tags: h.tags ?? [],
      note: h.note ?? '',
      hasJumpChain: !!h.proxy_jump,
      isProxyCommandHost: !!h.proxy_command,
    }),
  )
  const keys = (payload.keys ?? []).map(
    ({ key: k }): SSHKeyView => ({
      id: k.id,
      name: k.name,
      keyType: k.key_type ?? '',
    }),
  )
  return { hosts, keys }
}
