// Cross-language test: encrypt session fields in TS (mirroring the Go
// agent), then decrypt with openSessionFields and assert byte-for-byte
// recovery. Complements the agent-side unit tests in
// desktop/uplink_seal_fields_test.go.

import { describe, it, expect } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { openSessionFields, type SealedSessionFields } from '@shared/lib/opaque'

const SESSION_INFO_AAD_FRAME_TYPE = 0x12
const CIPHER_ID = 0x01

function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  if (hex.length !== 32) throw new Error('bad uuid')
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) {
    out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return out
}

function sealFields(
  accountKey: Uint8Array,
  uuid: string,
  fields: SealedSessionFields,
): Uint8Array {
  const uuidBytes = uuidStringToBytes(uuid)
  const info = new Uint8Array(utf8ToBytes('atterm-session-v1').length + uuidBytes.length)
  info.set(utf8ToBytes('atterm-session-v1'), 0)
  info.set(uuidBytes, utf8ToBytes('atterm-session-v1').length)
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32)

  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = SESSION_INFO_AAD_FRAME_TYPE

  const plaintext = new TextEncoder().encode(JSON.stringify(fields))
  const aead = xchacha20poly1305(sessionKey, nonce, aad)
  const ciphertext = aead.encrypt(plaintext)

  const envelope = new Uint8Array(1 + 24 + ciphertext.length)
  envelope[0] = CIPHER_ID
  envelope.set(nonce, 1)
  envelope.set(ciphertext, 1 + 24)
  return envelope
}

describe('openSessionFields (M3b-web)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 7) & 0xff)

  it('recovers all four content fields after sealing', () => {
    const original: SealedSessionFields = {
      title: 'atterm - bash',
      cwd: '/Users/alice/secrets',
      command: 'bash',
      current_command: 'rg api_key',
    }
    const env = sealFields(accountKey, uuid, original)
    const got = openSessionFields(env, accountKey, uuid)
    expect(got).toEqual(original)
  })

  it('returns null when the cipher_id byte is wrong', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    env[0] = 0xff
    expect(openSessionFields(env, accountKey, uuid)).toBeNull()
  })

  it('returns null when the envelope is shorter than the minimum', () => {
    expect(openSessionFields(new Uint8Array(40), accountKey, uuid)).toBeNull()
  })

  it('returns null when the session uuid does not match', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    const otherUUID = 'b1b2c3d4-e5f6-7890-1234-567890abcdef'
    expect(openSessionFields(env, accountKey, otherUUID)).toBeNull()
  })

  it('returns null when the account_key does not match', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    const otherKey = new Uint8Array(32).map((_, i) => i)
    expect(openSessionFields(env, otherKey, uuid)).toBeNull()
  })

  it('accepts the sealed bytes as a number[] (Vue / fetch JSON path)', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    expect(openSessionFields(Array.from(env), accountKey, uuid)).toEqual({ title: 'x' })
  })

  it('returns null for null / undefined input', () => {
    expect(openSessionFields(null, accountKey, uuid)).toBeNull()
    expect(openSessionFields(undefined, accountKey, uuid)).toBeNull()
  })
})
