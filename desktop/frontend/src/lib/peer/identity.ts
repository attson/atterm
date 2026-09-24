const P256_ORDER = BigInt('0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551')
const P256_HALF_ORDER = P256_ORDER >> 1n
const WEB_IDENTITY_VERSION = 1
const WEB_IDENTITY_DB = 'atterm-peer-identity-v1'
const WEB_IDENTITY_STORE = 'identities'
const WEB_IDENTITY_KEY = 'active'

export interface PeerIdentity {
  readonly peerId: string
  readonly publicKey: Uint8Array
  sign(message: Uint8Array): Promise<Uint8Array>
}

export interface WebIdentityRecord {
  version: number
  privateKey: CryptoKey
  publicKey: CryptoKey
}

export interface WebIdentityRecordStore {
  get(): Promise<WebIdentityRecord | null>
  add(record: WebIdentityRecord): Promise<boolean>
}

function p256Algorithm(): EcKeyGenParams {
  return { name: 'ECDSA', namedCurve: 'P-256' }
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

async function identityFromKeys(privateKey: CryptoKey, publicKey: CryptoKey): Promise<PeerIdentity> {
  const rawPublicKey = new Uint8Array(await crypto.subtle.exportKey('raw', publicKey))
  const peerId = await peerIDFromPublicKey(rawPublicKey)
  return {
    peerId,
    get publicKey(): Uint8Array { return rawPublicKey.slice() },
    async sign(message: Uint8Array): Promise<Uint8Array> {
      const signature = await crypto.subtle.sign({ name: 'ECDSA', hash: 'SHA-256' }, privateKey, ownedCryptoBytes(message))
      return normalizeP1363LowS(new Uint8Array(signature))
    },
  }
}

function assertStoredKey(record: WebIdentityRecord): void {
  const privateAlgorithm = record.privateKey.algorithm as Partial<EcKeyAlgorithm>
  const publicAlgorithm = record.publicKey.algorithm as Partial<EcKeyAlgorithm>
  if (record.version !== WEB_IDENTITY_VERSION
    || record.privateKey.type !== 'private'
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

async function generateNonExportableRecord(): Promise<WebIdentityRecord> {
  const keys = await crypto.subtle.generateKey(p256Algorithm(), false, ['sign', 'verify']) as CryptoKeyPair
  const record = { version: WEB_IDENTITY_VERSION, privateKey: keys.privateKey, publicKey: keys.publicKey }
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
  assertStoredKey(record)
  return identityFromKeys(record.privateKey, record.publicKey)
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
  }
}
