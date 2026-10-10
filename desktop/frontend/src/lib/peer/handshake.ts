import { hkdf } from '@noble/hashes/hkdf.js'
import { hmac } from '@noble/hashes/hmac.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes } from '@noble/hashes/utils.js'
import {
  DirectPermission,
  DirectRole,
  deriveP256SharedSecret,
  marshalDirectTranscript,
} from '../directCrypto'
import type { DirectAuthorization, DirectHandshakeKeys } from '../directHandshake'
import { ownedCryptoBytes, type PeerIdentity } from './identity'
import type { VerifiedGenesis, VerifiedGrant } from './documents'

const HANDSHAKE_VERSION = 1
const CLIENT_HELLO_KIND = 1
const HOST_HELLO_KIND = 2
const CLIENT_FINISH_KIND = 3
const AUTH_OK_KIND = 4
const PUBLIC_KEY_SIZE = 65
const SIGNATURE_SIZE = 64
const PEER_PROOF_DOMAIN = utf8ToBytes('atterm-peer-membership-proof-v1')
const PEER_BINDING_DOMAIN = utf8ToBytes('atterm-peer-membership-binding-v1')
const FINISH_DOMAIN = utf8ToBytes('atterm-direct-finish-v1')
const TRAFFIC_INFO = utf8ToBytes('atterm-direct-traffic-v1')

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((total, part) => total + part.length, 0))
  let offset = 0
  for (const part of parts) {
    out.set(part, offset)
    offset += part.length
  }
  return out
}

function uuidBytes(value: string): Uint8Array {
  const compact = value.replaceAll('-', '')
  if (!/^[0-9a-f]{32}$/i.test(compact)) throw new Error('peer handshake: invalid UUID')
  return Uint8Array.from({ length: 16 }, (_, index) => Number.parseInt(compact.slice(index * 2, index * 2 + 2), 16))
}

async function verifyPeerProof(publicKey: Uint8Array, transcript: Uint8Array, role: DirectRole, signature: Uint8Array): Promise<void> {
  const key = await crypto.subtle.importKey('raw', ownedCryptoBytes(publicKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
  const payload = concatBytes(PEER_PROOF_DOMAIN, new Uint8Array([role]), transcript)
  if (!await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, key, ownedCryptoBytes(signature), ownedCryptoBytes(payload))) {
    throw new Error('peer handshake: host proof mismatch')
  }
}

export function derivePeerBinding(genesisHash: string, clientMembershipToken: string, hostMembershipToken: string, transcript: Uint8Array): Uint8Array {
  return sha256(concatBytes(
    PEER_BINDING_DOMAIN,
    utf8ToBytes(genesisHash),
    sha256(utf8ToBytes(clientMembershipToken)),
    sha256(utf8ToBytes(hostMembershipToken)),
    transcript,
  ))
}

export function buildPeerFinishProof(binding: Uint8Array, transcript: Uint8Array): Uint8Array {
  return hmac(sha256, binding, concatBytes(FINISH_DOMAIN, sha256(transcript)))
}

export function derivePeerTrafficKeys(shared: Uint8Array, binding: Uint8Array, transcript: Uint8Array): DirectHandshakeKeys {
  const transcriptHash = sha256(transcript)
  const material = hkdf(sha256, shared, binding, concatBytes(TRAFFIC_INFO, transcriptHash), 96)
  return {
    clientToHostKey: material.slice(0, 32),
    hostToClientKey: material.slice(32, 64),
    clientToHostNoncePrefix: material.slice(64, 80),
    hostToClientNoncePrefix: material.slice(80, 96),
    transcriptHash,
  }
}

type State = 'await_host_hello' | 'await_auth_ok' | 'complete'

/** Browser client for the same variable-proof handshake used by
 * peertransport.PeerMembershipAuthenticator in Go. */
export class PeerClientHandshake {
  private state: State = 'await_host_hello'
  private helloSent = false
  private privateJWK: JsonWebKey | null
  private keys: DirectHandshakeKeys | null = null

  private constructor(
    private readonly authorization: DirectAuthorization,
    private readonly identity: PeerIdentity,
    private readonly genesis: VerifiedGenesis,
    private readonly clientMembership: VerifiedGrant,
    private readonly hostMembership: VerifiedGrant,
    privateJWK: JsonWebKey,
    private readonly publicKey: Uint8Array,
  ) {
    this.privateJWK = privateJWK
  }

  static async create(
    authorization: DirectAuthorization,
    identity: PeerIdentity,
    genesis: VerifiedGenesis,
    clientMembership: VerifiedGrant,
    hostMembership: VerifiedGrant,
  ): Promise<PeerClientHandshake> {
    if (authorization.ticket.length !== 32 || authorization.expiresAtUnixMs <= BigInt(Date.now())) throw new Error('peer handshake: invalid authorization')
    if (clientMembership.document.subject_peer_id !== identity.peerId || hostMembership.document.subject_peer_id === identity.peerId) {
      throw new Error('peer handshake: invalid membership roles')
    }
    const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']) as CryptoKeyPair
    const [publicKey, privateJWK] = await Promise.all([
      crypto.subtle.exportKey('raw', pair.publicKey).then((value) => new Uint8Array(value)),
      crypto.subtle.exportKey('jwk', pair.privateKey),
    ])
    return new PeerClientHandshake(authorization, identity, genesis, clientMembership, hostMembership, privateJWK, publicKey)
  }

  clientHello(): Uint8Array {
    if (this.state !== 'await_host_hello' || this.helloSent) throw new Error('peer handshake: CLIENT_HELLO already sent')
    this.helloSent = true
    return concatBytes(new Uint8Array([HANDSHAKE_VERSION, CLIENT_HELLO_KIND]), uuidBytes(this.authorization.attemptId), this.authorization.ticket, this.publicKey)
  }

  async handleHostHello(message: Uint8Array): Promise<Uint8Array> {
    if (this.state !== 'await_host_hello' || !this.helloSent || !this.privateJWK ||
        message.length !== 2 + PUBLIC_KEY_SIZE + SIGNATURE_SIZE || message[0] !== HANDSHAKE_VERSION || message[1] !== HOST_HELLO_KIND) {
      throw new Error('peer handshake: malformed HOST_HELLO')
    }
    const hostPublicKey = message.slice(2, 2 + PUBLIC_KEY_SIZE)
    const hostProof = message.slice(2 + PUBLIC_KEY_SIZE)
    const transcript = marshalDirectTranscript({ ...this.authorization, clientPublicKey: this.publicKey, hostPublicKey })
    await verifyPeerProof(this.hostMembership.publicKey, transcript, DirectRole.Host, hostProof)
    const binding = derivePeerBinding(this.genesis.hash, this.clientMembership.token, this.hostMembership.token, transcript)
    const shared = await deriveP256SharedSecret(this.privateJWK, hostPublicKey)
    this.privateJWK = null
    this.keys = derivePeerTrafficKeys(shared, binding, transcript)
    shared.fill(0)
    const clientProof = await this.identity.sign(concatBytes(PEER_PROOF_DOMAIN, new Uint8Array([DirectRole.Client]), transcript))
    this.state = 'await_auth_ok'
    return concatBytes(new Uint8Array([HANDSHAKE_VERSION, CLIENT_FINISH_KIND]), clientProof, buildPeerFinishProof(binding, transcript))
  }

  handleAuthOK(message: Uint8Array): DirectHandshakeKeys {
    if (this.state !== 'await_auth_ok' || message.length !== 2 || message[0] !== HANDSHAKE_VERSION || message[1] !== AUTH_OK_KIND || !this.keys) {
      throw new Error('peer handshake: malformed AUTH_OK')
    }
    this.state = 'complete'
    return this.keys
  }

  dispose(): void {
    this.privateJWK = null
    if (!this.keys) return
    this.keys.clientToHostKey.fill(0)
    this.keys.hostToClientKey.fill(0)
    this.keys.clientToHostNoncePrefix.fill(0)
    this.keys.hostToClientNoncePrefix.fill(0)
    this.keys.transcriptHash.fill(0)
    this.keys = null
  }
}

export function peerPermission(value: string | number): DirectPermission {
  if (value === 'view' || value === DirectPermission.View) return DirectPermission.View
  if (value === 'control' || value === DirectPermission.Control) return DirectPermission.Control
  if (value === 'full' || value === DirectPermission.Full) return DirectPermission.Full
  throw new Error('peer handshake: invalid permission')
}
