// E2EE OPAQUE client + AEAD account-key wrap for the atterm web client.
// Mirrors internal/e2eeclient (Go): same cipher suite (P-256-SHA256 +
// Scrypt), same identity bindings (ClientIdentity = email,
// ServerIdentity = "atterm-relay"), same AEAD construction
// (XChaCha20-Poly1305 with the AAD literal "atterm-account-key-v1").
//
// The relay's OPAQUE server in internal/relay/opaque_server.go is
// configured to match this suite — see opaqueSuiteTag = "p256-scrypt-v1".
// Changing either side without the other will break the protocol.

import {
  OpaqueClient,
  OpaqueID,
  ScryptMemHardFn,
  getOpaqueConfig,
  type Config,
  KE2,
  RegistrationResponse,
} from '@cloudflare/opaque-ts'
import { argon2id } from '@noble/hashes/argon2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
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

function getConfig(): Config {
  return getOpaqueConfig(OpaqueID.OPAQUE_P256)
}

/** New OPAQUE client bound to the relay's cipher suite + Scrypt MHF. */
export function newOpaqueClient(): OpaqueClient {
  return new OpaqueClient(getConfig(), ScryptMemHardFn)
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

/** Helpers — base64url with no padding so we round-trip with Go's
 * encoding/base64.RawURLEncoding (used by the relay's wire encoding). */
function bytesToB64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}

function b64ToBytes(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? '' : '='.repeat(4 - (s.length % 4))
  const norm = s.replace(/-/g, '+').replace(/_/g, '/') + pad
  const bin = atob(norm)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

/** Re-export message types so callers can deserialize relay responses. */
export { KE2, RegistrationResponse, getConfig as getOpaqueConfig }
