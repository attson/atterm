// Unit test for openOutFrame — the web-side inverse of the desktop agent's
// sealOutFrame (desktop/uplink.go) / e2eecrypto.SealOut (internal/e2eecrypto).
//
// We construct a sealed TypeOut envelope here with the SAME primitives the Go
// agent uses (HKDF-SHA256 session key + XChaCha20-Poly1305 with AAD
// uuid(16) || 0x03 || seq_be64) so the test is self-contained — no live relay
// needed — and proves the web client can decrypt what the desktop seals.
import { describe, it, expect } from 'vitest'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes } from '@noble/hashes/utils.js'
import { openOutFrame } from '@shared/lib/opaque'

const CIPHER_ID = 0x01
const TYPE_OUT = 0x03
const SESSION_KEY_INFO_PREFIX = utf8ToBytes('atterm-session-v1')

function uuidToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  return out
}

function deriveSessionKey(accountKey: Uint8Array, uuid: string): Uint8Array {
  const u = uuidToBytes(uuid)
  const info = new Uint8Array(SESSION_KEY_INFO_PREFIX.length + u.length)
  info.set(SESSION_KEY_INFO_PREFIX, 0)
  info.set(u, SESSION_KEY_INFO_PREFIX.length)
  return hkdf(sha256, accountKey, undefined, info, 32)
}

function seqBE64(seq: number): Uint8Array {
  const b = new Uint8Array(8)
  new DataView(b.buffer).setBigUint64(0, BigInt(seq), false)
  return b
}

// sealOut mirrors internal/e2eecrypto.SealOut + the desktop sealOutFrame.
function sealOut(accountKey: Uint8Array, uuid: string, seq: number, plaintext: Uint8Array): Uint8Array {
  const sk = deriveSessionKey(accountKey, uuid)
  const nonce = new Uint8Array(24)
  crypto.getRandomValues(nonce)
  const aad = new Uint8Array(16 + 1 + 8)
  aad.set(uuidToBytes(uuid), 0)
  aad[16] = TYPE_OUT
  aad.set(seqBE64(seq), 17)
  const ciphertext = xchacha20poly1305(sk, nonce, aad).encrypt(plaintext)
  const env = new Uint8Array(1 + nonce.length + ciphertext.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ciphertext, 1 + nonce.length)
  return env
}

describe('openOutFrame', () => {
  const accountKey = new Uint8Array(32).fill(7)
  const uuid = '2baf7dc3-d900-48fc-a8c8-124eb9ba4fe9'

  it('round-trips a sealed OUT frame back to plaintext', () => {
    const pt = new TextEncoder().encode('hello world\r\n$ ls -la\r\n')
    const env = sealOut(accountKey, uuid, 42, pt)
    expect(openOutFrame(env, accountKey, uuid, 42)).toEqual(pt)
  })

  it('returns null when the seq bound into the AAD differs', () => {
    const env = sealOut(accountKey, uuid, 42, new Uint8Array([1, 2, 3]))
    expect(openOutFrame(env, accountKey, uuid, 43)).toBeNull()
  })

  it('returns null when the session uuid differs', () => {
    const env = sealOut(accountKey, uuid, 7, new Uint8Array([1, 2, 3]))
    expect(openOutFrame(env, accountKey, 'ffffffff-0000-0000-0000-000000000000', 7)).toBeNull()
  })

  it('returns null when the account key differs', () => {
    const env = sealOut(accountKey, uuid, 1, new Uint8Array([1, 2, 3]))
    expect(openOutFrame(env, new Uint8Array(32).fill(9), uuid, 1)).toBeNull()
  })

  it('returns null on a non-envelope cipher_id or a too-short payload', () => {
    expect(openOutFrame(new Uint8Array([0x00, 1, 2, 3]), accountKey, uuid, 1)).toBeNull()
    expect(openOutFrame(new Uint8Array(0), accountKey, uuid, 1)).toBeNull()
    expect(openOutFrame(new Uint8Array([CIPHER_ID, 1, 2]), accountKey, uuid, 1)).toBeNull()
  })
})
