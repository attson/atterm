const P256_ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')
const P256_HALF_ORDER = P256_ORDER >> 1n
const WEB_IDENTITY_VERSION = 2
const WEB_IDENTITY_DB = 'atterm-peer-identity-v1'
const WEB_IDENTITY_STORE = 'identities'
const WEB_IDENTITY_KEY = 'active'

export interface PeerIdentity {
  readonly peerId: string
  readonly publicKey: Uint8Array
  readonly wrappingPublicKey: Uint8Array
  sign(message: Uint8Array): Promise<Uint8Array>
  deriveWrappingSecret(peerPublicKey: Uint8Array): Promise<Uint8Array>
}

export interface WebIdentityRecord {
  version: number
  privateKey: CryptoKey
  publicKey: CryptoKey
  wrappingPrivateKey?: CryptoKey
  wrappingPublicKey?: CryptoKey
}

export interface WebIdentityRecordStore {
  get(): Promise<WebIdentityRecord | null>
  add(record: WebIdentityRecord): Promise<boolean>
  put(record: WebIdentityRecord): Promise<void>
}

function signingAlgorithm(): EcKeyGenParams {
  return { name: 'ECDSA', namedCurve: 'P-256' }
}

function wrappingAlgorithm(): EcKeyGenParams {
  return { name: 'ECDH', namedCurve: 'P-256' }
}

function bytesToBigInt(bytes: Uint8Array): bigint {
  let value = 0n
  for (const byte of bytes) value = (value << 8n) | BigInt(byte)
  return value
}

function bigIntTo32Bytes(value: bigint): Uint8Array {
  const out = new Uint8Array(32)
  for (let index = out.length - 1; index >= 0; index--) {
    out[index] = Number(value & 0xffn)
    value >>= 8n
  }
  return out
}

export function normalizeP1363LowS(signature: Uint8Array): Uint8Array {
  if (signature.length !== 64) throw new Error('peer identity: ECDSA signature must be 64-byte P1363')
  const r = bytesToBigInt(signature.subarray(0, 32))
  let s = bytesToBigInt(signature.subarray(32))
  if (r <= 0n || r >= P256_ORDER || s <= 0n || s >= P256_ORDER) {
    throw new Error('peer identity: ECDSA signature scalar out of range')
  }
  if (s > P256_HALF_ORDER) s = P256_ORDER - s
  const out = new Uint8Array(64)
  out.set(signature.subarray(0, 32), 0)
  out.set(bigIntTo32Bytes(s), 32)
  return out
}

export function isP1363LowS(signature: Uint8Array): boolean {
  if (signature.length !== 64) return false
  const r = bytesToBigInt(signature.subarray(0, 32))
  const s = bytesToBigInt(signature.subarray(32))
  return r > 0n && r < P256_ORDER && s > 0n && s <= P256_HALF_ORDER
}

export function bytesToBase64URL(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
}

export function base64URLToBytes(value: string): Uint8Array {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new Error('peer identity: invalid base64url')
  const padded = value.replaceAll('-', '+').replaceAll('_', '/') + '='.repeat((4 - value.length % 4) % 4)
  let binary: string
  try {
    binary = atob(padded)
  } catch {
    throw new Error('peer identity: invalid base64url')
  }
  const out = Uint8Array.from(binary, char => char.charCodeAt(0))
  if (bytesToBase64URL(out) !== value) throw new Error('peer identity: non-canonical base64url')
  return out
}

export function ownedCryptoBytes(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(new ArrayBuffer(bytes.byteLength))
  out.set(bytes)
  return out
}

export async function peerIDFromPublicKey(publicKey: Uint8Array): Promise<string> {
  if (publicKey.length !== 65 || publicKey[0] !== 0x04) throw new Error('peer identity: invalid P-256 public key')
  const digest = await crypto.subtle.digest('SHA-256', ownedCryptoBytes(publicKey))
  return bytesToBase64URL(new Uint8Array(digest))
}

async function identityFromKeys(
  privateKey: CryptoKey,
  publicKey: CryptoKey,
  wrappingPrivateKey: CryptoKey,
  wrappingPublicKey: CryptoKey,
): Promise<PeerIdentity> {
  await verifyWrappingKeyPair(wrappingPrivateKey, wrappingPublicKey)
  const rawPublicKey = new Uint8Array(await crypto.subtle.exportKey('raw', publicKey))
  const rawWrappingPublicKey = new Uint8Array(await crypto.subtle.exportKey('raw', wrappingPublicKey))
  const peerId = await peerIDFromPublicKey(rawPublicKey)
  return {
    peerId,
    get publicKey(): Uint8Array { return rawPublicKey.slice() },
    get wrappingPublicKey(): Uint8Array { return rawWrappingPublicKey.slice() },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const signature = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, ownedCryptoBytes(message))
      return normalizeP1363LowS(new Uint8Array(signature))
    },
    async deriveWrappingSecret(peerPublicKey: Uint8Array): Promise<Uint8Array> {
      let peer: CryptoKey
      try {
        peer = await crypto.subtle.importKey('raw', ownedCryptoBytes(peerPublicKey), wrappingAlgorithm(), false, [])
      } catch {
        throw new Error('peer identity: invalid wrapping public key')
      }
      const shared = await crypto.subtle.deriveBits({ name: 'ECDH', public: peer }, wrappingPrivateKey, 256)
      return new Uint8Array(shared)
    },
  }
}

async function verifyWrappingKeyPair(privateKey: CryptoKey, publicKey: CryptoKey): Promise<void> {
  const probe = await crypto.subtle.generateKey(wrappingAlgorithm(), false, ['deriveBits']) as CryptoKeyPair
  const [fromStoredPrivate, fromStoredPublic] = await Promise.all([
    crypto.subtle.deriveBits({ name: 'ECDH', public: probe.publicKey }, privateKey, 256),
    crypto.subtle.deriveBits({ name: 'ECDH', public: publicKey }, probe.privateKey, 256),
  ])
  const left = new Uint8Array(fromStoredPrivate)
  const right = new Uint8Array(fromStoredPublic)
  if (left.length !== right.length || left.some((value, index) => value !== right[index])) {
    throw new Error('peer identity: stored wrapping private/public key mismatch')
  }
}

function assertSigningKeys(record: WebIdentityRecord): void {
  const privateAlgorithm = record.privateKey.algorithm as Partial<EcKeyAlgorithm>
  const publicAlgorithm = record.publicKey.algorithm as Partial<EcKeyAlgorithm>
  if (record.privateKey.type !== 'private'
    || record.privateKey.extractable
    || !record.privateKey.usages.includes('sign')
    || privateAlgorithm.name !== 'ECDSA'
    || privateAlgorithm.namedCurve !== 'P-256'
    || record.publicKey.type !== 'public'
    || !record.publicKey.usages.includes('verify')
    || publicAlgorithm.name !== 'ECDSA'
    || publicAlgorithm.namedCurve !== 'P-256') {
    throw new Error('peer identity: invalid stored WebCrypto key pair')
  }
}

function assertStoredKey(record: WebIdentityRecord): asserts record is WebIdentityRecord & {
  wrappingPrivateKey: CryptoKey
  wrappingPublicKey: CryptoKey
} {
  assertSigningKeys(record)
  const privateAlgorithm = record.wrappingPrivateKey?.algorithm as Partial<EcKeyAlgorithm> | undefined
  const publicAlgorithm = record.wrappingPublicKey?.algorithm as Partial<EcKeyAlgorithm> | undefined
  if (record.version !== WEB_IDENTITY_VERSION
    || record.wrappingPrivateKey?.type !== 'private'
    || record.wrappingPrivateKey.extractable
    || !record.wrappingPrivateKey.usages.includes('deriveBits')
    || privateAlgorithm?.name !== 'ECDH'
    || privateAlgorithm.namedCurve !== 'P-256'
    || record.wrappingPublicKey?.type !== 'public'
    || publicAlgorithm?.name !== 'ECDH'
    || publicAlgorithm.namedCurve !== 'P-256') {
    throw new Error('peer identity: invalid stored WebCrypto wrapping key pair')
  }
}

async function generateWrappingKeys(): Promise<CryptoKeyPair> {
  return crypto.subtle.generateKey(wrappingAlgorithm(), false, ['deriveBits']) as Promise<CryptoKeyPair>
}

async function generateNonExportableRecord(): Promise<WebIdentityRecord> {
  const [keys, wrappingKeys] = await Promise.all([
    crypto.subtle.generateKey(signingAlgorithm(), false, ['sign', 'verify']) as Promise<CryptoKeyPair>,
    generateWrappingKeys(),
  ])
  const record = {
    version: WEB_IDENTITY_VERSION,
    privateKey: keys.privateKey,
    publicKey: keys.publicKey,
    wrappingPrivateKey: wrappingKeys.privateKey,
    wrappingPublicKey: wrappingKeys.publicKey,
  }
  assertStoredKey(record)
  return record
}

export async function loadOrCreateWebPeerIdentity(store: WebIdentityRecordStore): Promise<PeerIdentity> {
  let record = await store.get()
  if (record === null) {
    const candidate = await generateNonExportableRecord()
    if (await store.add(candidate)) {
      record = candidate
    } else {
      record = await store.get()
      if (record === null) throw new Error('peer identity: concurrent IndexedDB insert disappeared')
    }
  }
  if (record.version === 1) {
    assertSigningKeys(record)
    const wrappingKeys = await generateWrappingKeys()
    const migrated: WebIdentityRecord = {
      ...record,
      version: WEB_IDENTITY_VERSION,
      wrappingPrivateKey: wrappingKeys.privateKey,
      wrappingPublicKey: wrappingKeys.publicKey,
    }
    await store.put(migrated)
    record = await store.get()
    if (record === null) throw new Error('peer identity: migrated IndexedDB record disappeared')
  }
  assertStoredKey(record)
  return identityFromKeys(record.privateKey, record.publicKey, record.wrappingPrivateKey, record.wrappingPublicKey)
}

export function createIndexedDBPeerIdentityStore(factory: IDBFactory = indexedDB): WebIdentityRecordStore {
  let database: Promise<IDBDatabase> | null = null
  const open = (): Promise<IDBDatabase> => database ??= new Promise((resolve, reject) => {
    const request = factory.open(WEB_IDENTITY_DB, 1)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(WEB_IDENTITY_STORE)) {
        request.result.createObjectStore(WEB_IDENTITY_STORE)
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error('peer identity: open IndexedDB failed'))
  })

  return {
    async get(): Promise<WebIdentityRecord | null> {
      const db = await open()
      return new Promise((resolve, reject) => {
        const request = db.transaction(WEB_IDENTITY_STORE, 'readonly').objectStore(WEB_IDENTITY_STORE).get(WEB_IDENTITY_KEY)
        request.onsuccess = () => resolve((request.result as WebIdentityRecord | undefined) ?? null)
        request.onerror = () => reject(request.error ?? new Error('peer identity: read IndexedDB failed'))
      })
    },
    async add(record: WebIdentityRecord): Promise<boolean> {
      const db = await open()
      return new Promise((resolve, reject) => {
        const transaction = db.transaction(WEB_IDENTITY_STORE, 'readwrite')
        let duplicate = false
        const request = transaction.objectStore(WEB_IDENTITY_STORE).add(record, WEB_IDENTITY_KEY)
        request.onerror = (event) => {
          if (request.error?.name === 'ConstraintError') {
            duplicate = true
            event.preventDefault()
            event.stopPropagation()
          }
        }
        transaction.oncomplete = () => resolve(!duplicate)
        transaction.onerror = () => reject(transaction.error ?? new Error('peer identity: write IndexedDB failed'))
        transaction.onabort = () => reject(transaction.error ?? new Error('peer identity: IndexedDB transaction aborted'))
      })
    },
    async put(record: WebIdentityRecord): Promise<void> {
      const db = await open()
      return new Promise((resolve, reject) => {
        const transaction = db.transaction(WEB_IDENTITY_STORE, 'readwrite')
        transaction.objectStore(WEB_IDENTITY_STORE).put(record, WEB_IDENTITY_KEY)
        transaction.oncomplete = () => resolve()
        transaction.onerror = () => reject(transaction.error ?? new Error('peer identity: update IndexedDB failed'))
        transaction.onabort = () => reject(transaction.error ?? new Error('peer identity: IndexedDB transaction aborted'))
      })
    },
  }
}
