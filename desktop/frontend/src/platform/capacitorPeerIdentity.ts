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

interface StoredIdentityV1 {
  v: 1
  private_pkcs8: string
  public_raw: string
}

interface StoredIdentityV2 {
  v: 2
  private_pkcs8: string
  public_raw: string
  wrapping_private_pkcs8: string
  wrapping_public_raw: string
}

type StoredIdentity = StoredIdentityV1 | StoredIdentityV2

function parseStoredIdentity(raw: string): StoredIdentity {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    throw new Error('peer identity: invalid Keychain record')
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error('peer identity: invalid Keychain record')
  const record = value as Record<string, unknown>
  const commonValid = typeof record.private_pkcs8 === 'string' && typeof record.public_raw === 'string'
  if (record.v === 1
    && Object.keys(record).sort().join(',') === 'private_pkcs8,public_raw,v'
    && commonValid) {
    return record as unknown as StoredIdentityV1
  }
  if (record.v !== 2
    || Object.keys(record).sort().join(',') !== 'private_pkcs8,public_raw,v,wrapping_private_pkcs8,wrapping_public_raw'
    || !commonValid
    || typeof record.wrapping_private_pkcs8 !== 'string'
    || typeof record.wrapping_public_raw !== 'string') {
    throw new Error('peer identity: invalid Keychain record')
  }
  return record as unknown as StoredIdentityV2
}

async function importStoredIdentity(record: StoredIdentityV2): Promise<PeerIdentity> {
  const privatePKCS8 = base64URLToBytes(record.private_pkcs8)
  const publicRaw = base64URLToBytes(record.public_raw)
  const wrappingPrivatePKCS8 = base64URLToBytes(record.wrapping_private_pkcs8)
  const wrappingPublicRaw = base64URLToBytes(record.wrapping_public_raw)
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
  const wrappingPrivateKey = await crypto.subtle.importKey(
    'pkcs8',
    ownedCryptoBytes(wrappingPrivatePKCS8),
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    ['deriveBits'],
  )
  const wrappingPublicKey = await crypto.subtle.importKey(
    'raw',
    ownedCryptoBytes(wrappingPublicRaw),
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    [],
  )
  const probe = crypto.getRandomValues(new Uint8Array(32))
  const signature = new Uint8Array(await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, probe))
  if (!await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, publicKey, ownedCryptoBytes(signature), probe)) {
    throw new Error('peer identity: Keychain private/public key mismatch')
  }
  const wrappingProbe = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, false, ['deriveBits']) as CryptoKeyPair
  const [fromStoredPrivate, fromStoredPublic] = await Promise.all([
    crypto.subtle.deriveBits({ name: 'ECDH', public: wrappingProbe.publicKey }, wrappingPrivateKey, 256),
    crypto.subtle.deriveBits({ name: 'ECDH', public: wrappingPublicKey }, wrappingProbe.privateKey, 256),
  ])
  const left = new Uint8Array(fromStoredPrivate)
  const right = new Uint8Array(fromStoredPublic)
  if (left.length !== right.length || left.some((value, index) => value !== right[index])) {
    throw new Error('peer identity: Keychain wrapping private/public key mismatch')
  }
  const peerId = await peerIDFromPublicKey(publicRaw)
  return {
    peerId,
    get publicKey(): Uint8Array { return publicRaw.slice() },
    get wrappingPublicKey(): Uint8Array { return wrappingPublicRaw.slice() },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const signed = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, ownedCryptoBytes(message))
      return normalizeP1363LowS(new Uint8Array(signed))
    },
    async deriveWrappingSecret(peerPublicKey: Uint8Array): Promise<Uint8Array> {
      let peer: CryptoKey
      try {
        peer = await crypto.subtle.importKey(
          'raw',
          ownedCryptoBytes(peerPublicKey),
          { name: 'ECDH', namedCurve: 'P-256' },
          false,
          [],
        )
      } catch {
        throw new Error('peer identity: invalid wrapping public key')
      }
      const shared = await crypto.subtle.deriveBits({ name: 'ECDH', public: peer }, wrappingPrivateKey, 256)
      return new Uint8Array(shared)
    },
  }
}

async function generateWrappingRecord(): Promise<Pick<StoredIdentityV2, 'wrapping_private_pkcs8' | 'wrapping_public_raw'>> {
  const keys = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']) as CryptoKeyPair
  const privatePKCS8 = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keys.privateKey))
  const publicRaw = new Uint8Array(await crypto.subtle.exportKey('raw', keys.publicKey))
  return {
    wrapping_private_pkcs8: bytesToBase64URL(privatePKCS8),
    wrapping_public_raw: bytesToBase64URL(publicRaw),
  }
}

async function generateStoredIdentity(): Promise<StoredIdentityV2> {
  const [keys, wrapping] = await Promise.all([
    crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']) as Promise<CryptoKeyPair>,
    generateWrappingRecord(),
  ])
  const privatePKCS8 = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keys.privateKey))
  const publicRaw = new Uint8Array(await crypto.subtle.exportKey('raw', keys.publicKey))
  return { v: 2, private_pkcs8: bytesToBase64URL(privatePKCS8), public_raw: bytesToBase64URL(publicRaw), ...wrapping }
}

export async function loadOrCreateCapacitorPeerIdentity(storage: SecureStorage = secureStorage): Promise<PeerIdentity> {
  const existing = await storage.get(IDENTITY_KEY)
  if (existing !== null) {
    const parsed = parseStoredIdentity(existing)
    if (parsed.v === 2) return importStoredIdentity(parsed)
    const migrated: StoredIdentityV2 = { ...parsed, v: 2, ...await generateWrappingRecord() }
    await storage.set(IDENTITY_KEY, JSON.stringify(migrated))
    return importStoredIdentity(migrated)
  }
  const record = await generateStoredIdentity()
  await storage.set(IDENTITY_KEY, JSON.stringify(record))
  return importStoredIdentity(record)
}
