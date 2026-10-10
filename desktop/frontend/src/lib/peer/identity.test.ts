import { describe, expect, it } from 'vitest'
import { createInMemorySecureStorage } from '../../platform/secureStorage'
import { loadOrCreateCapacitorPeerIdentity } from '../../platform/capacitorPeerIdentity'
import { createJoinRequest, verifyJoinRequestSubject } from './joinRequest'
import {
  base64URLToBytes,
  bytesToBase64URL,
  loadOrCreateWebPeerIdentity,
  peerIDFromPublicKey,
  type WebIdentityRecord,
  type WebIdentityRecordStore,
} from './identity'

const GO_JOIN_REQUEST_VECTOR = 'apj1.eyJ2IjoxLCJpbnZpdGF0aW9uIjoiYXRwMS50ZXN0LnBheWxvYWQiLCJzdWJqZWN0X3BlZXJfaWQiOiJ1VGl1SVNJQ3k1SkN4U0pCQzBMSGtCMDFGTVhvOEZxOXZGT2FHNzNYT3NRIiwic3ViamVjdF9wdWJsaWNfa2V5IjoiQkNfQUM5SUpnU0s4Y1ZLenVhM1FDdDNnWDRYNDFVczhuaFNmSkI0SldIT0Q0ZEpEd0ZCdHp3RTAtcTFYQVZTR0lfbF9weWZyRnhDaG5oVlhhbkQ5Q3VnIiwic3ViamVjdF93cmFwcGluZ19wdWJsaWNfa2V5IjoiQkhLa1RzcGM1WThhMmp6QmgwckIyV1lWS0ZtMWJxVE13RUJZSUFsSG5vZXJjeDAxVkxmN3k3cFRFa0hPUFJ1dWJGR1ZLRXVNS29jZUhMTkkyUlp6cGc0Iiwibm9uY2UiOiJBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBIiwiY3JlYXRlZF9hdCI6MTgwMDAwMDAwMH0.LAr2uXiub5BWwzva-xGWWXCEX4Cy7YSwTG5iWoWdfPZQfR_EQHaldjOJhnw9Jy6nuKn944MwVrkKauhDFB3wdQ'

function memoryWebStore(initial: WebIdentityRecord | null = null): WebIdentityRecordStore {
  let value: WebIdentityRecord | null = initial
  return {
    async get() { return value },
    async add(record) {
      if (value !== null) return false
      value = record
      return true
    },
    async put(record) { value = record },
  }
}

async function legacyWebRecord(): Promise<WebIdentityRecord> {
  const keys = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']) as CryptoKeyPair
  return { version: 1, privateKey: keys.privateKey, publicKey: keys.publicKey }
}

async function legacyCapacitorRecord(): Promise<{ raw: string; peerId: string }> {
  const keys = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']) as CryptoKeyPair
  const privatePKCS8 = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keys.privateKey))
  const publicRaw = new Uint8Array(await crypto.subtle.exportKey('raw', keys.publicKey))
  return {
    raw: JSON.stringify({
      v: 1,
      private_pkcs8: bytesToBase64URL(privatePKCS8),
      public_raw: bytesToBase64URL(publicRaw),
    }),
    peerId: await peerIDFromPublicKey(publicRaw),
  }
}

describe('PeerIdentity', () => {
  it('keeps the browser private key non-exportable and stable', async () => {
    const store = memoryWebStore()
    const first = await loadOrCreateWebPeerIdentity(store)
    const stored = await store.get()
    const second = await loadOrCreateWebPeerIdentity(store)

    expect(stored?.privateKey.extractable).toBe(false)
    await expect(crypto.subtle.exportKey('pkcs8', stored!.privateKey)).rejects.toBeTruthy()
    expect(stored?.wrappingPrivateKey?.extractable).toBe(false)
    await expect(crypto.subtle.exportKey('pkcs8', stored!.wrappingPrivateKey!)).rejects.toBeTruthy()
    expect(second.peerId).toBe(first.peerId)
    expect(second.publicKey).toEqual(first.publicKey)
    expect(second.wrappingPublicKey).toEqual(first.wrappingPublicKey)
  })

  it('migrates a v1 browser record without rotating its signing identity', async () => {
    const legacy = await legacyWebRecord()
    const originalPublic = new Uint8Array(await crypto.subtle.exportKey('raw', legacy.publicKey))
    const originalPeerId = await peerIDFromPublicKey(originalPublic)
    const store = memoryWebStore(legacy)

    const identity = await loadOrCreateWebPeerIdentity(store)
    const migrated = await store.get()

    expect(identity.peerId).toBe(originalPeerId)
    expect(identity.publicKey).toEqual(originalPublic)
    expect(migrated?.version).toBe(2)
    expect(migrated?.privateKey).toBe(legacy.privateKey)
    expect(migrated?.wrappingPrivateKey?.extractable).toBe(false)
  })

  it('persists the Capacitor identity through secure storage', async () => {
    const storage = createInMemorySecureStorage()
    const first = await loadOrCreateCapacitorPeerIdentity(storage)
    const second = await loadOrCreateCapacitorPeerIdentity(storage)

    expect(second.peerId).toBe(first.peerId)
    expect(second.publicKey).toEqual(first.publicKey)
    expect(second.wrappingPublicKey).toEqual(first.wrappingPublicKey)
    expect(await storage.get('atterm.peer.identity.v1')).toContain('wrapping_private_pkcs8')
  })

  it('migrates a v1 Capacitor record without rotating its signing identity', async () => {
    const storage = createInMemorySecureStorage()
    const legacy = await legacyCapacitorRecord()
    await storage.set('atterm.peer.identity.v1', legacy.raw)

    const identity = await loadOrCreateCapacitorPeerIdentity(storage)
    const migrated = JSON.parse((await storage.get('atterm.peer.identity.v1'))!) as Record<string, unknown>

    expect(identity.peerId).toBe(legacy.peerId)
    expect(migrated.v).toBe(2)
    expect(migrated.wrapping_private_pkcs8).toBeTypeOf('string')
  })

  it('derives matching browser and Capacitor ECDH secrets', async () => {
    const browser = await loadOrCreateWebPeerIdentity(memoryWebStore())
    const capacitor = await loadOrCreateCapacitorPeerIdentity(createInMemorySecureStorage())

    const browserSecret = await browser.deriveWrappingSecret(capacitor.wrappingPublicKey)
    const capacitorSecret = await capacitor.deriveWrappingSecret(browser.wrappingPublicKey)

    expect(browserSecret).toHaveLength(32)
    expect(capacitorSecret).toEqual(browserSecret)
  })

  it('creates and verifies a Go-compatible low-S apj1 subject proof', async () => {
    const identity = await loadOrCreateWebPeerIdentity(memoryWebStore())
    const now = new Date('2027-01-15T08:00:00Z')
    const token = await createJoinRequest(identity, 'atp1.test.payload', now)
    const verified = await verifyJoinRequestSubject(token, now)

    expect(token.startsWith('apj1.')).toBe(true)
    expect(verified.document.subject_peer_id).toBe(identity.peerId)
    expect(verified.document.subject_public_key).toBeTruthy()
    expect(verified.document.subject_wrapping_public_key).toBeTruthy()
    expect(verified.wrappingPublicKey).toEqual(identity.wrappingPublicKey)
    expect(base64URLToBytes(token.split('.')[2])).toHaveLength(64)

    const parts = token.split('.')
    const signature = base64URLToBytes(parts[2])
    signature[0] ^= 1
    const binary = String.fromCharCode(...signature)
    parts[2] = btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
    await expect(verifyJoinRequestSubject(parts.join('.'), now)).rejects.toThrow('signature')
  })

  it('verifies the Go peercrypto apj1 compatibility vector', async () => {
    const verified = await verifyJoinRequestSubject(GO_JOIN_REQUEST_VECTOR, new Date(1_800_000_000_000))
    expect(verified.document.subject_peer_id).toBe('uTiuISICy5JCxSJBC0LHkB01FMXo8Fq9vFOaG73XOsQ')
    expect(verified.publicKey).toHaveLength(65)
    expect(verified.wrappingPublicKey).toHaveLength(65)
  })
})
