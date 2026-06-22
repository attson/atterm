// E2EE OPAQUE client + AEAD account-key wrap for the atterm web client.
// Mirrors internal/e2eeclient (Go): same cipher suite (P-256-SHA256 +
// Scrypt), same identity bindings (ClientIdentity = email,
// ServerIdentity = "atterm-relay"), same AEAD construction
// (XChaCha20-Poly1305 with the AAD literal "atterm-account-key-v1").
//
// The relay's OPAQUE server in internal/relay/opaque_server.go is
// configured to match this suite — see opaqueSuiteTag = "p256-scrypt-v1".
// Changing either side without the other will break the protocol.

// NOTE: the OPAQUE protocol itself runs in the bytemare WASM client
// (@shared/lib/opaqueWasm). This module keeps only the account-key AEAD wrap
// (Argon2id + XChaCha20) and the per-session content decrypt — both noble,
// both byte-compatible with the Go agent — neither of which is part of OPAQUE.
import { argon2id } from '@noble/hashes/argon2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'

/** Server identity bound into the AKE transcript. Must match Go side. */
export const SERVER_IDENTITY = 'atterm-relay'

/** AAD bound into the account-key wrap envelope. Must match Go side. */
const ACCOUNT_KEY_AAD = utf8ToBytes('atterm-account-key-v1')

/** Argon2id parameters used to derive the wrap key from the user password.
 * Matches Go internal/e2eeclient DefaultKDFParams(). */
export interface KDFParams {
  alg: 'argon2id'
  /** memory in KiB */
  m: number
  /** iterations */
  t: number
  /** parallelism */
  p: number
}

/** Default KDF parameters: 64 MiB / 3 iterations / 1 thread. */
export function defaultKDFParams(): KDFParams {
  return { alg: 'argon2id', m: 64 * 1024, t: 3, p: 1 }
}

/** Wire-shape of an account_key wrap envelope. Mirrors the Go struct
 * exactly so the same blob round-trips both ways. */
export interface AccountKeyWrap {
  method: string
  /** base64url-encoded ciphertext (AEAD output, includes 16-byte tag) */
  wrapped: string
  /** base64url-encoded 24-byte nonce */
  nonce: string
  /** base64url-encoded 16-byte salt */
  salt: string
  /** JSON string of KDFParams */
  kdf_params: string
}

/** Derive a 32-byte wrap key from password + salt + Argon2id params. */
function deriveWrapKey(password: string, salt: Uint8Array, kp: KDFParams): Uint8Array {
  return argon2id(utf8ToBytes(password), salt, {
    m: kp.m,
    t: kp.t,
    p: kp.p,
    dkLen: 32,
  })
}

/** AEAD-seal the 32-byte account_key under password-derived wrap key. */
export function wrapAccountKey(
  password: string,
  accountKey: Uint8Array,
  kp: KDFParams = defaultKDFParams(),
): AccountKeyWrap {
  if (accountKey.length !== 32) {
    throw new Error(`accountKey must be 32 bytes, got ${accountKey.length}`)
  }
  const salt = randomBytes(16)
  const wrapKey = deriveWrapKey(password, salt, kp)
  const nonce = randomBytes(24)
  const aead = xchacha20poly1305(wrapKey, nonce, ACCOUNT_KEY_AAD)
  const ciphertext = aead.encrypt(accountKey)
  return {
    method: 'password',
    wrapped: bytesToB64(ciphertext),
    nonce: bytesToB64(nonce),
    salt: bytesToB64(salt),
    kdf_params: JSON.stringify(kp),
  }
}

/** AEAD-open a wrap envelope using password. Throws on tag mismatch
 * (wrong password). */
export function unwrapWithPassword(password: string, wrap: AccountKeyWrap): Uint8Array {
  const kp = JSON.parse(wrap.kdf_params) as KDFParams
  if (kp.alg !== 'argon2id') {
    throw new Error(`unsupported kdf alg: ${kp.alg}`)
  }
  const salt = b64ToBytes(wrap.salt)
  const wrapKey = deriveWrapKey(password, salt, kp)
  const nonce = b64ToBytes(wrap.nonce)
  const aead = xchacha20poly1305(wrapKey, nonce, ACCOUNT_KEY_AAD)
  try {
    return aead.decrypt(b64ToBytes(wrap.wrapped))
  } catch {
    throw new Error('invalid password')
  }
}

/** Helpers — standard base64 WITH padding. This is what Go's
 * encoding/json picks when serializing a struct field typed []byte,
 * so the wrap envelope round-trips byte-for-byte through the Go
 * relay's accountKeyWrapPayload. URL-safe / no-padding variants
 * decode cleanly too thanks to the explicit char remap below. */
function bytesToB64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function b64ToBytes(s: string): Uint8Array {
  // Accept either std or URL-safe encodings, with or without padding,
  // so a Go server that someday switches encodings cannot silently
  // break unwrap.
  const norm = s.replace(/-/g, '+').replace(/_/g, '/')
  const pad = norm.length % 4 === 0 ? '' : '='.repeat(4 - (norm.length % 4))
  const bin = atob(norm + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

// ---- M3b: per-session content decrypt ----
//
// The Go agent encrypts {title, cwd, command, current_command} into
// SessionInfo.Sealed via:
//   session_key = HKDF-SHA256(account_key, info = "atterm-session-v1" || uuid_bytes)
//   envelope    = cipher_id(0x01) || nonce(24) || XChaCha20-Poly1305.encrypt(
//                    key = session_key,
//                    nonce,
//                    plaintext = JSON({title, cwd, command, current_command}),
//                    aad = uuid(16) || 0x12 /* TypeListResp */,
//                 )
// openSessionFields below is the matching client-side inverse.

const SESSION_INFO_AAD_FRAME_TYPE = 0x12
const SESSION_KEY_INFO_PREFIX = utf8ToBytes('atterm-session-v1')
const CIPHER_ID_XCHACHA20_POLY1305 = 0x01

/** Sealed JSON shape — keep in sync with desktop/uplink_seal_fields.go's
 * sealedSessionFields struct. */
export interface SealedSessionFields {
  title?: string
  cwd?: string
  command?: string
  current_command?: string
}

/** uuidStringToBytes parses a canonical UUID string into 16 raw bytes.
 * The Go side mixes these into the HKDF info and AEAD AAD, so a mismatch
 * here breaks decryption silently. */
function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  if (hex.length !== 32) throw new Error(`invalid uuid: ${s}`)
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) {
    out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return out
}

/** Derive the per-session AEAD key from the user's unlocked account_key
 * and a session uuid. Mirrors internal/e2eecrypto/sessionkey.go. */
function deriveSessionKey(accountKey: Uint8Array, sessionUUID: string): Uint8Array {
  const uuidBytes = uuidStringToBytes(sessionUUID)
  const info = new Uint8Array(SESSION_KEY_INFO_PREFIX.length + uuidBytes.length)
  info.set(SESSION_KEY_INFO_PREFIX, 0)
  info.set(uuidBytes, SESSION_KEY_INFO_PREFIX.length)
  return hkdf(sha256, accountKey, undefined, info, 32)
}

/** Open a SessionInfo.Sealed envelope and parse the JSON inside.
 * Returns null on any cipher / parse error; the caller falls back to the
 * plaintext fields on the outer SessionInfo. */
/** SealedMetaFields mirrors the Go agent's sealedMetaFields struct in
 * desktop/uplink.go. The live-META envelope (M5-meta-seal, v0.2.104)
 * carries only the three fields the relay was structurally able to
 * peek at as a session ran: cwd, title, current_command. */
export interface SealedMetaFields {
  cwd?: string
  title?: string
  current_command?: string
}

/** AAD frame_type discriminator for live TypeMeta sealed envelopes.
 * Distinct from SESSION_INFO_AAD_FRAME_TYPE so a META envelope cannot
 * be replayed as a SessionInfo blob and vice versa. */
const META_AAD_FRAME_TYPE = 0x05

/** openMetaFields decrypts a MetaPayload.Sealed envelope (M5-meta-seal).
 * Returns null on any cipher / parse error; the caller falls back to
 * the (now-empty) plaintext fields on the outer MetaPayload. */
export function openMetaFields(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
): SealedMetaFields | null {
  return openSealedFields<SealedMetaFields>(sealed, accountKey, sessionUUID, META_AAD_FRAME_TYPE)
}

export function openSessionFields(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
): SealedSessionFields | null {
  return openSealedFields<SealedSessionFields>(sealed, accountKey, sessionUUID, SESSION_INFO_AAD_FRAME_TYPE)
}

// ---- M2-web: live TypeOut stream decrypt ----
//
// The Go agent seals every TypeOut chunk (desktop/uplink.go sealOutFrame →
// e2eecrypto.SealOut). Unlike the META/SessionInfo envelopes, an OUT frame's
// AAD binds the monotonic seq, so a chunk cannot be replayed at a different
// position: aad = uuid(16) || frame_type(0x03 = TypeOut) || seq(8B BE).
// The envelope wire shape is the same: cipher_id(0x01) || nonce(24) ||
// XChaCha20-Poly1305 ciphertext+tag. openOutFrame is the matching inverse and
// returns the RAW plaintext bytes (no JSON parse — this is terminal output).

/** AAD frame_type discriminator for live TypeOut stream envelopes. Matches
 * proto.TypeOut (0x03) on the wire; distinct from the META (0x05) /
 * SessionInfo (0x12) discriminators so a stream chunk cannot be replayed as
 * a metadata blob and vice versa. */
const OUT_AAD_FRAME_TYPE = 0x03

/** openOutFrame decrypts a sealed TypeOut envelope for sequence `seq`.
 * Returns null on any structural / cipher error (truncated, wrong
 * cipher_id, wrong key/uuid/seq) so the caller can drop the chunk rather
 * than render ciphertext. */
export function openOutFrame(
  envelope: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
  seq: number,
): Uint8Array | null {
  if (!envelope) return null
  const env = envelope instanceof Uint8Array ? envelope : new Uint8Array(envelope)
  const minEnvelopeLen = 1 + 24 + 16
  if (env.length < minEnvelopeLen) return null
  if (env[0] !== CIPHER_ID_XCHACHA20_POLY1305) return null

  let sk: Uint8Array
  let uuidBytes: Uint8Array
  try {
    sk = deriveSessionKey(accountKey, sessionUUID)
    uuidBytes = uuidStringToBytes(sessionUUID)
  } catch {
    return null
  }

  const nonce = env.subarray(1, 1 + 24)
  const ciphertext = env.subarray(1 + 24)

  const aad = new Uint8Array(uuidBytes.length + 1 + 8)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = OUT_AAD_FRAME_TYPE
  // seq is a JS number (PTY chunk counter, always < 2^53); write it as an
  // 8-byte big-endian uint64 to match Go's binary.BigEndian.PutUint64.
  new DataView(aad.buffer, aad.byteOffset, aad.byteLength).setBigUint64(
    uuidBytes.length + 1,
    BigInt(seq),
    false,
  )

  try {
    return xchacha20poly1305(sk, nonce, aad).decrypt(ciphertext)
  } catch {
    return null
  }
}

/** SealedPushBody mirrors the Go agent's sealedPushBody struct in
 * desktop/uplink_seal_push.go. The CommandEvent envelope (M6-foundation,
 * v0.2.108) carries the three fields the service worker needs to
 * render a rich "command finished" notification without leaking any
 * of them to the relay. */
export interface SealedPushBody {
  label?: string
  exit_code: number
  elapsed_ms: number
}

/** AAD frame_type discriminator for sealed push body envelopes. Distinct
 * from SESSION_INFO_AAD_FRAME_TYPE (0x12) and META_AAD_FRAME_TYPE
 * (0x05) so a stolen envelope cannot be replayed across types.
 * 0x35 matches proto.TypeCommandEvent on the wire. */
const PUSH_BODY_AAD_FRAME_TYPE = 0x35

/** openPushBodyFields decrypts a CommandEvent SealedBody envelope
 * (M6-foundation). Returns null on any cipher / parse error; the SW
 * caller falls back to the legacy plaintext title/body strings. */
export function openPushBodyFields(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
): SealedPushBody | null {
  return openSealedFields<SealedPushBody>(sealed, accountKey, sessionUUID, PUSH_BODY_AAD_FRAME_TYPE)
}

/** openSealedFields is the shared low-level inverse of the agent's
 * AEAD seal. The frameType byte goes into the AAD; SESSION_INFO and
 * META live on the same wire codec but with distinct discriminators so
 * an envelope sealed for one cannot be replayed as the other. */
function openSealedFields<T>(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
  frameType: number,
): T | null {
  if (!sealed) return null
  const envelope = sealed instanceof Uint8Array ? sealed : new Uint8Array(sealed)
  const minEnvelopeLen = 1 + 24 + 16
  if (envelope.length < minEnvelopeLen) return null
  if (envelope[0] !== CIPHER_ID_XCHACHA20_POLY1305) return null

  let sk: Uint8Array
  try {
    sk = deriveSessionKey(accountKey, sessionUUID)
  } catch {
    return null
  }

  const nonce = envelope.subarray(1, 1 + 24)
  const ciphertext = envelope.subarray(1 + 24)

  const uuidBytes = uuidStringToBytes(sessionUUID)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = frameType

  const aead = xchacha20poly1305(sk, nonce, aad)
  let plaintext: Uint8Array
  try {
    plaintext = aead.decrypt(ciphertext)
  } catch {
    return null
  }
  try {
    return JSON.parse(new TextDecoder().decode(plaintext)) as T
  } catch {
    return null
  }
}
