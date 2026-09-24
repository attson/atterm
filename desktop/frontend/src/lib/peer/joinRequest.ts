import {
  base64URLToBytes,
  bytesToBase64URL,
  isP1363LowS,
  ownedCryptoBytes,
  peerIDFromPublicKey,
  type PeerIdentity,
} from './identity'

const JOIN_PREFIX = 'apj1'
const INVITATION_PREFIX = 'atp1.'
const MAX_TOKEN_BYTES = 64 << 10
const CLOCK_SKEW_SECONDS = 5 * 60

export interface JoinRequestDocument {
  v: number
  invitation: string
  subject_peer_id: string
  subject_public_key: string
  nonce: string
  created_at: number
}

export interface VerifiedJoinSubject {
  token: string
  document: JoinRequestDocument
  publicKey: Uint8Array
}

const encoder = new TextEncoder()
const decoder = new TextDecoder('utf-8', { fatal: true })

function exactJoinDocument(value: unknown): JoinRequestDocument {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('peer join: invalid document')
  const record = value as Record<string, unknown>
  const expected = ['created_at', 'invitation', 'nonce', 'subject_peer_id', 'subject_public_key', 'v']
  if (Object.keys(record).sort().join('\n') !== expected.join('\n')) throw new Error('peer join: unknown or missing field')
  if (record.v !== 1
    || typeof record.invitation !== 'string'
    || !record.invitation.startsWith(INVITATION_PREFIX)
    || encoder.encode(record.invitation).length > MAX_TOKEN_BYTES
    || typeof record.subject_peer_id !== 'string'
    || typeof record.subject_public_key !== 'string'
    || typeof record.nonce !== 'string'
    || typeof record.created_at !== 'number'
    || !Number.isSafeInteger(record.created_at)
    || record.created_at <= 0) {
    throw new Error('peer join: invalid document')
  }
  return record as unknown as JoinRequestDocument
}

export async function createJoinRequest(identity: PeerIdentity, invitation: string, now: Date = new Date()): Promise<string> {
  if (!invitation.startsWith(INVITATION_PREFIX) || encoder.encode(invitation).length > MAX_TOKEN_BYTES) {
    throw new Error('peer join: invalid invitation')
  }
  const nonce = crypto.getRandomValues(new Uint8Array(32))
  const document: JoinRequestDocument = {
    v: 1,
    invitation,
    subject_peer_id: identity.peerId,
    subject_public_key: bytesToBase64URL(identity.publicKey),
    nonce: bytesToBase64URL(nonce),
    created_at: Math.floor(now.getTime() / 1000),
  }
  const payload = encoder.encode(JSON.stringify(document))
  const signature = await identity.sign(payload)
  const token = `${JOIN_PREFIX}.${bytesToBase64URL(payload)}.${bytesToBase64URL(signature)}`
  if (encoder.encode(token).length > MAX_TOKEN_BYTES) throw new Error('peer join: token too large')
  return token
}

// This verifies the new device's proof of key possession. The redemption host
// still verifies the embedded invitation and membership chain in Go.
export async function verifyJoinRequestSubject(token: string, now: Date = new Date()): Promise<VerifiedJoinSubject> {
  if (encoder.encode(token).length > MAX_TOKEN_BYTES) throw new Error('peer join: token too large')
  const parts = token.split('.')
  if (parts.length !== 3 || parts[0] !== JOIN_PREFIX) throw new Error('peer join: invalid token')
  const payload = base64URLToBytes(parts[1])
  const signature = base64URLToBytes(parts[2])
  if (!isP1363LowS(signature)) throw new Error('peer join: invalid signature encoding')
  let parsed: unknown
  try {
    parsed = JSON.parse(decoder.decode(payload))
  } catch {
    throw new Error('peer join: invalid JSON')
  }
  const document = exactJoinDocument(parsed)
  const nowSeconds = Math.floor(now.getTime() / 1000)
  if (Math.abs(document.created_at - nowSeconds) > CLOCK_SKEW_SECONDS) throw new Error('peer join: request expired')
  const publicKey = base64URLToBytes(document.subject_public_key)
  if (await peerIDFromPublicKey(publicKey) !== document.subject_peer_id) throw new Error('peer join: subject peer id mismatch')
  if (base64URLToBytes(document.nonce).length !== 32) throw new Error('peer join: invalid nonce')
  let key: CryptoKey
  try {
    key = await crypto.subtle.importKey('raw', ownedCryptoBytes(publicKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
  } catch {
    throw new Error('peer join: invalid subject public key')
  }
  const valid = await crypto.subtle.verify(
    { name: 'ECDSA', hash: 'SHA-256' },
    key,
    ownedCryptoBytes(signature),
    ownedCryptoBytes(payload),
  )
  if (!valid) throw new Error('peer join: invalid subject signature')
  return { token, document, publicKey }
}
