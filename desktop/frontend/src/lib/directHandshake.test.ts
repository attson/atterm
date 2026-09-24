import { describe, expect, it } from 'vitest'
import { sha256 } from '@noble/hashes/sha2.js'
import {
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
import {
  DIRECT_CLIENT_FINISH_SIZE,
  DIRECT_CLIENT_HELLO_SIZE,
  DirectClientHandshake,
  DirectClientRecordCodec,
  type DirectAuthorization,
} from './directHandshake'

function join(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let offset = 0
  for (const part of parts) {
    out.set(part, offset)
    offset += part.length
  }
  return out
}

function sameBytes(left: Uint8Array, right: Uint8Array): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index])
}

async function keyPair(): Promise<{ publicKey: Uint8Array; privateJWK: JsonWebKey }> {
  const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']) as CryptoKeyPair
  return {
    publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
    privateJWK: await crypto.subtle.exportKey('jwk', pair.privateKey),
  }
}

describe('browser direct client handshake', () => {
  it('completes the corrected four-message handshake with matching directional keys', async () => {
    const accountKey = new Uint8Array(32).fill(0x62)
    const authorization: DirectAuthorization = {
      attemptId: '11111111-2222-4333-8444-555555555555',
      ticket: new Uint8Array(32).fill(0x31),
      sessionId: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
      userId: 'user-1',
      hostId: 'host-1',
      clientInstanceId: 'client-1',
      permission: DirectPermission.Full,
      expiresAtUnixMs: BigInt(Date.now() + 60_000),
    }
    const client = await DirectClientHandshake.create(authorization, accountKey)
    const hello = client.clientHello()
    expect(hello).toHaveLength(DIRECT_CLIENT_HELLO_SIZE)
    expect(Array.from(hello.slice(18, 50))).toEqual(Array.from(authorization.ticket))

    const clientPublic = hello.slice(50)
    const host = await keyPair()
    const transcript = marshalDirectTranscript({
      ...authorization,
      clientPublicKey: clientPublic,
      hostPublicKey: host.publicKey,
    })
    const hostProof = buildDirectProof(accountKey, transcript, DirectRole.Host)
    const finish = await client.handleHostHello(join(new Uint8Array([1, 2]), host.publicKey, hostProof))
    expect(finish).toHaveLength(DIRECT_CLIENT_FINISH_SIZE)
    expect(sameBytes(finish.slice(2, 34), buildDirectProof(accountKey, transcript, DirectRole.Client))).toBe(true)

    const clientKeys = client.handleAuthOK(new Uint8Array([1, 4]))
    const hostShared = await deriveP256SharedSecret(host.privateJWK, clientPublic)
    const hostKeys = deriveDirectTrafficKeys(accountKey, hostShared, transcript)
    expect(sameBytes(clientKeys.clientToHostKey, hostKeys.clientToHostKey)).toBe(true)
    expect(sameBytes(clientKeys.hostToClientKey, hostKeys.hostToClientKey)).toBe(true)

    const codec = new DirectClientRecordCodec(clientKeys)
    const outbound = codec.seal(DirectRecordKind.Frame, new Uint8Array([1, 2, 3]))
    expect(openDirectRecord(
      hostKeys.clientToHostKey,
      hostKeys.clientToHostNoncePrefix,
      sha256(transcript),
      0n,
      outbound,
    ).plaintext).toEqual(new Uint8Array([1, 2, 3]))

    const inbound = sealDirectRecord(
      hostKeys.hostToClientKey,
      hostKeys.hostToClientNoncePrefix,
      sha256(transcript),
      DirectRecordKind.DirectReady,
      0n,
      new Uint8Array(8),
    )
    expect(codec.open(inbound).kind).toBe(DirectRecordKind.DirectReady)
  })

  it('rejects a host proof made with another account key', async () => {
    const authorization: DirectAuthorization = {
      attemptId: '11111111-2222-4333-8444-555555555555',
      ticket: new Uint8Array(32).fill(0x31),
      sessionId: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
      userId: 'user-1',
      hostId: 'host-1',
      clientInstanceId: 'client-1',
      permission: DirectPermission.Control,
      expiresAtUnixMs: BigInt(Date.now() + 60_000),
    }
    const client = await DirectClientHandshake.create(authorization, new Uint8Array(32).fill(0x10))
    const hello = client.clientHello()
    const host = await keyPair()
    const transcript = marshalDirectTranscript({
      ...authorization,
      clientPublicKey: hello.slice(50),
      hostPublicKey: host.publicKey,
    })
    const wrongProof = buildDirectProof(new Uint8Array(32).fill(0x11), transcript, DirectRole.Host)
    await expect(client.handleHostHello(join(new Uint8Array([1, 2]), host.publicKey, wrongProof)))
      .rejects.toThrow('host proof mismatch')
  })
})
