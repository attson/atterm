import { describe, it, expect } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { openSessionFields, type SealedSessionFields } from './opaque'

const AAD_FRAME_TYPE = 0x12
const CIPHER_ID = 0x01

function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
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
  const prefix = utf8ToBytes('atterm-session-v1')
  const info = new Uint8Array(prefix.length + uuidBytes.length)
  info.set(prefix, 0)
  info.set(uuidBytes, prefix.length)
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32)
  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = AAD_FRAME_TYPE
  const aead = xchacha20poly1305(sessionKey, nonce, aad)
  const ct = aead.encrypt(new TextEncoder().encode(JSON.stringify(fields)))
  const env = new Uint8Array(1 + 24 + ct.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ct, 1 + 24)
  return env
}

describe('openSessionFields (M3b-mobile)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 11) & 0xff)

  it('recovers all four content fields after sealing', () => {
    const original: SealedSessionFields = {
      title: 'atterm - bash',
      cwd: '/Users/alice/secrets',
      command: 'bash',
      current_command: 'rg api_key',
    }
    const env = sealFields(accountKey, uuid, original)
    expect(openSessionFields(env, accountKey, uuid)).toEqual(original)
  })

  it('returns null on wrong cipher_id, truncated input, wrong uuid, wrong key, null input', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    const bad = new Uint8Array(env)
    bad[0] = 0xff
    expect(openSessionFields(bad, accountKey, uuid)).toBeNull()
    expect(openSessionFields(new Uint8Array(40), accountKey, uuid)).toBeNull()
    expect(openSessionFields(env, accountKey, 'b1b2c3d4-e5f6-7890-1234-567890abcdef')).toBeNull()
    const otherKey = new Uint8Array(32).map((_, i) => i)
    expect(openSessionFields(env, otherKey, uuid)).toBeNull()
    expect(openSessionFields(null, accountKey, uuid)).toBeNull()
    expect(openSessionFields(undefined, accountKey, uuid)).toBeNull()
  })

  it('accepts number[] (JSON-parsed) input', () => {
    const env = sealFields(accountKey, uuid, { title: 'x' })
    expect(openSessionFields(Array.from(env), accountKey, uuid)).toEqual({ title: 'x' })
  })
})
