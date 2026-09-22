import { describe, expect, it } from 'vitest'
import { sha256 } from '@noble/hashes/sha2.js'
import vector from '../../../../internal/peertransport/testdata/direct_crypto_v1.json'
import {
  buildDirectFinishProof,
  buildDirectProof,
  deriveDirectTrafficKeys,
  deriveP256SharedSecret,
  DirectPermission,
  DirectRecordKind,
  DirectRole,
  marshalDirectTranscript,
  openDirectRecord,
  sealDirectRecord,
} from './directCrypto'

function fromHex(value: string): Uint8Array {
  if (value.length % 2 !== 0) throw new Error('odd hex input')
  const out = new Uint8Array(value.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(value.slice(i * 2, i * 2 + 2), 16)
  return out
}

function toHex(value: Uint8Array): string {
  return Array.from(value, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

function base64URL(value: Uint8Array): string {
  let binary = ''
  for (const byte of value) binary += String.fromCharCode(byte)
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
}

function privateJWK(privateHex: string, publicHex: string): JsonWebKey {
  const privateKey = fromHex(privateHex)
  const publicKey = fromHex(publicHex)
  return {
    kty: 'EC',
    crv: 'P-256',
    d: base64URL(privateKey),
    x: base64URL(publicKey.slice(1, 33)),
    y: base64URL(publicKey.slice(33, 65)),
    ext: true,
  }
}

function transcript() {
  return marshalDirectTranscript({
    attemptId: vector.attempt_id,
    ticket: fromHex(vector.ticket_hex),
    sessionId: vector.session_id,
    userId: vector.user_id,
    hostId: vector.host_id,
    clientInstanceId: vector.client_instance_id,
    permission: vector.permission as DirectPermission,
    expiresAtUnixMs: BigInt(vector.expires_at_unix_ms),
    clientPublicKey: fromHex(vector.client_public_key_hex),
    hostPublicKey: fromHex(vector.host_public_key_hex),
  })
}

describe('Relay direct crypto interoperability', () => {
  it('matches the Go transcript and account-key proofs', () => {
    const encoded = transcript()
    const accountKey = fromHex(vector.account_key_hex)
    expect(toHex(encoded)).toBe(vector.transcript_hex)
    expect(toHex(buildDirectProof(accountKey, encoded, DirectRole.Client))).toBe(vector.client_proof_hex)
    expect(toHex(buildDirectProof(accountKey, encoded, DirectRole.Host))).toBe(vector.host_proof_hex)
    expect(toHex(buildDirectFinishProof(accountKey, encoded))).toBe(vector.finish_proof_hex)
  })

  it('matches Go P-256 ECDH and directional HKDF outputs', async () => {
    const encoded = transcript()
    const accountKey = fromHex(vector.account_key_hex)
    const clientShared = await deriveP256SharedSecret(
      privateJWK(vector.client_private_key_hex, vector.client_public_key_hex),
      fromHex(vector.host_public_key_hex),
    )
    const hostShared = await deriveP256SharedSecret(
      privateJWK(vector.host_private_key_hex, vector.host_public_key_hex),
      fromHex(vector.client_public_key_hex),
    )
    expect(toHex(clientShared)).toBe(toHex(hostShared))

    const keys = deriveDirectTrafficKeys(accountKey, clientShared, encoded)
    expect(toHex(keys.clientToHostKey)).toBe(vector.client_to_host_key_hex)
    expect(toHex(keys.hostToClientKey)).toBe(vector.host_to_client_key_hex)
    expect(toHex(keys.clientToHostNoncePrefix)).toBe(vector.client_nonce_prefix_hex)
    expect(toHex(keys.hostToClientNoncePrefix)).toBe(vector.host_nonce_prefix_hex)
  })

  it('seals and opens the Go record vector', () => {
    const encoded = transcript()
    const transcriptHash = sha256(encoded)
    const key = fromHex(vector.client_to_host_key_hex)
    const prefix = fromHex(vector.client_nonce_prefix_hex)
    const plaintext = fromHex(vector.record_plaintext_hex)
    const record = sealDirectRecord(key, prefix, transcriptHash, DirectRecordKind.Frame, 0n, plaintext)
    expect(toHex(record)).toBe(vector.record_hex)

    const opened = openDirectRecord(key, prefix, transcriptHash, 0n, fromHex(vector.record_hex))
    expect(opened.kind).toBe(DirectRecordKind.Frame)
    expect(toHex(opened.plaintext)).toBe(vector.record_plaintext_hex)
    expect(() => openDirectRecord(key, prefix, transcriptHash, 1n, fromHex(vector.record_hex)))
      .toThrow('invalid direct record sequence')
  })

  it('fails closed for a changed account key or transcript', () => {
    const encoded = transcript()
    const wrongKey = new Uint8Array(fromHex(vector.account_key_hex))
    wrongKey[0] ^= 1
    expect(toHex(buildDirectProof(wrongKey, encoded, DirectRole.Client))).not.toBe(vector.client_proof_hex)

    const changed = new Uint8Array(encoded)
    changed[changed.length - 1] ^= 1
    expect(toHex(buildDirectProof(fromHex(vector.account_key_hex), changed, DirectRole.Client)))
      .not.toBe(vector.client_proof_hex)
  })
})
