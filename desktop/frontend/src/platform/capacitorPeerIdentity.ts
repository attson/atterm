import {
  base64URLToBytes,
  bytesToBase64URL,
  peerIDFromPublicKey,
  normalizeP1363LowS,
  ownedCryptoBytes,
  type PeerIdentity,
} from '../lib/peer/identity'
import { secureStorage, type SecureStorage } from './secureStorage'

const IDENTITY_KEY = 'atterm.peer.identity.v1'

interface StoredIdentity {
  v: 1
  private_pkcs8: string
  public_raw: string
}

function parseStoredIdentity(raw: string): StoredIdentity {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    throw new Error('peer identity: invalid Keychain record')
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('peer identity: invalid Keychain record')
  const record = value as Record<string, unknown>
  if (Object.keys(record).sort().join(',') !== 'private_pkcs8,public_raw,v'
    || record.v !== 1
    || typeof record.private_pkcs8 !== 'string'
    || typeof record.public_raw !== 'string') {
    throw new Error('peer identity: invalid Keychain record')
  }
  return record as unknown as StoredIdentity
}

async function importStoredIdentity(record: StoredIdentity): Promise<PeerIdentity> {
  const privatePKCS8 = base64URLToBytes(record.private_pkcs8)
  const publicRaw = base64URLToBytes(record.public_raw)
  const privateKey = await crypto.subtle.importKey(
    'pkcs8',
    ownedCryptoBytes(privatePKCS8),
    { name: 'ECDSA', namedCurve: 'P-256' },
    false,
    ['sign'],
  )
  const publicKey = await crypto.subtle.importKey(
    'raw',
    ownedCryptoBytes(publicRaw),
    { name: 'ECDSA', namedCurve: 'P-256' },
    false,
    ['verify'],
  )
  const probe = crypto.getRandomValues(new Uint8Array(32))
  const signature = new Uint8Array(await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, probe))
  if (!await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, publicKey, ownedCryptoBytes(signature), probe)) {
    throw new Error('peer identity: Keychain private/public key mismatch')
  }
  const peerId = await peerIDFromPublicKey(publicRaw)
  return {
    peerId,
    get publicKey(): Uint8Array { return publicRaw.slice() },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const signed = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, ownedCryptoBytes(message))
      return normalizeP1363LowS(new Uint8Array(signed))
    },
  }
}

async function generateStoredIdentity(): Promise<StoredIdentity> {
  const keys = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']) as CryptoKeyPair
  const privatePKCS8 = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keys.privateKey))
  const publicRaw = new Uint8Array(await crypto.subtle.exportKey('raw', keys.publicKey))
  return { v: 1, private_pkcs8: bytesToBase64URL(privatePKCS8), public_raw: bytesToBase64URL(publicRaw) }
}

export async function loadOrCreateCapacitorPeerIdentity(storage: SecureStorage = secureStorage): Promise<PeerIdentity> {
  const existing = await storage.get(IDENTITY_KEY)
  if (existing !== null) return importStoredIdentity(parseStoredIdentity(existing))
  const record = await generateStoredIdentity()
  await storage.set(IDENTITY_KEY, JSON.stringify(record))
  return importStoredIdentity(record)
}
