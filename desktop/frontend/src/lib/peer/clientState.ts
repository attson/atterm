import { xchacha20poly1305 } from '@noble/ciphers/chacha.js'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes } from '@noble/hashes/utils.js'
import type { PeerConnectionPreview, PeerMember, PeerSpaceStatus } from '../../platform/types'
import { createJoinRequest } from './joinRequest'
import {
  base64URLToBytes,
  bytesToBase64URL,
  ownedCryptoBytes,
  type PeerIdentity,
} from './identity'
import {
  activeMemberships,
  verifyConnectionBundle,
  verifyGenesis,
  verifyGrant,
  verifyMembershipForPeer,
  type VerifiedConnectionBundle,
} from './documents'

const JOIN_KEY_INFO = 'atterm-quick-tunnel-join-v1'
const STATE_VERSION = 1
const encoder = new TextEncoder()
const decoder = new TextDecoder('utf-8', { fatal: true })

export interface PeerClientState {
  v: 1
  genesis: string
  membership: string
  memberships: string[]
  revocations: string[]
  epoch_rotations: string[]
  epoch_envelopes: string[]
  route_bundle: string
  joined_at: number
  fallback_consent: boolean
}

export interface PeerClientStateStore {
  load(): Promise<PeerClientState | null>
  save(state: PeerClientState): Promise<void>
  clear(): Promise<void>
}

export interface JoinBootstrap {
  genesis: string
  membership: string
  memberships: string[]
  revocations?: string[]
  epoch_rotations: string[]
  epoch_envelopes: string[]
  connection_bundle: string
}

interface JoinEnvelope {
  v: number
  invite_id: string
  nonce: string
  ciphertext: string
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((total, part) => total + part.length, 0))
  let offset = 0
  for (const part of parts) {
    out.set(part, offset)
    offset += part.length
  }
  return out
}

function joinKey(secret: Uint8Array, inviteId: string, direction: 'request' | 'response'): Uint8Array {
  return hkdf(sha256, secret, utf8ToBytes(inviteId), utf8ToBytes(`${JOIN_KEY_INFO}\0${direction}`), 32)
}

function joinAAD(inviteId: string, direction: 'request' | 'response'): Uint8Array {
  return utf8ToBytes(`${JOIN_KEY_INFO}\0${direction}\0${inviteId}`)
}

function sealJoin(secret: Uint8Array, inviteId: string, direction: 'request' | 'response', plaintext: Uint8Array): JoinEnvelope {
  const nonce = crypto.getRandomValues(new Uint8Array(24))
  const ciphertext = xchacha20poly1305(joinKey(secret, inviteId, direction), nonce, joinAAD(inviteId, direction)).encrypt(plaintext)
  return { v: 1, invite_id: inviteId, nonce: bytesToBase64URL(nonce), ciphertext: bytesToBase64URL(ciphertext) }
}

function openJoin(secret: Uint8Array, inviteId: string, direction: 'request' | 'response', envelope: JoinEnvelope): Uint8Array {
  if (envelope.v !== 1 || envelope.invite_id !== inviteId) throw new Error('Peer Space join rejected')
  const nonce = base64URLToBytes(envelope.nonce)
  const ciphertext = base64URLToBytes(envelope.ciphertext)
  if (nonce.length !== 24 || ciphertext.length < 16) throw new Error('Peer Space join rejected')
  return xchacha20poly1305(joinKey(secret, inviteId, direction), nonce, joinAAD(inviteId, direction)).decrypt(ciphertext)
}

function strictBootstrap(value: unknown): JoinBootstrap {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Peer Space join rejected')
  const record = value as Record<string, unknown>
  const required = ['connection_bundle', 'epoch_envelopes', 'epoch_rotations', 'genesis', 'membership', 'memberships']
  const allowed = new Set([...required, 'revocations'])
  if (required.some((key) => !(key in record)) || Object.keys(record).some((key) => !allowed.has(key))) throw new Error('Peer Space join rejected')
  for (const key of ['connection_bundle', 'genesis', 'membership'] as const) if (typeof record[key] !== 'string') throw new Error('Peer Space join rejected')
  for (const key of ['memberships', 'epoch_rotations', 'epoch_envelopes'] as const) {
    if (!Array.isArray(record[key]) || (record[key] as unknown[]).some((item) => typeof item !== 'string')) throw new Error('Peer Space join rejected')
  }
  if (record.revocations !== undefined && (!Array.isArray(record.revocations) || record.revocations.some((item) => typeof item !== 'string'))) {
    throw new Error('Peer Space join rejected')
  }
  return record as unknown as JoinBootstrap
}

function connectionEndpoint(bundle: VerifiedConnectionBundle, path: string): string {
  const endpoint = new URL(bundle.route.url)
  endpoint.pathname = path
  endpoint.search = ''
  endpoint.hash = ''
  return endpoint.toString()
}

export async function previewPeerConnection(raw: string): Promise<{ bundle: VerifiedConnectionBundle; preview: PeerConnectionPreview }> {
  const bundle = await verifyConnectionBundle(raw)
  if (!bundle.ticket) throw new Error('Peer Space join rejected')
  return {
    bundle,
    preview: {
      space_id: bundle.genesis.document.space_id,
      fingerprint: `SHA256:${bundle.genesis.hash}`,
      issuer_peer_id: bundle.document.issuer_peer_id,
      permission: bundle.ticket.permission,
      allowed_session_ids: [...bundle.ticket.allowed_session_ids],
      can_invite: bundle.ticket.can_invite,
      can_sync_secrets: bundle.ticket.can_sync_secrets,
      invitation_expires_at: bundle.ticket.expires_at,
      bundle_expires_at: bundle.document.expires_at,
      route_kind: bundle.route.kind,
      route_url: bundle.route.url,
    },
  }
}

export async function joinPeerSpace(identity: PeerIdentity, raw: string, expectedFingerprint: string): Promise<PeerClientState> {
  const { bundle, preview } = await previewPeerConnection(raw)
  if (!bundle.ticket || expectedFingerprint.trim() !== preview.fingerprint) throw new Error('Peer Space fingerprint confirmation does not match')
  const secret = base64URLToBytes(bundle.ticket.pairing_secret)
  const joinRequest = await createJoinRequest(identity, bundle.document.ticket!, new Date())
  const payload = encoder.encode(JSON.stringify({ v: 1, join_request: joinRequest }))
  const requestEnvelope = sealJoin(secret, bundle.ticket.invite_id, 'request', payload)
  const response = await fetch(connectionEndpoint(bundle, '/peer/v1/join'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestEnvelope),
    credentials: 'omit',
    redirect: 'error',
  })
  if (!response.ok) throw new Error('Peer Space join rejected')
  const responseText = await response.text()
  if (encoder.encode(responseText).length > 8 << 20) throw new Error('Peer Space join rejected')
  let wire: unknown
  try { wire = JSON.parse(responseText) } catch { throw new Error('Peer Space join rejected') }
  const plaintext = openJoin(secret, bundle.ticket.invite_id, 'response', wire as JoinEnvelope)
  if (plaintext.length > 4 << 20) throw new Error('Peer Space join rejected')
  let decoded: unknown
  try { decoded = JSON.parse(decoder.decode(plaintext)) } catch { throw new Error('Peer Space join rejected') }
  const bootstrap = strictBootstrap(decoded)
  if (bootstrap.genesis !== bundle.genesis.token) throw new Error('Peer Space join rejected')
  const local = await verifyMembershipForPeer(bootstrap.membership, bundle.genesis, identity.peerId)
  if (local.document.subject_public_key !== bytesToBase64URL(identity.publicKey) ||
      local.document.subject_wrapping_public_key !== bytesToBase64URL(identity.wrappingPublicKey) ||
      local.document.issuer_peer_id !== bundle.ticket.issuer_peer_id ||
      local.document.permission !== bundle.ticket.permission ||
      JSON.stringify(local.document.allowed_session_ids) !== JSON.stringify(bundle.ticket.allowed_session_ids) ||
      local.document.can_invite !== bundle.ticket.can_invite || local.document.can_sync_secrets !== bundle.ticket.can_sync_secrets) {
    throw new Error('Peer Space join rejected')
  }
  const membershipTokens = [...bootstrap.memberships, bootstrap.membership].filter((token, index, all) => all.indexOf(token) === index)
  const active = await activeMemberships(membershipTokens, bootstrap.revocations ?? [], bundle.genesis)
  if (!active.some((grant) => grant.token === bootstrap.membership)) throw new Error('Peer Space join rejected')
  const reconnect = await verifyConnectionBundle(bootstrap.connection_bundle)
  if (reconnect.ticket || reconnect.genesis.hash !== bundle.genesis.hash ||
      !active.some((grant) => grant.token === reconnect.issuer.token)) throw new Error('Peer Space join rejected')
  secret.fill(0)
  return {
    v: STATE_VERSION,
    genesis: bootstrap.genesis,
    membership: bootstrap.membership,
    memberships: membershipTokens,
    revocations: [...(bootstrap.revocations ?? [])],
    epoch_rotations: [...bootstrap.epoch_rotations],
    epoch_envelopes: [...bootstrap.epoch_envelopes],
    route_bundle: reconnect.token,
    joined_at: Math.floor(Date.now() / 1000),
    fallback_consent: false,
  }
}

export async function peerClientStatus(identity: PeerIdentity, state: PeerClientState | null): Promise<PeerSpaceStatus> {
  if (!state) return { configured: false, open_invitations: 0, used_invitations: 0, revoked_invitations: 0, expired_invitations: 0 }
  const genesis = await verifyGenesis(state.genesis)
  await verifyMembershipForPeer(state.membership, genesis, identity.peerId)
  const active = await activeMemberships(state.memberships, state.revocations, genesis)
  if (!active.some((grant) => grant.token === state.membership)) throw new Error('Peer Space membership is no longer active')
  return {
    configured: true,
    peer_id: identity.peerId,
    space_id: genesis.document.space_id,
    genesis_hash: genesis.hash,
    created_at: state.joined_at,
    open_invitations: 0,
    used_invitations: 0,
    revoked_invitations: 0,
    expired_invitations: 0,
  }
}

export async function peerClientMembers(state: PeerClientState): Promise<PeerMember[]> {
  const genesis = await verifyGenesis(state.genesis)
  const grants = await activeMemberships(state.memberships, state.revocations, genesis)
  return grants.map((grant) => ({
    peer_id: grant.document.subject_peer_id,
    grant_serial: grant.document.serial,
    issuer_peer_id: grant.document.issuer_peer_id,
    permission: grant.document.permission,
    allowed_session_ids: [...grant.document.allowed_session_ids],
    can_invite: grant.document.can_invite,
    can_sync_secrets: grant.document.can_sync_secrets,
    issued_at: grant.document.issued_at,
    expires_at: grant.document.expires_at,
    status: 'active',
    local: grant.token === state.membership,
    can_revoke: false,
  }))
}

export function createLocalPeerStateStore(key: string, storage: Storage = localStorage): PeerClientStateStore {
  return {
    async load() {
      const raw = storage.getItem(key)
      if (!raw) return null
      const parsed = JSON.parse(raw) as Partial<PeerClientState>
      if (!validStoredState(parsed)) throw new Error('peer client state is invalid')
      return parsed as PeerClientState
    },
    async save(state) { storage.setItem(key, JSON.stringify(state)) },
    async clear() { storage.removeItem(key) },
  }
}

export function createSecurePeerStateStore(key: string, storage: { get(key: string): Promise<string | null>; set(key: string, value: string): Promise<void>; remove(key: string): Promise<void> }): PeerClientStateStore {
  return {
    async load() {
      const raw = await storage.get(key)
      if (!raw) return null
      const parsed = JSON.parse(raw) as Partial<PeerClientState>
      if (!validStoredState(parsed)) throw new Error('peer client state is invalid')
      return parsed
    },
    async save(state) { await storage.set(key, JSON.stringify(state)) },
    async clear() { await storage.remove(key) },
  }
}

function validStoredState(state: Partial<PeerClientState>): state is PeerClientState {
  return state.v === STATE_VERSION && typeof state.genesis === 'string' && typeof state.membership === 'string' &&
    typeof state.route_bundle === 'string' && Number.isSafeInteger(state.joined_at) && (state.joined_at ?? 0) > 0 &&
    typeof state.fallback_consent === 'boolean' &&
    [state.memberships, state.revocations, state.epoch_rotations, state.epoch_envelopes]
      .every((values) => Array.isArray(values) && values.every((value) => typeof value === 'string'))
}

export function joinEnvelopeVector(secret: Uint8Array, inviteId: string, plaintext: Uint8Array): Uint8Array {
  const key = joinKey(secret, inviteId, 'request')
  return concatBytes(key, joinAAD(inviteId, 'request'), ownedCryptoBytes(plaintext))
}
