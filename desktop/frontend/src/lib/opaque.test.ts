import { describe, it, expect } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import {
  openAccountKeyWrap,
  openMetaFields,
  openOutFrame,
  openSessionFields,
  type SealedMetaFields,
  type SealedSessionFields,
} from './opaque'

const AAD_FRAME_TYPE = 0x12
const META_AAD_FRAME_TYPE = 0x05
const OUT_AAD_FRAME_TYPE = 0x03
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

function sealMetaWithFrameType(
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
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32)
  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = frameType
  const aead = xchacha20poly1305(sessionKey, nonce, aad)
  const ct = aead.encrypt(new TextEncoder().encode(JSON.stringify(fields)))
  const env = new Uint8Array(1 + 24 + ct.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ct, 1 + 24)
  return env
}

describe('openMetaFields (M5-meta-mobile)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 13) & 0xff)

  it('recovers cwd / title / current_command', () => {
    const fields: SealedMetaFields = {
      cwd: '/Users/alice/secrets',
      title: 'atterm - bash',
      current_command: 'rg api_key',
    }
    const env = sealMetaWithFrameType(accountKey, uuid, META_AAD_FRAME_TYPE, fields)
    expect(openMetaFields(env, accountKey, uuid)).toEqual(fields)
  })

  it('rejects a SessionInfo-AAD envelope (cross-replay guard)', () => {
    const env = sealMetaWithFrameType(accountKey, uuid, AAD_FRAME_TYPE, { title: 'x' })
    expect(openMetaFields(env, accountKey, uuid)).toBeNull()
    // Same envelope opens under openSessionFields.
    expect(openSessionFields(env, accountKey, uuid)).toEqual({ title: 'x' })
  })
})

// sealOut mirrors internal/e2eecrypto.SealOut + the desktop agent's
// sealOutFrame: AAD = uuid(16) || 0x03 || seq(8B BE), raw plaintext payload.
function sealOut(accountKey: Uint8Array, uuid: string, seq: number, plaintext: Uint8Array): Uint8Array {
  const uuidBytes = uuidStringToBytes(uuid)
  const prefix = utf8ToBytes('atterm-session-v1')
  const info = new Uint8Array(prefix.length + uuidBytes.length)
  info.set(prefix, 0)
  info.set(uuidBytes, prefix.length)
  const sessionKey = hkdf(sha256, accountKey, undefined, info, 32)
  const nonce = randomBytes(24)
  const aad = new Uint8Array(uuidBytes.length + 1 + 8)
  aad.set(uuidBytes, 0)
  aad[uuidBytes.length] = OUT_AAD_FRAME_TYPE
  new DataView(aad.buffer).setBigUint64(uuidBytes.length + 1, BigInt(seq), false)
  const ct = xchacha20poly1305(sessionKey, nonce, aad).encrypt(plaintext)
  const env = new Uint8Array(1 + 24 + ct.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ct, 1 + 24)
  return env
}

describe('openOutFrame (remote TypeOut stream decrypt)', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 17) & 0xff)

  it('round-trips a sealed OUT chunk to raw plaintext', () => {
    const pt = new TextEncoder().encode('$ ls -la\r\ntotal 0\r\n')
    const out = openOutFrame(sealOut(accountKey, uuid, 42, pt), accountKey, uuid, 42)
    // Compare as plain arrays: noble returns a view whose backing buffer differs,
    // which vitest 1.6's typed-array toEqual treats as unequal despite identical bytes.
    expect(out && Array.from(out)).toEqual(Array.from(pt))
  })

  it('returns null when seq / uuid / key differ, or input is non-envelope', () => {
    const env = sealOut(accountKey, uuid, 7, new Uint8Array([1, 2, 3]))
    expect(openOutFrame(env, accountKey, uuid, 8)).toBeNull() // seq bound into AAD
    expect(openOutFrame(env, accountKey, 'b1b2c3d4-e5f6-7890-1234-567890abcdef', 7)).toBeNull()
    expect(openOutFrame(env, new Uint8Array(32), uuid, 7)).toBeNull()
    expect(openOutFrame(new Uint8Array(40), accountKey, uuid, 7)).toBeNull() // too short
    expect(openOutFrame(null, accountKey, uuid, 7)).toBeNull()
  })
})

function hexToBytes(h: string): Uint8Array {
  const out = new Uint8Array(h.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.substring(i * 2, i * 2 + 2), 16)
  return out
}

describe('openAccountKeyWrap', () => {
  // Values captured from desktop/wrap_account_key_test.go
  // TestWrapAccountKey_GoldenForTS — see that test's t.Logf output.
  const AK = new Uint8Array(32).fill(0x42)
  const WK = new Uint8Array(32).fill(0x99)
  const ENV = hexToBytes(
    '0177777777777777777777777777777777777777777777777749f5e18042a088e014b6e94085cec3f0423cde0e15ba982caca14bef556aa6fd8f2e225a7ecb16e6f9e12754da515726',
  )

  it('opens a Go-sealed envelope', () => {
    const got = openAccountKeyWrap(ENV, WK)
    expect(got).not.toBeNull()
    expect(Array.from(got!)).toEqual(Array.from(AK))
  })

  it('returns null on wrong wrap key', () => {
    const bad = new Uint8Array(32).fill(0xaa)
    expect(openAccountKeyWrap(ENV, bad)).toBeNull()
  })

  it('returns null on wrong cipher_id', () => {
    const tampered = new Uint8Array(ENV)
    tampered[0] = 0x02
    expect(openAccountKeyWrap(tampered, WK)).toBeNull()
  })

  it('returns null on truncated envelope', () => {
    expect(openAccountKeyWrap(ENV.subarray(0, 20), WK)).toBeNull()
  })

  it('returns null on missing envelope', () => {
    expect(openAccountKeyWrap(null, WK)).toBeNull()
    expect(openAccountKeyWrap(undefined, WK)).toBeNull()
  })
})
