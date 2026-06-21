// AEAD account-key wrap + per-session content decrypt for the mobile/capacitor
// bundle. Mirrors web/src/shared/lib/opaque.ts: same AEAD construction
// (XChaCha20-Poly1305 with AAD "atterm-account-key-v1"), same Argon2id KDF,
// same session/meta seal codec. The OPAQUE protocol itself now runs in the
// bytemare WASM client (./opaqueWasm.ts), not here — see capacitor.ts.
//
// Why duplicate the web copy: the two bundles ship to different runtimes
// (Capacitor WKWebView vs browser tab) and currently have separate
// node_modules. Mechanically identical, divergence would silently break
// cross-device interop, so any change here MUST be mirrored in
// web/src/shared/lib/opaque.ts.

import { argon2id } from '@noble/hashes/argon2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'

export const SERVER_IDENTITY = 'atterm-relay'
const ACCOUNT_KEY_AAD = utf8ToBytes('atterm-account-key-v1')

export interface KDFParams {
  alg: 'argon2id'
  m: number
  t: number
  p: number
}

export function defaultKDFParams(): KDFParams {
  return { alg: 'argon2id', m: 64 * 1024, t: 3, p: 1 }
}

export interface AccountKeyWrap {
  method: string
  wrapped: string
  nonce: string
  salt: string
  kdf_params: string
}

function deriveWrapKey(password: string, salt: Uint8Array, kp: KDFParams): Uint8Array {
  return argon2id(utf8ToBytes(password), salt, {
    m: kp.m,
    t: kp.t,
    p: kp.p,
    dkLen: 32,
  })
}

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

function bytesToB64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function b64ToBytes(s: string): Uint8Array {
  const norm = s.replace(/-/g, '+').replace(/_/g, '/')
  const pad = norm.length % 4 === 0 ? '' : '='.repeat(4 - (norm.length % 4))
  const bin = atob(norm + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

// ---- M3b: per-session content decrypt ----
// Mirror of web/src/shared/lib/opaque.ts. Any change to this block MUST
// be mirrored in the web copy so a cross-device user's sealed
// envelopes interop between the iOS app and the browser tab.

const SESSION_INFO_AAD_FRAME_TYPE = 0x12
const SESSION_KEY_INFO_PREFIX = utf8ToBytes('atterm-session-v1')
const CIPHER_ID_XCHACHA20_POLY1305 = 0x01

export interface SealedSessionFields {
  title?: string
  cwd?: string
  command?: string
  current_command?: string
}

function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  if (hex.length !== 32) throw new Error(`invalid uuid: ${s}`)
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) {
    out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return out
}

function deriveSessionKey(accountKey: Uint8Array, sessionUUID: string): Uint8Array {
  const uuidBytes = uuidStringToBytes(sessionUUID)
  const info = new Uint8Array(SESSION_KEY_INFO_PREFIX.length + uuidBytes.length)
  info.set(SESSION_KEY_INFO_PREFIX, 0)
  info.set(uuidBytes, SESSION_KEY_INFO_PREFIX.length)
  return hkdf(sha256, accountKey, undefined, info, 32)
}

export function openSessionFields(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
): SealedSessionFields | null {
  return openSealedFields<SealedSessionFields>(sealed, accountKey, sessionUUID, SESSION_INFO_AAD_FRAME_TYPE)
}

/** SealedMetaFields mirrors the Go agent's sealedMetaFields struct in
 * desktop/uplink.go (M5-meta-seal). Live TypeMeta envelope carries
 * only the three fields the relay was able to peek at as a session
 * ran: cwd, title, current_command. */
export interface SealedMetaFields {
  cwd?: string
  title?: string
  current_command?: string
}

/** AAD frame_type discriminator for live TypeMeta sealed envelopes.
 * Distinct from SESSION_INFO_AAD_FRAME_TYPE so a META envelope cannot
 * be replayed as a SessionInfo blob and vice versa. */
const META_AAD_FRAME_TYPE = 0x05

/** openMetaFields decrypts a MetaPayload.Sealed envelope from a live
 * TypeMeta frame. */
export function openMetaFields(
  sealed: Uint8Array | number[] | undefined | null,
  accountKey: Uint8Array,
  sessionUUID: string,
): SealedMetaFields | null {
  return openSealedFields<SealedMetaFields>(sealed, accountKey, sessionUUID, META_AAD_FRAME_TYPE)
}

/** Shared low-level inverse of the agent's AEAD seal. The frameType
 * byte goes into the AAD; SessionInfo (0x12) and META (0x05) live on
 * the same wire codec but with distinct discriminators so an envelope
 * sealed for one cannot be replayed as the other. */
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
