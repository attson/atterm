import { describe, expect, it } from 'vitest'
import { sha256 } from '@noble/hashes/sha2.js'
import {
  activeMemberships,
  verifyConnectionBundle,
  verifyGenesis,
  verifyGrant,
  verifyRevocation,
  type ConnectionBundleDocument,
  type DeviceGrantDocument,
  type RevocationDocument,
  type SpaceGenesisDocument,
} from './documents'
import { base64URLToBytes, bytesToBase64URL, normalizeP1363LowS, peerIDFromPublicKey } from './identity'

interface SigningFixture {
  privateKey: CryptoKey
  publicKey: Uint8Array
  wrappingPublicKey: Uint8Array
  peerId: string
}

async function signingFixture(): Promise<SigningFixture> {
  const signing = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify']) as CryptoKeyPair
  const wrapping = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']) as CryptoKeyPair
  const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', signing.publicKey))
  return {
    privateKey: signing.privateKey,
    publicKey,
    wrappingPublicKey: new Uint8Array(await crypto.subtle.exportKey('raw', wrapping.publicKey)),
    peerId: await peerIDFromPublicKey(publicKey),
  }
}

async function token(prefix: string, document: object, signer: SigningFixture): Promise<string> {
  const raw = new TextEncoder().encode(JSON.stringify(document))
  const signature = normalizeP1363LowS(new Uint8Array(await crypto.subtle.sign(
    { name: 'ECDSA', hash: 'SHA-256' }, signer.privateKey, raw,
  )))
  return `${prefix}.${bytesToBase64URL(raw)}.${bytesToBase64URL(signature)}`
}

async function spaceFixture() {
  const now = 1_800_000_000
  const root = await signingFixture()
  const child = await signingFixture()
  const genesisDocument: SpaceGenesisDocument = {
    v: 1,
    space_id: '11111111-2222-4333-8444-555555555555',
    creator_peer_id: root.peerId,
    creator_public_key: bytesToBase64URL(root.publicKey),
    created_at: now - 100,
  }
  const genesisToken = await token('apg1', genesisDocument, root)
  const genesisHash = bytesToBase64URL(sha256(new TextEncoder().encode(genesisToken)))
  const rootDocument: DeviceGrantDocument = {
    v: 1, serial: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', space_id: genesisDocument.space_id,
    space_genesis_hash: genesisHash, subject_peer_id: root.peerId,
    subject_public_key: bytesToBase64URL(root.publicKey),
    subject_wrapping_public_key: bytesToBase64URL(root.wrappingPublicKey), issuer_peer_id: root.peerId,
    issued_at: now - 90, permission: 'full', allowed_session_ids: [], can_invite: true,
    can_sync_secrets: true, delegation_depth: 0,
  }
  const rootToken = await token('apm1', rootDocument, root)
  const childDocument: DeviceGrantDocument = {
    v: 1, serial: 'bbbbbbbb-cccc-4ddd-8eee-ffffffffffff', space_id: genesisDocument.space_id,
    space_genesis_hash: genesisHash, subject_peer_id: child.peerId,
    subject_public_key: bytesToBase64URL(child.publicKey),
    subject_wrapping_public_key: bytesToBase64URL(child.wrappingPublicKey), issuer_peer_id: root.peerId,
    issuer_membership: rootToken, issued_at: now - 80, expires_at: now + 3600,
    permission: 'control', allowed_session_ids: ['22222222-3333-4444-8555-666666666666'],
    can_invite: true, can_sync_secrets: false, delegation_depth: 1,
  }
  const childToken = await token('apm1', childDocument, root)
  return { now, root, child, genesisDocument, genesisToken, rootDocument, rootToken, childDocument, childToken }
}

describe('Peer signed documents', () => {
  it('verifies an exact-byte Go-compatible chain and ticketless route bundle', async () => {
    const fixture = await spaceFixture()
    const genesis = await verifyGenesis(fixture.genesisToken)
    const child = await verifyGrant(fixture.childToken, genesis, new Date(fixture.now * 1000))
    expect(child.document.subject_peer_id).toBe(fixture.child.peerId)

    const bundleDocument: ConnectionBundleDocument = {
      v: 1, bundle_id: '33333333-4444-4555-8666-777777777777', genesis: fixture.genesisToken,
      issuer_peer_id: fixture.root.peerId, issuer_membership: fixture.rootToken,
      routes: [{ kind: 'quick_tunnel', url: 'https://unit-test.trycloudflare.com/' }],
      created_at: fixture.now, expires_at: fixture.now + 600,
    }
    const bundleToken = await token('atc1', bundleDocument, fixture.root)
    const bundle = await verifyConnectionBundle(bundleToken, new Date(fixture.now * 1000))
    expect(bundle.route.kind).toBe('quick_tunnel')
    expect(bundle.issuer.token).toBe(fixture.rootToken)
  })

  it('rejects capability broadening, unknown route fields, and payload tampering', async () => {
    const fixture = await spaceFixture()
    const genesis = await verifyGenesis(fixture.genesisToken)
    const broadened: DeviceGrantDocument = {
      ...fixture.childDocument,
      serial: '55555555-6666-4777-8888-999999999999',
      subject_peer_id: fixture.root.peerId,
      subject_public_key: bytesToBase64URL(fixture.root.publicKey),
      subject_wrapping_public_key: bytesToBase64URL(fixture.root.wrappingPublicKey),
      issuer_peer_id: fixture.child.peerId,
      issuer_membership: fixture.childToken,
      issued_at: fixture.now - 70,
      permission: 'full',
      delegation_depth: 2,
    }
    await expect(verifyGrant(await token('apm1', broadened, fixture.child), genesis, new Date(fixture.now * 1000)))
      .rejects.toThrow('broadens capabilities')

    const parts = fixture.childToken.split('.')
    const payload = base64URLToBytes(parts[1])
    payload[0] ^= 1
    await expect(verifyGrant(`${parts[0]}.${bytesToBase64URL(payload)}.${parts[2]}`, genesis, new Date(fixture.now * 1000)))
      .rejects.toThrow()
  })

  it('applies signed member revocation with deny-wins semantics', async () => {
    const fixture = await spaceFixture()
    const genesis = await verifyGenesis(fixture.genesisToken)
    const revocationDocument: RevocationDocument = {
      v: 1, revocation_id: '44444444-5555-4666-8777-888888888888',
      space_id: fixture.genesisDocument.space_id, space_genesis_hash: genesis.hash,
      kind: 'member', target_id: fixture.child.peerId, actor_peer_id: fixture.root.peerId,
      actor_membership: fixture.rootToken, created_at: fixture.now,
    }
    const revocationToken = await token('arv1', revocationDocument, fixture.root)
    expect((await verifyRevocation(revocationToken, genesis)).document.target_id).toBe(fixture.child.peerId)
    const active = await activeMemberships(
      [fixture.rootToken, fixture.childToken], [revocationToken], genesis, new Date(fixture.now * 1000),
    )
    expect(active.map((grant) => grant.document.subject_peer_id)).toEqual([fixture.root.peerId])
  })
})
