// E2EE OPAQUE client + AEAD account-key wrap for the mobile/capacitor
// bundle. Mirrors web/src/shared/lib/opaque.ts and internal/e2eeclient
// (Go): same P-256-SHA256 + Scrypt OPAQUE suite, same identity bindings
// (ClientIdentity = email, ServerIdentity = "atterm-relay"), same AEAD
// construction (XChaCha20-Poly1305 with AAD "atterm-account-key-v1").
//
// Why duplicate the web copy: the two bundles ship to different runtimes
// (Capacitor WKWebView vs browser tab) and currently have separate
// node_modules. Mechanically identical, divergence would silently break
// cross-device interop, so any change here MUST be mirrored in
// web/src/shared/lib/opaque.ts.

import {
  OpaqueClient,
  OpaqueID,
  ScryptMemHardFn,
  getOpaqueConfig as cfGetOpaqueConfig,
  type Config,
  KE2,
  RegistrationResponse,
} from '@cloudflare/opaque-ts'
import { argon2id } from '@noble/hashes/argon2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
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

function getConfig(): Config {
  return cfGetOpaqueConfig(OpaqueID.OPAQUE_P256)
}

export function newOpaqueClient(): OpaqueClient {
  return new OpaqueClient(getConfig(), ScryptMemHardFn)
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

export { KE2, RegistrationResponse, getConfig as getOpaqueConfig }
