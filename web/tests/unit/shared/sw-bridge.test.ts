// Unit tests for the main-thread side of the SW decrypt bridge
// (M6-sw). The bridge listens for navigator.serviceWorker `message`
// events and replies via the provided MessagePort. We exercise the
// pure decryptForSW() function plus the installSWBridge() integration
// against a minimal navigator.serviceWorker stub.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { utf8ToBytes, randomBytes } from '@noble/hashes/utils.js'
import { saveAccountKey, clearAccountKey } from '@shared/api/account-key'
import { decryptForSW, installSWBridge, SW_DECRYPT_REQUEST_TYPE } from '@shared/sw-bridge'
import type { SealedPushBody } from '@shared/lib/opaque'

const PUSH_BODY_AAD_FRAME_TYPE = 0x35
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

function bytesToB64(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function sealBody(
  accountKey: Uint8Array,
  uuid: string,
  fields: SealedPushBody,
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
  aad[uuidBytes.length] = PUSH_BODY_AAD_FRAME_TYPE

  const plaintext = new TextEncoder().encode(JSON.stringify(fields))
  const aead = xchacha20poly1305(sessionKey, nonce, aad)
  const ciphertext = aead.encrypt(plaintext)

  const env = new Uint8Array(1 + 24 + ciphertext.length)
  env[0] = CIPHER_ID
  env.set(nonce, 1)
  env.set(ciphertext, 1 + 24)
  return env
}

describe('sw-bridge decryptForSW', () => {
  const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
  const accountKey = new Uint8Array(32).map((_, i) => (i * 13) & 0xff)

  beforeEach(() => {
    clearAccountKey()
  })

  it('decrypts a valid sealedBody when account_key is unlocked', () => {
    saveAccountKey(accountKey)
    const env = sealBody(accountKey, uuid, { label: 'deploy', exit_code: 0, elapsed_ms: 4_000 })
    const reply = decryptForSW({
      type: SW_DECRYPT_REQUEST_TYPE,
      sessionId: uuid,
      sealedBody: bytesToB64(env),
    })
    expect(reply.fields).toEqual({ label: 'deploy', exit_code: 0, elapsed_ms: 4_000 })
  })

  it('returns null fields when no account_key is loaded', () => {
    const env = sealBody(accountKey, uuid, { label: 'x', exit_code: 0, elapsed_ms: 0 })
    const reply = decryptForSW({
      type: SW_DECRYPT_REQUEST_TYPE,
      sessionId: uuid,
      sealedBody: bytesToB64(env),
    })
    expect(reply.fields).toBeNull()
  })

  it('returns null fields when sealedBody is garbage base64', () => {
    saveAccountKey(accountKey)
    const reply = decryptForSW({
      type: SW_DECRYPT_REQUEST_TYPE,
      sessionId: uuid,
      sealedBody: '!!!!notbase64!!!!',
    })
    expect(reply.fields).toBeNull()
  })

  it('returns null fields when the session_id does not match the envelope', () => {
    saveAccountKey(accountKey)
    const env = sealBody(accountKey, uuid, { label: 'x', exit_code: 0, elapsed_ms: 0 })
    const otherUUID = 'b1b2c3d4-e5f6-7890-1234-567890abcdef'
    const reply = decryptForSW({
      type: SW_DECRYPT_REQUEST_TYPE,
      sessionId: otherUUID,
      sealedBody: bytesToB64(env),
    })
    expect(reply.fields).toBeNull()
  })
})

describe('sw-bridge installSWBridge', () => {
  beforeEach(() => {
    clearAccountKey()
  })

  it('replies to a SW message through the provided port', async () => {
    const listeners: Array<(e: MessageEvent) => void> = []
    const addEventListener = vi.fn((type: string, cb: (e: MessageEvent) => void) => {
      if (type === 'message') listeners.push(cb)
    })
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: { addEventListener },
    })
    const accountKey = new Uint8Array(32).map((_, i) => (i * 17) & 0xff)
    saveAccountKey(accountKey)
    const uuid = 'a1b2c3d4-e5f6-7890-1234-567890abcdef'
    const env = sealBody(accountKey, uuid, { label: 'lint', exit_code: 1, elapsed_ms: 500 })

    installSWBridge()
    expect(addEventListener).toHaveBeenCalledWith('message', expect.any(Function))

    const port = { postMessage: vi.fn() }
    const evt = {
      data: { type: SW_DECRYPT_REQUEST_TYPE, sessionId: uuid, sealedBody: bytesToB64(env) },
      ports: [port],
    } as unknown as MessageEvent

    for (const cb of listeners) cb(evt)
    expect(port.postMessage).toHaveBeenCalledTimes(1)
    expect(port.postMessage.mock.calls[0]![0]).toEqual({
      fields: { label: 'lint', exit_code: 1, elapsed_ms: 500 },
    })
  })

  it('ignores messages without the matching request type', () => {
    const listeners: Array<(e: MessageEvent) => void> = []
    const addEventListener = vi.fn((type: string, cb: (e: MessageEvent) => void) => {
      if (type === 'message') listeners.push(cb)
    })
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: { addEventListener },
    })

    installSWBridge()
    const port = { postMessage: vi.fn() }
    const evt = {
      data: { type: 'other-type', sessionId: 'x', sealedBody: 'y' },
      ports: [port],
    } as unknown as MessageEvent
    for (const cb of listeners) cb(evt)
    expect(port.postMessage).not.toHaveBeenCalled()
  })

  it('is a no-op when navigator.serviceWorker is missing', () => {
    delete (navigator as { serviceWorker?: unknown }).serviceWorker
    expect(() => installSWBridge()).not.toThrow()
  })
})
