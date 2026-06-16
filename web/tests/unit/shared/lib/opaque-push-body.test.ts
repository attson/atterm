// Cross-language test: seal a SealedPushBody envelope in TS (mirroring
// the Go agent's sealCommandEventBody in desktop/uplink_seal_push.go),
// then decrypt with openPushBodyFields and assert byte-for-byte
// recovery. Also asserts that an envelope sealed with the SessionInfo
// or META AAD discriminator MUST NOT open under the push-body AAD —
// cross-type replay guard.

import { describe, it, expect } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { openPushBodyFields, type SealedPushBody } from '@shared/lib/opaque'

const PUSH_BODY_AAD_FRAME_TYPE = 0x35
const SESSION_INFO_AAD_FRAME_TYPE = 0x12
const META_AAD_FRAME_TYPE = 0x05
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

function sealBody(
  accountKey: Uint8Array,
  uuid: string,
  fields: SealedPushBody,
  aadFrameType: number,
): Uint8Array {
  const uuidBytes = uuidStringToBytes(uuid)
  const infoPrefix = utf8ToBytes('atterm-session-v1')
  const info = new Uint8Array(infoPrefix.length + uuidBytes.length)
  info.set(infoPrefix, 0)
  info.set(uuidBytes, infoPrefix.length)
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32)

  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = aadFrameType

  const plaintext = new TextEncoder().encode(JSON.stringify(fields))
  const aead = xchacha20poly1305(sessionKey, nonce, aad)
  const ciphertext = aead.encrypt(plaintext)

  const envelope = new Uint8Array(1 + 24 + ciphertext.length)
  envelope[0] = CIPHER_ID
  envelope.set(nonce, 1)
  envelope.set(ciphertext, 1 + 24)
  return envelope
}

describe('openPushBodyFields (M6-sw)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 11) & 0xff)

  it('recovers label / exit_code / elapsed_ms after sealing', () => {
    const original: SealedPushBody = {
      label: 'build',
      exit_code: 0,
      elapsed_ms: 12_345,
    }
    const env = sealBody(accountKey, uuid, original, PUSH_BODY_AAD_FRAME_TYPE)
    const got = openPushBodyFields(env, accountKey, uuid)
    expect(got).toEqual(original)
  })

  it('returns null when the envelope was sealed under the SessionInfo AAD', () => {
    const env = sealBody(
      accountKey,
      uuid,
      { label: 'x', exit_code: 0, elapsed_ms: 1 },
      SESSION_INFO_AAD_FRAME_TYPE,
    )
    expect(openPushBodyFields(env, accountKey, uuid)).toBeNull()
  })

  it('returns null when the envelope was sealed under the META AAD', () => {
    const env = sealBody(
      accountKey,
      uuid,
      { label: 'x', exit_code: 0, elapsed_ms: 1 },
      META_AAD_FRAME_TYPE,
    )
    expect(openPushBodyFields(env, accountKey, uuid)).toBeNull()
  })

  it('returns null when the session uuid does not match', () => {
    const env = sealBody(accountKey, uuid, { exit_code: 0, elapsed_ms: 0 }, PUSH_BODY_AAD_FRAME_TYPE)
    const other = 'b1b2c3d4-e5f6-7890-1234-567890abcdef'
    expect(openPushBodyFields(env, accountKey, other)).toBeNull()
  })

  it('returns null when the account_key does not match', () => {
    const env = sealBody(accountKey, uuid, { exit_code: 0, elapsed_ms: 0 }, PUSH_BODY_AAD_FRAME_TYPE)
    const otherKey = new Uint8Array(32).map((_, i) => i)
    expect(openPushBodyFields(env, otherKey, uuid)).toBeNull()
  })

  it('returns null for null / undefined input', () => {
    expect(openPushBodyFields(null, accountKey, uuid)).toBeNull()
    expect(openPushBodyFields(undefined, accountKey, uuid)).toBeNull()
  })
})
