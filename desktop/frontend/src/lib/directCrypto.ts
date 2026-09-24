import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { hmac } from '@noble/hashes/hmac.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes } from '@noble/hashes/utils.js'

const TRANSCRIPT_DOMAIN = utf8ToBytes('atterm-direct-handshake-v1')
const PROOF_INFO = utf8ToBytes('atterm-direct-proof-v1')
const CLIENT_PROOF_DOMAIN = utf8ToBytes('atterm-direct-client-v1')
const HOST_PROOF_DOMAIN = utf8ToBytes('atterm-direct-host-v1')
const FINISH_PROOF_DOMAIN = utf8ToBytes('atterm-direct-finish-v1')
const TRAFFIC_INFO = utf8ToBytes('atterm-direct-traffic-v1')
const RECORD_VERSION = 1
const RECORD_HEADER_SIZE = 14
const RECORD_TAG_SIZE = 16

export const MAX_DIRECT_RECORD_PLAINTEXT = 16 * 1024

export enum DirectPermission {
  View = 1,
  Control = 2,
  Full = 3,
}

export enum DirectRole {
  Client = 1,
  Host = 2,
}

export enum DirectRecordKind {
  Frame = 1,
  Fragment = 2,
  DirectReady = 3,
  Ping = 4,
  Pong = 5,
  Close = 6,
}

export interface DirectTranscript {
  attemptId: string
  ticket: Uint8Array
  sessionId: string
  userId: string
  hostId: string
  clientInstanceId: string
  permission: DirectPermission
  expiresAtUnixMs: bigint
  clientPublicKey: Uint8Array
  hostPublicKey: Uint8Array
}

export interface DirectTrafficKeys {
  clientToHostKey: Uint8Array
  hostToClientKey: Uint8Array
  clientToHostNoncePrefix: Uint8Array
  hostToClientNoncePrefix: Uint8Array
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

function uuidBytes(value: string): Uint8Array {
  if (!/^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(value)) {
    throw new Error('invalid UUID')
  }
  const compact = value.replaceAll('-', '')
  const out = new Uint8Array(16)
  for (let i = 0; i < out.length; i++) out[i] = Number.parseInt(compact.slice(i * 2, i * 2 + 2), 16)
  return out
}

function sizedField(value: Uint8Array): Uint8Array {
  if (value.length > 0xffff) throw new Error('direct transcript field too large')
  const out = new Uint8Array(2 + value.length)
  new DataView(out.buffer).setUint16(0, value.length, false)
  out.set(value, 2)
  return out
}

function transcriptString(name: string, value: string): Uint8Array {
  const bytes = utf8ToBytes(value)
  if (bytes.length === 0 || bytes.length > 128) throw new Error(`invalid ${name}`)
  return sizedField(bytes)
}

/** Builds the byte-for-byte canonical transcript shared with Go. Public-key
 * curve validation happens when WebCrypto imports each key for ECDH. */
export function marshalDirectTranscript(value: DirectTranscript): Uint8Array {
  if (value.ticket.length !== 32) throw new Error('direct ticket must be 32 bytes')
  if (value.permission < DirectPermission.View || value.permission > DirectPermission.Full) {
    throw new Error('invalid direct permission')
  }
  if (value.expiresAtUnixMs <= 0n) throw new Error('invalid direct expiry')
  if (value.clientPublicKey.length !== 65 || value.hostPublicKey.length !== 65) {
    throw new Error('P-256 public keys must be 65 bytes')
  }
  const permission = new Uint8Array([value.permission])
  const expiry = new Uint8Array(8)
  new DataView(expiry.buffer).setBigUint64(0, value.expiresAtUnixMs, false)
  return concatBytes(
    TRANSCRIPT_DOMAIN,
    uuidBytes(value.attemptId),
    sizedField(value.ticket),
    uuidBytes(value.sessionId),
    transcriptString('user_id', value.userId),
    transcriptString('host_id', value.hostId),
    transcriptString('client_instance_id', value.clientInstanceId),
    permission,
    expiry,
    sizedField(value.clientPublicKey),
    sizedField(value.hostPublicKey),
  )
}

export function deriveDirectProofKey(accountKey: Uint8Array, transcript: Uint8Array): Uint8Array {
  if (accountKey.length !== 32) throw new Error('account_key must be 32 bytes')
  return hkdf(sha256, accountKey, sha256(transcript), PROOF_INFO, 32)
}

export function buildDirectProof(accountKey: Uint8Array, transcript: Uint8Array, role: DirectRole): Uint8Array {
  const domain = role === DirectRole.Client
    ? CLIENT_PROOF_DOMAIN
    : role === DirectRole.Host
      ? HOST_PROOF_DOMAIN
      : null
  if (!domain) throw new Error('invalid direct role')
  return hmac(sha256, deriveDirectProofKey(accountKey, transcript), concatBytes(domain, sha256(transcript)))
}

export function buildDirectFinishProof(accountKey: Uint8Array, transcript: Uint8Array): Uint8Array {
  return hmac(
    sha256,
    deriveDirectProofKey(accountKey, transcript),
    concatBytes(FINISH_PROOF_DOMAIN, sha256(transcript)),
  )
}

/** Imports a non-exportable P-256 private JWK and a raw peer point, then
 * returns only the ECDH result needed by the attempt key schedule. */
export async function deriveP256SharedSecret(privateJwk: JsonWebKey, peerPublicKey: Uint8Array): Promise<Uint8Array> {
  const algorithm = { name: 'ECDH', namedCurve: 'P-256' } as const
  const privateKey = await crypto.subtle.importKey('jwk', privateJwk, algorithm, false, ['deriveBits'])
  const peerKey = await crypto.subtle.importKey('raw', peerPublicKey as BufferSource, algorithm, false, [])
  const bits = await crypto.subtle.deriveBits({ name: 'ECDH', public: peerKey }, privateKey, 256)
  return new Uint8Array(bits)
}

export function deriveDirectTrafficKeys(
  accountKey: Uint8Array,
  sharedSecret: Uint8Array,
  transcript: Uint8Array,
): DirectTrafficKeys {
  const transcriptHash = sha256(transcript)
  const material = hkdf(
    sha256,
    sharedSecret,
    deriveDirectProofKey(accountKey, transcript),
    concatBytes(TRAFFIC_INFO, transcriptHash),
    96,
  )
  return {
    clientToHostKey: material.slice(0, 32),
    hostToClientKey: material.slice(32, 64),
    clientToHostNoncePrefix: material.slice(64, 80),
    hostToClientNoncePrefix: material.slice(80, 96),
  }
}

function validRecordKind(kind: DirectRecordKind): boolean {
  return kind >= DirectRecordKind.Frame && kind <= DirectRecordKind.Close
}

function recordNonce(prefix: Uint8Array, counter: bigint): Uint8Array {
  if (prefix.length !== 16) throw new Error('direct nonce prefix must be 16 bytes')
  const nonce = new Uint8Array(24)
  nonce.set(prefix)
  new DataView(nonce.buffer).setBigUint64(16, counter, false)
  return nonce
}

function recordHeader(kind: DirectRecordKind, counter: bigint, plainLength: number): Uint8Array {
  if (!validRecordKind(kind)) throw new Error('invalid direct record kind')
  if (counter < 0n || counter > 0xffffffffffffffffn) throw new Error('invalid direct record counter')
  if (plainLength > MAX_DIRECT_RECORD_PLAINTEXT) throw new Error('direct record plaintext too large')
  const header = new Uint8Array(RECORD_HEADER_SIZE)
  header[0] = RECORD_VERSION
  header[1] = kind
  const view = new DataView(header.buffer)
  view.setBigUint64(2, counter, false)
  view.setUint32(10, plainLength, false)
  return header
}

export function sealDirectRecord(
  key: Uint8Array,
  noncePrefix: Uint8Array,
  transcriptHash: Uint8Array,
  kind: DirectRecordKind,
  counter: bigint,
  plaintext: Uint8Array,
): Uint8Array {
  if (key.length !== 32 || transcriptHash.length !== 32) throw new Error('invalid direct record key binding')
  const header = recordHeader(kind, counter, plaintext.length)
  const aad = concatBytes(transcriptHash, header)
  const ciphertext = xchacha20poly1305(key, recordNonce(noncePrefix, counter), aad).encrypt(plaintext)
  return concatBytes(header, ciphertext)
}

export function openDirectRecord(
  key: Uint8Array,
  noncePrefix: Uint8Array,
  transcriptHash: Uint8Array,
  expectedCounter: bigint,
  record: Uint8Array,
): { kind: DirectRecordKind; plaintext: Uint8Array } {
  if (key.length !== 32 || transcriptHash.length !== 32) throw new Error('invalid direct record key binding')
  if (record.length < RECORD_HEADER_SIZE + RECORD_TAG_SIZE) throw new Error('truncated direct record')
  const header = record.subarray(0, RECORD_HEADER_SIZE)
  const view = new DataView(header.buffer, header.byteOffset, header.byteLength)
  if (header[0] !== RECORD_VERSION) throw new Error('unsupported direct record version')
  const kind = header[1] as DirectRecordKind
  if (!validRecordKind(kind)) throw new Error('invalid direct record kind')
  const counter = view.getBigUint64(2, false)
  if (counter !== expectedCounter) throw new Error('invalid direct record sequence')
  const plainLength = view.getUint32(10, false)
  if (plainLength > MAX_DIRECT_RECORD_PLAINTEXT || record.length !== RECORD_HEADER_SIZE + plainLength + RECORD_TAG_SIZE) {
    throw new Error('invalid direct record length')
  }
  const aad = concatBytes(transcriptHash, header)
  const plaintext = xchacha20poly1305(key, recordNonce(noncePrefix, counter), aad)
    .decrypt(record.subarray(RECORD_HEADER_SIZE))
  return { kind, plaintext }
}
