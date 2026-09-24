import { sha256 } from '@noble/hashes/sha2.js'
import {
  buildDirectFinishProof,
  buildDirectProof,
  deriveDirectTrafficKeys,
  deriveP256SharedSecret,
  DirectPermission,
  DirectRecordKind,
  DirectRole,
  type DirectTrafficKeys,
  marshalDirectTranscript,
  MAX_DIRECT_RECORD_PLAINTEXT,
  openDirectRecord,
  sealDirectRecord,
} from './directCrypto'
import { fragmentDirectFrame } from './directFraming'

const HANDSHAKE_VERSION = 1
const CLIENT_HELLO_KIND = 1
const HOST_HELLO_KIND = 2
const CLIENT_FINISH_KIND = 3
const AUTH_OK_KIND = 4
const P256_PUBLIC_KEY_SIZE = 65
const PROOF_SIZE = 32
const CLIENT_HELLO_SIZE = 2 + 16 + 32 + P256_PUBLIC_KEY_SIZE
const HOST_HELLO_SIZE = 2 + P256_PUBLIC_KEY_SIZE + PROOF_SIZE
const CLIENT_FINISH_SIZE = 2 + PROOF_SIZE + PROOF_SIZE

export interface DirectAuthorization {
  attemptId: string
  ticket: Uint8Array
  sessionId: string
  userId: string
  hostId: string
  clientInstanceId: string
  permission: DirectPermission
  expiresAtUnixMs: bigint
}

export interface DirectHandshakeKeys extends DirectTrafficKeys {
  transcriptHash: Uint8Array
}

function uuidBytes(value: string): Uint8Array {
  const compact = value.replaceAll('-', '')
  if (!/^[0-9a-fA-F]{32}$/.test(compact)) throw new Error('invalid direct attempt UUID')
  const out = new Uint8Array(16)
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(compact.slice(i * 2, i * 2 + 2), 16)
  return out
}

function equalBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.length !== right.length) return false
  let difference = 0
  for (let i = 0; i < left.length; i++) difference |= left[i] ^ right[i]
  return difference === 0
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const length = parts.reduce((sum, part) => sum + part.length, 0)
  const out = new Uint8Array(length)
  let offset = 0
  for (const part of parts) {
    out.set(part, offset)
    offset += part.length
  }
  return out
}

type ClientHandshakeState = 'await_host_hello' | 'await_auth_ok' | 'complete'

/** Browser half of the fixed four-message direct handshake. The generated
 * private JWK is retained only until HOST_HELLO derives the ECDH secret. */
export class DirectClientHandshake {
  private state: ClientHandshakeState = 'await_host_hello'
  private keys: DirectHandshakeKeys | null = null
  private helloSent = false

  private constructor(
    private readonly authorization: DirectAuthorization,
    private readonly accountKey: Uint8Array,
    private privateJWK: JsonWebKey | null,
    private readonly clientPublicKey: Uint8Array,
  ) {}

  static async create(authorization: DirectAuthorization, accountKey: Uint8Array): Promise<DirectClientHandshake> {
    if (accountKey.length !== 32) throw new Error('direct handshake requires a 32-byte account_key')
    if (authorization.ticket.length !== 32) throw new Error('direct ticket must be 32 bytes')
    if (authorization.expiresAtUnixMs <= BigInt(Date.now())) throw new Error('direct authorization expired')
    const algorithm = { name: 'ECDH', namedCurve: 'P-256' } as const
    const pair = await crypto.subtle.generateKey(algorithm, true, ['deriveBits']) as CryptoKeyPair
    const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey))
    const privateJWK = await crypto.subtle.exportKey('jwk', pair.privateKey)
    if (publicKey.length !== P256_PUBLIC_KEY_SIZE) throw new Error('invalid generated P-256 public key')
    return new DirectClientHandshake(authorization, accountKey.slice(), privateJWK, publicKey)
  }

  clientHello(): Uint8Array {
    if (this.state !== 'await_host_hello' || this.helloSent) throw new Error('direct CLIENT_HELLO already sent')
    this.helloSent = true
    return concatBytes(
      new Uint8Array([HANDSHAKE_VERSION, CLIENT_HELLO_KIND]),
      uuidBytes(this.authorization.attemptId),
      this.authorization.ticket,
      this.clientPublicKey,
    )
  }

  async handleHostHello(message: Uint8Array): Promise<Uint8Array> {
    if (this.state !== 'await_host_hello' || !this.helloSent) throw new Error('unexpected direct HOST_HELLO')
    if (message.length !== HOST_HELLO_SIZE || message[0] !== HANDSHAKE_VERSION || message[1] !== HOST_HELLO_KIND) {
      throw new Error('malformed direct HOST_HELLO')
    }
    if (!this.privateJWK) throw new Error('direct private key unavailable')
    const hostPublicKey = message.slice(2, 2 + P256_PUBLIC_KEY_SIZE)
    const hostProof = message.slice(2 + P256_PUBLIC_KEY_SIZE)
    const transcript = marshalDirectTranscript({
      ...this.authorization,
      clientPublicKey: this.clientPublicKey,
      hostPublicKey,
    })
    const expectedProof = buildDirectProof(this.accountKey, transcript, DirectRole.Host)
    if (!equalBytes(hostProof, expectedProof)) throw new Error('direct host proof mismatch')
    const sharedSecret = await deriveP256SharedSecret(this.privateJWK, hostPublicKey)
    this.privateJWK = null
    const traffic = deriveDirectTrafficKeys(this.accountKey, sharedSecret, transcript)
    sharedSecret.fill(0)
    this.keys = { ...traffic, transcriptHash: sha256(transcript) }
    const clientProof = buildDirectProof(this.accountKey, transcript, DirectRole.Client)
    const finishProof = buildDirectFinishProof(this.accountKey, transcript)
    this.state = 'await_auth_ok'
    return concatBytes(
      new Uint8Array([HANDSHAKE_VERSION, CLIENT_FINISH_KIND]),
      clientProof,
      finishProof,
    )
  }

  handleAuthOK(message: Uint8Array): DirectHandshakeKeys {
    if (this.state !== 'await_auth_ok') throw new Error('unexpected direct AUTH_OK')
    if (message.length !== 2 || message[0] !== HANDSHAKE_VERSION || message[1] !== AUTH_OK_KIND || !this.keys) {
      throw new Error('malformed direct AUTH_OK')
    }
    this.state = 'complete'
    this.accountKey.fill(0)
    return this.keys
  }

  dispose(): void {
    this.privateJWK = null
    this.accountKey.fill(0)
    if (this.keys) {
      this.keys.clientToHostKey.fill(0)
      this.keys.hostToClientKey.fill(0)
      this.keys.clientToHostNoncePrefix.fill(0)
      this.keys.hostToClientNoncePrefix.fill(0)
      this.keys.transcriptHash.fill(0)
      this.keys = null
    }
  }
}

/** Strictly ordered record codec for the browser/client direction. */
export class DirectClientRecordCodec {
  private sendCounter = 0n
  private receiveCounter = 0n
  private nextMessageId = 1n

  constructor(private readonly keys: DirectHandshakeKeys) {}

  seal(kind: DirectRecordKind, plaintext: Uint8Array): Uint8Array {
    const record = sealDirectRecord(
      this.keys.clientToHostKey,
      this.keys.clientToHostNoncePrefix,
      this.keys.transcriptHash,
      kind,
      this.sendCounter,
      plaintext,
    )
    this.sendCounter++
    return record
  }

  sealFrame(frame: Uint8Array): Uint8Array[] {
    if (frame.length <= MAX_DIRECT_RECORD_PLAINTEXT) {
      return [this.seal(DirectRecordKind.Frame, frame)]
    }
    const fragments = fragmentDirectFrame(this.nextMessageId, frame)
    this.nextMessageId++
    return fragments.map((fragment) => this.seal(DirectRecordKind.Fragment, fragment))
  }

  open(record: Uint8Array): { kind: DirectRecordKind; plaintext: Uint8Array } {
    const opened = openDirectRecord(
      this.keys.hostToClientKey,
      this.keys.hostToClientNoncePrefix,
      this.keys.transcriptHash,
      this.receiveCounter,
      record,
    )
    this.receiveCounter++
    return opened
  }
}

export function directPermission(value: string): DirectPermission {
  switch (value) {
    case 'view': return DirectPermission.View
    case 'control': return DirectPermission.Control
    case 'full': return DirectPermission.Full
    default: throw new Error('invalid direct permission')
  }
}

export function decodeDirectTicket(value: string): Uint8Array {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error('invalid direct ticket')
  const standard = value.replaceAll('-', '+').replaceAll('_', '/')
  const padded = standard + '='.repeat((4 - standard.length % 4) % 4)
  const binary = atob(padded)
  const ticket = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  if (ticket.length !== 32) throw new Error('invalid direct ticket length')
  return ticket
}

export const DIRECT_CLIENT_HELLO_SIZE = CLIENT_HELLO_SIZE
export const DIRECT_CLIENT_FINISH_SIZE = CLIENT_FINISH_SIZE
