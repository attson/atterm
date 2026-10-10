import { describe, expect, it } from 'vitest'
import directVector from '../../../../../internal/peertransport/testdata/direct_crypto_v1.json'
import peerVector from '../../../../../internal/peertransport/testdata/peer_auth_v1.json'
import { deriveP256SharedSecret, marshalDirectTranscript } from '../directCrypto'
import { buildPeerFinishProof, derivePeerBinding, derivePeerTrafficKeys, peerPermission } from './handshake'

function fromHex(value: string): Uint8Array {
  return Uint8Array.from({ length: value.length / 2 }, (_, index) => Number.parseInt(value.slice(index * 2, index * 2 + 2), 16))
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
  const publicKey = fromHex(publicHex)
  return {
    kty: 'EC', crv: 'P-256', d: base64URL(fromHex(privateHex)),
    x: base64URL(publicKey.slice(1, 33)), y: base64URL(publicKey.slice(33)), ext: true,
  }
}

describe('Peer membership handshake interoperability', () => {
  it('matches Go binding, finish proof, and traffic keys', async () => {
    const transcript = fromHex(peerVector.transcript_hex)
    const binding = derivePeerBinding(
      peerVector.genesis_hash,
      peerVector.client_membership,
      peerVector.host_membership,
      transcript,
    )
    expect(toHex(binding)).toBe(peerVector.binding_hex)
    expect(toHex(buildPeerFinishProof(binding, transcript))).toBe(peerVector.finish_proof_hex)

    const shared = await deriveP256SharedSecret(
      privateJWK(peerVector.client_private_key_hex, directVector.client_public_key_hex),
      fromHex(directVector.host_public_key_hex),
    )
    const keys = derivePeerTrafficKeys(shared, binding, transcript)
    expect(toHex(keys.clientToHostKey)).toBe(peerVector.client_to_host_key_hex)
    expect(toHex(keys.hostToClientKey)).toBe(peerVector.host_to_client_key_hex)
    expect(toHex(keys.clientToHostNoncePrefix)).toBe(peerVector.client_nonce_prefix_hex)
    expect(toHex(keys.hostToClientNoncePrefix)).toBe(peerVector.host_nonce_prefix_hex)
  })

  it('accepts Go numeric permissions and rejects unknown values', () => {
    expect(peerPermission(1)).toBe(1)
    expect(peerPermission('control')).toBe(2)
    expect(peerPermission(3)).toBe(3)
    expect(() => peerPermission(4)).toThrow('invalid permission')
  })
})
