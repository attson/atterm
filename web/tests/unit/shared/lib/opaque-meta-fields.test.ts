// Cross-language test for the M5-meta-web decrypt path. Seals in TS
// using the same construction the Go agent uses for MetaPayload.Sealed
// (frame_type discriminator = TypeMeta 0x05), then opens with
// openMetaFields and asserts byte-for-byte recovery.

import { describe, it, expect } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { openMetaFields, openSessionFields, type SealedMetaFields } from '@shared/lib/opaque'

const AAD_META = 0x05
const AAD_SESSION_INFO = 0x12
const CIPHER_ID = 0x01

function uuidStringToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) {
    out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return out
}

function sealWithFrameType(
  accountKey: Uint8Array,
  uuid: string,
  frameType: number,
  fields: SealedMetaFields,
): Uint8Array {
  const uuidBytes = uuidStringToBytes(uuid)
  const prefix = utf8ToBytes('atterm-session-v1')
  const info = new Uint8Array(prefix.length + uuidBytes.length)
  info.set(prefix, 0)
  info.set(uuidBytes, prefix.length)
  const sk = hkdf(sha256, accountKey, undefined, info, 32)
  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = frameType
  const aead = xchacha20poly1305(sk, nonce, aad)
  const ct = aead.encrypt(new TextEncoder().encode(JSON.stringify(fields)))
  const env = new Uint8Array(1 + 24 + ct.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ct, 1 + 24)
  return env
}

describe('openMetaFields (M5-meta-web)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 17) & 0xff)

  it('recovers cwd / title / current_command after sealing', () => {
    const fields: SealedMetaFields = {
      cwd: '/Users/alice/secrets',
      title: 'atterm - bash',
      current_command: 'rg api_key',
    }
    const env = sealWithFrameType(accountKey, uuid, AAD_META, fields)
    expect(openMetaFields(env, accountKey, uuid)).toEqual(fields)
  })

  it('rejects an envelope sealed under the SessionInfo frame_type', () => {
    // Cross-replay guard: a SessionInfo.Sealed blob (frame_type 0x12)
    // must not open under openMetaFields. This is the load-bearing
    // separation between the two envelope namespaces.
    const env = sealWithFrameType(accountKey, uuid, AAD_SESSION_INFO, { title: 'x' })
    expect(openMetaFields(env, accountKey, uuid)).toBeNull()
    // Sanity check: the same envelope opens under openSessionFields.
    expect(openSessionFields(env, accountKey, uuid)).toEqual({ title: 'x' })
  })

  it('returns null on wrong cipher_id, truncated, wrong uuid, wrong key, null input', () => {
    const env = sealWithFrameType(accountKey, uuid, AAD_META, { title: 'x' })
    const bad = new Uint8Array(env)
    bad[0] = 0xff
    expect(openMetaFields(bad, accountKey, uuid)).toBeNull()
    expect(openMetaFields(new Uint8Array(40), accountKey, uuid)).toBeNull()
    expect(openMetaFields(env, accountKey, 'b1b2c3d4-e5f6-7890-1234-567890abcdef')).toBeNull()
    const otherKey = new Uint8Array(32).map((_, i) => i)
    expect(openMetaFields(env, otherKey, uuid)).toBeNull()
    expect(openMetaFields(null, accountKey, uuid)).toBeNull()
    expect(openMetaFields(undefined, accountKey, uuid)).toBeNull()
  })

  it('accepts number[] input (Vue / JSON-decoded shape)', () => {
    const env = sealWithFrameType(accountKey, uuid, AAD_META, { title: 'x' })
    expect(openMetaFields(Array.from(env), accountKey, uuid)).toEqual({ title: 'x' })
  })
})
