// Mobile-side readers for the two E2EE-sealed prefs-sync blobs the desktop
// already produces: `profiles_encrypted` (desktop/profiles.go sealProfiles)
// and `ssh_hosts_encrypted` (desktop/ssh_sync.go sealSSHHosts). Direction is
// one-way — mobile only reads these keys, never writes them (design §2/§6).
//
// The crypto is not new: `openUnsequencedFrame` in ./opaque.ts is the exact
// inverse of e2eecrypto.SealUnsequenced, which both sealProfiles and
// sealSSHHosts call. What each reader adds is only the envelope shape:
//
//   value = base64( SealUnsequenced(
//             DeriveSessionKey(accountKey, <fixed session UUID>),
//             <fixed session UUID>, <AAD tag>, json(payload)) )
//
// `value` here is the plain base64 string as it actually arrives at the
// call site — appConfigAdapter.ReadValue (desktop/prefssync_adapter.go)
// returns sealProfiles/sealSSHHosts's json.RawMessage (a JSON string token)
// embedded verbatim as a ServerItem's `value` field, so after the relay
// round trip and one `res.json()` on the client, the caller already holds
// a plain string — no extra JSON.parse of `value` itself.
//
// Golden vectors: the fixed session UUIDs and AAD tags below, the fixed
// account key, and the two sealed values in
// desktop/testdata/synced_blob_vectors.json are all produced by
// desktop/synced_blob_vectors_test.go (TestGenerateSyncedBlobVectors), never
// hand-authored. desktop/frontend/src/lib/syncedBlobs.test.ts opens that
// fixture — see the file's own header for why this is the load-bearing
// half of this feature.

import { openUnsequencedFrame } from './opaque'

// Must match profilesSyncSessionID (desktop/profiles.go).
const PROFILES_SYNC_SESSION_ID = 'bbbac178-5e9b-45b4-8b79-3257d9af7ca5'
// Must match sshHostsSyncSessionID (desktop/ssh_sync.go).
const SSH_HOSTS_SYNC_SESSION_ID = '55480000-0000-4000-8000-000000000001'

// Must match e2eecrypto.AADTagProfiles / AADTagSSHHosts (internal/e2eecrypto/aadtags.go).
const AAD_TAG_PROFILES = 0xf1
const AAD_TAG_SSH_HOSTS = 0xf0

/** Mobile-facing view of one SessionProfile (desktop/profiles.go). Field
 *  names are the TS convention (camelCase), not the wire's snake_case.
 *
 *  `syncEnv` is the signal a caller MUST check before reading `env`:
 *  stripUnsyncedEnv (desktop/profiles.go) clears Env before sealing for
 *  every profile with SyncEnv == false, so `env` is always undefined for
 *  those — "not synced from that machine" — which is a different fact from
 *  syncEnv == true with an empty/absent env — "this profile has no env
 *  vars set". Collapsing the two into one optional field would make mobile
 *  unable to tell them apart. */
export interface ProfileView {
  id: string
  name: string
  shell: string
  cwd: string
  startupCmd: string
  syncEnv: boolean
  env?: Record<string, string>
}

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

interface RawSessionProfile {
  id: string
  name: string
  shell?: string
  cwd?: string
  startup_cmd?: string
  env?: Record<string, string>
  sync_env?: boolean
}

interface RawProfilesSyncPayload {
  profiles?: RawSessionProfile[]
  default_profile_id?: string
}

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

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

/** Shared open: base64-decode `value`, open the envelope for `sessionUUID`
 *  / `aadTag`, JSON.parse the plaintext. Throws on any failure — a wrong
 *  account key or a corrupted/foreign blob must fail loudly rather than
 *  hand the caller garbage it might render. */
function openBlob(accountKey: Uint8Array, value: string, sessionUUID: string, aadTag: number, what: string): unknown {
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
  try {
    return JSON.parse(new TextDecoder().decode(plaintext))
  } catch {
    throw new Error(`${what}: decrypted payload is not valid JSON`)
  }
}

/** Opens a `profiles_encrypted` prefs-sync value into the profile list and
 *  the default-profile selection that traveled with it. Inverse of
 *  desktop/profiles.go's sealProfiles/openProfiles. Throws on a wrong
 *  account key or a malformed/foreign blob. */
export function openProfilesBlob(
  accountKey: Uint8Array,
  value: string,
): { profiles: ProfileView[]; defaultProfileId: string } {
  const payload = openBlob(
    accountKey,
    value,
    PROFILES_SYNC_SESSION_ID,
    AAD_TAG_PROFILES,
    'openProfilesBlob',
  ) as RawProfilesSyncPayload
  const profiles = (payload.profiles ?? []).map(
    (p): ProfileView => ({
      id: p.id,
      name: p.name,
      shell: p.shell ?? '',
      cwd: p.cwd ?? '',
      startupCmd: p.startup_cmd ?? '',
      syncEnv: !!p.sync_env,
      env: p.env,
    }),
  )
  return { profiles, defaultProfileId: payload.default_profile_id ?? '' }
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
