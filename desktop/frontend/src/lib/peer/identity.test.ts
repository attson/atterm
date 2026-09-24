import { describe, expect, it } from 'vitest'
import { createInMemorySecureStorage } from '../../platform/secureStorage'
import { loadOrCreateCapacitorPeerIdentity } from '../../platform/capacitorPeerIdentity'
import { createJoinRequest, verifyJoinRequestSubject } from './joinRequest'
import {
  base64URLToBytes,
  loadOrCreateWebPeerIdentity,
  type WebIdentityRecord,
  type WebIdentityRecordStore,
} from './identity'

const GO_JOIN_REQUEST_VECTOR = 'apj1.eyJ2IjoxLCJpbnZpdGF0aW9uIjoiYXRwMS50ZXN0LnBheWxvYWQiLCJzdWJqZWN0X3BlZXJfaWQiOiJRbW1JbERIakV4bG1fSzlxUlhGQmxEN1N3MXRia1hybUxMTTVWRzlTTlZFIiwic3ViamVjdF9wdWJsaWNfa2V5IjoiQkZGY1BXNjU0NWE1Qk5QLXluOVVfYzBNd2VtWHZ6ZGR5bEZhMEtiRHRBTmZSVGEtT2xEekdQdjVwVWRaQXFJaFVDdnZEVmZnakZPeXpBcFc4WDJmazFRIiwibm9uY2UiOiJBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBIiwiY3JlYXRlZF9hdCI6MTgwMDAwMDAwMH0.A02gar35CPrDTlXI1j2xuVZ5oI93FnU6bibNUUHZkCMJRJb6Yr7BFehfaSWwDqVuDXucZntQMyrQmoakQX02Qw'

function memoryWebStore(): WebIdentityRecordStore {
  let value: WebIdentityRecord | null = null
  return {
    async get() { return value },
    async add(record) {
      if (value !== null) return false
      value = record
      return true
    },
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
    expect(second.peerId).toBe(first.peerId)
    expect(second.publicKey).toEqual(first.publicKey)
  })

  it('persists the Capacitor identity through secure storage', async () => {
    const storage = createInMemorySecureStorage()
    const first = await loadOrCreateCapacitorPeerIdentity(storage)
    const second = await loadOrCreateCapacitorPeerIdentity(storage)

    expect(second.peerId).toBe(first.peerId)
    expect(second.publicKey).toEqual(first.publicKey)
    expect(await storage.get('atterm.peer.identity.v1')).toContain('private_pkcs8')
  })

  it('creates and verifies a Go-compatible low-S apj1 subject proof', async () => {
    const identity = await loadOrCreateWebPeerIdentity(memoryWebStore())
    const now = new Date('2027-01-15T08:00:00Z')
    const token = await createJoinRequest(identity, 'atp1.test.payload', now)
    const verified = await verifyJoinRequestSubject(token, now)

    expect(token.startsWith('apj1.')).toBe(true)
    expect(verified.document.subject_peer_id).toBe(identity.peerId)
    expect(verified.document.subject_public_key).toBeTruthy()
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
    expect(verified.document.subject_peer_id).toBe('QmmIlDHjExlm_K9qRXFBlD7Sw1tbkXrmLLM5VG9SNVE')
    expect(verified.publicKey).toHaveLength(65)
  })
})
