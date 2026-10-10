import { sha256 } from '@noble/hashes/sha2.js'
import {
  base64URLToBytes,
  bytesToBase64URL,
  isP1363LowS,
  ownedCryptoBytes,
  peerIDFromPublicKey,
} from './identity'

const MAX_TOKEN_BYTES = 64 << 10
const CLOCK_SKEW_SECONDS = 5 * 60
const encoder = new TextEncoder()
const decoder = new TextDecoder('utf-8', { fatal: true })

export type PeerPermission = 'view' | 'control' | 'full'

export interface SpaceGenesisDocument {
  v: 1
  space_id: string
  creator_peer_id: string
  creator_public_key: string
  created_at: number
}

export interface DeviceGrantDocument {
  v: 1
  serial: string
  space_id: string
  space_genesis_hash: string
  subject_peer_id: string
  subject_public_key: string
  subject_wrapping_public_key: string
  issuer_peer_id: string
  issuer_membership?: string
  issued_at: number
  expires_at?: number
  permission: PeerPermission
  allowed_session_ids: string[]
  can_invite: boolean
  can_sync_secrets: boolean
  delegation_depth: number
}

export interface CapabilityTicketDocument {
  v: 1
  invite_id: string
  batch_id: string
  space_id: string
  redemption_peer_id: string
  space_genesis_hash: string
  issuer_peer_id: string
  issuer_membership: string
  pairing_secret: string
  issued_at: number
  expires_at: number
  max_uses: 1
  permission: PeerPermission
  allowed_session_ids: string[]
  can_invite: boolean
  can_sync_secrets: boolean
}

export interface ConnectionRouteDocument {
  kind: 'quick_tunnel' | 'rendezvous' | 'manual_lan'
  url: string
  topic?: string
}

export interface ConnectionBundleDocument {
  v: 1
  bundle_id: string
  genesis: string
  ticket?: string
  issuer_peer_id: string
  issuer_membership: string
  routes: ConnectionRouteDocument[]
  created_at: number
  expires_at: number
}

export type RevocationKind = 'member' | 'grant' | 'invitation_batch'

export interface RevocationDocument {
  v: 1
  revocation_id: string
  space_id: string
  space_genesis_hash: string
  kind: RevocationKind
  target_id: string
  actor_peer_id: string
  actor_membership: string
  created_at: number
}

export interface VerifiedRevocation {
  token: string
  document: RevocationDocument
  hash: string
}

export interface VerifiedGenesis {
  token: string
  document: SpaceGenesisDocument
  publicKey: Uint8Array
  hash: string
}

export interface VerifiedGrant {
  token: string
  document: DeviceGrantDocument
  publicKey: Uint8Array
  wrappingPublicKey: Uint8Array
}

export interface VerifiedConnectionBundle {
  token: string
  document: ConnectionBundleDocument
  genesis: VerifiedGenesis
  issuer: VerifiedGrant
  ticket?: CapabilityTicketDocument
  route: ConnectionRouteDocument
}

function isUUID(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function isPermission(value: unknown): value is PeerPermission {
  return value === 'view' || value === 'control' || value === 'full'
}

function isDigest(value: unknown): value is string {
  if (typeof value !== 'string') return false
  try { return base64URLToBytes(value).length === 32 } catch { return false }
}

function permissionRank(value: PeerPermission): number {
  return value === 'view' ? 1 : value === 'control' ? 2 : 3
}

function exactKeys(value: Record<string, unknown>, required: string[], optional: string[] = []): void {
  const allowed = new Set([...required, ...optional])
  if (required.some((key) => !(key in value)) || Object.keys(value).some((key) => !allowed.has(key))) {
    throw new Error('peer document: unknown or missing field')
  }
}

function parseSigned<T>(prefix: string, token: string): { raw: Uint8Array; signature: Uint8Array; document: T } {
  if (encoder.encode(token).length === 0 || encoder.encode(token).length > MAX_TOKEN_BYTES) throw new Error('peer document: token size')
  const parts = token.split('.')
  if (parts.length !== 3 || parts[0] !== prefix) throw new Error('peer document: token prefix')
  const raw = base64URLToBytes(parts[1])
  const signature = base64URLToBytes(parts[2])
  if (!isP1363LowS(signature)) throw new Error('peer document: invalid signature encoding')
  let document: unknown
  try {
    document = JSON.parse(decoder.decode(raw))
  } catch {
    throw new Error('peer document: invalid JSON')
  }
  if (!document || typeof document !== 'object' || Array.isArray(document)) throw new Error('peer document: invalid payload')
  return { raw, signature, document: document as T }
}

async function verifySignature(publicKey: Uint8Array, raw: Uint8Array, signature: Uint8Array): Promise<void> {
  let key: CryptoKey
  try {
    key = await crypto.subtle.importKey('raw', ownedCryptoBytes(publicKey), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify'])
  } catch {
    throw new Error('peer document: invalid public key')
  }
  if (!await crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, key, ownedCryptoBytes(signature), ownedCryptoBytes(raw))) {
    throw new Error('peer document: invalid signature')
  }
}

function validateScope(child: string[], parent: string[]): void {
  if (!Array.isArray(child) || child.some((id) => !isUUID(id)) || new Set(child).size !== child.length) {
    throw new Error('peer document: invalid session scope')
  }
  if (parent.length > 0 && child.some((id) => !parent.includes(id))) throw new Error('peer document: session scope broadened')
}

export async function verifyGenesis(token: string): Promise<VerifiedGenesis> {
  const parsed = parseSigned<SpaceGenesisDocument>('apg1', token)
  const doc = parsed.document as unknown as Record<string, unknown>
  exactKeys(doc, ['v', 'space_id', 'creator_peer_id', 'creator_public_key', 'created_at'])
  if (doc.v !== 1 || !isUUID(doc.space_id) || typeof doc.creator_peer_id !== 'string' || typeof doc.creator_public_key !== 'string' ||
      typeof doc.created_at !== 'number' || !Number.isSafeInteger(doc.created_at) || doc.created_at <= 0) {
    throw new Error('peer document: invalid genesis')
  }
  const publicKey = base64URLToBytes(doc.creator_public_key)
  if (await peerIDFromPublicKey(publicKey) !== doc.creator_peer_id) throw new Error('peer document: genesis peer mismatch')
  await verifySignature(publicKey, parsed.raw, parsed.signature)
  return {
    token,
    document: parsed.document,
    publicKey,
    hash: bytesToBase64URL(sha256(encoder.encode(token))),
  }
}

export async function verifyGrant(token: string, genesis: VerifiedGenesis, now = new Date(), depth = 0): Promise<VerifiedGrant> {
  if (depth > 8) throw new Error('peer document: membership chain too deep')
  const parsed = parseSigned<DeviceGrantDocument>('apm1', token)
  const doc = parsed.document as unknown as Record<string, unknown>
  exactKeys(doc, [
    'v', 'serial', 'space_id', 'space_genesis_hash', 'subject_peer_id', 'subject_public_key',
    'subject_wrapping_public_key', 'issuer_peer_id', 'issued_at', 'permission', 'allowed_session_ids',
    'can_invite', 'can_sync_secrets', 'delegation_depth',
  ], ['issuer_membership', 'expires_at'])
  const grant = parsed.document
  if (grant.v !== 1 || !isUUID(grant.serial) || grant.space_id !== genesis.document.space_id || grant.space_genesis_hash !== genesis.hash ||
      typeof grant.subject_peer_id !== 'string' || typeof grant.issuer_peer_id !== 'string' || !isPermission(grant.permission) ||
      !Number.isSafeInteger(grant.issued_at) || grant.issued_at <= 0 || grant.issued_at > Math.floor(now.getTime() / 1000) + CLOCK_SKEW_SECONDS ||
      !Number.isSafeInteger(grant.delegation_depth) || grant.delegation_depth < 0 || grant.delegation_depth > 8 ||
      typeof grant.can_invite !== 'boolean' || typeof grant.can_sync_secrets !== 'boolean') {
    throw new Error('peer document: invalid membership')
  }
  if (grant.expires_at !== undefined && (!Number.isSafeInteger(grant.expires_at) || grant.expires_at <= grant.issued_at || Math.floor(now.getTime() / 1000) >= grant.expires_at)) {
    throw new Error('peer document: membership expired')
  }
  const publicKey = base64URLToBytes(grant.subject_public_key)
  const wrappingPublicKey = base64URLToBytes(grant.subject_wrapping_public_key)
  if (wrappingPublicKey.length !== 65 || await peerIDFromPublicKey(publicKey) !== grant.subject_peer_id) throw new Error('peer document: membership key mismatch')
  validateScope(grant.allowed_session_ids, [])

  let issuerPublicKey = genesis.publicKey
  let issuerPermission: PeerPermission = 'full'
  let issuerCanInvite = true
  let issuerCanSyncSecrets = true
  let issuerScope: string[] = []
  let issuerDepth = -1
  if (grant.issuer_peer_id !== genesis.document.creator_peer_id) {
    if (!grant.issuer_membership) throw new Error('peer document: missing issuer membership')
    const issuer = await verifyGrant(grant.issuer_membership, genesis, now, depth + 1)
    if (issuer.document.subject_peer_id !== grant.issuer_peer_id) throw new Error('peer document: issuer mismatch')
    issuerPublicKey = issuer.publicKey
    issuerPermission = issuer.document.permission
    issuerCanInvite = issuer.document.can_invite
    issuerCanSyncSecrets = issuer.document.can_sync_secrets
    issuerScope = issuer.document.allowed_session_ids
    issuerDepth = issuer.document.delegation_depth
  }
  if (!issuerCanInvite || permissionRank(grant.permission) > permissionRank(issuerPermission) || grant.can_sync_secrets && !issuerCanSyncSecrets) {
    throw new Error('peer document: membership broadens capabilities')
  }
  if (issuerDepth >= 0 ? grant.delegation_depth !== issuerDepth + 1 : grant.issuer_peer_id !== grant.subject_peer_id && grant.delegation_depth !== 1) {
    throw new Error('peer document: invalid delegation depth')
  }
  validateScope(grant.allowed_session_ids, issuerScope)
  await verifySignature(issuerPublicKey, parsed.raw, parsed.signature)
  return { token, document: grant, publicKey, wrappingPublicKey }
}

async function verifyInvitation(token: string, genesis: VerifiedGenesis, now: Date): Promise<{ ticket: CapabilityTicketDocument; issuer: VerifiedGrant }> {
  const parsed = parseSigned<CapabilityTicketDocument>('atp1', token)
  const doc = parsed.document as unknown as Record<string, unknown>
  exactKeys(doc, [
    'v', 'invite_id', 'batch_id', 'space_id', 'redemption_peer_id', 'space_genesis_hash', 'issuer_peer_id',
    'issuer_membership', 'pairing_secret', 'issued_at', 'expires_at', 'max_uses', 'permission',
    'allowed_session_ids', 'can_invite', 'can_sync_secrets',
  ])
  const ticket = parsed.document
  const issuer = await verifyGrant(ticket.issuer_membership, genesis, now)
  if (ticket.v !== 1 || !isUUID(ticket.invite_id) || !isUUID(ticket.batch_id) || ticket.space_id !== genesis.document.space_id ||
      ticket.space_genesis_hash !== genesis.hash || ticket.redemption_peer_id !== ticket.issuer_peer_id ||
      ticket.issuer_peer_id !== issuer.document.subject_peer_id || ticket.max_uses !== 1 || !isPermission(ticket.permission) ||
      !Number.isSafeInteger(ticket.issued_at) || !Number.isSafeInteger(ticket.expires_at) ||
      ticket.issued_at <= 0 || ticket.issued_at > Math.floor(now.getTime() / 1000) + CLOCK_SKEW_SECONDS ||
      ticket.expires_at <= ticket.issued_at || Math.floor(now.getTime() / 1000) >= ticket.expires_at ||
      base64URLToBytes(ticket.pairing_secret).length !== 32 || permissionRank(ticket.permission) > permissionRank(issuer.document.permission) ||
      ticket.can_sync_secrets && !issuer.document.can_sync_secrets) {
    throw new Error('peer document: invalid invitation')
  }
  validateScope(ticket.allowed_session_ids, issuer.document.allowed_session_ids)
  await verifySignature(issuer.publicKey, parsed.raw, parsed.signature)
  return { ticket, issuer }
}

function validateRoute(route: ConnectionRouteDocument): void {
  if (!route || typeof route !== 'object' || Array.isArray(route)) throw new Error('peer document: invalid route')
  exactKeys(route as unknown as Record<string, unknown>, ['kind', 'url'], ['topic'])
  if (!['quick_tunnel', 'rendezvous', 'manual_lan'].includes(route.kind) || typeof route.url !== 'string' || route.url.length > 2048 ||
      route.topic !== undefined && typeof route.topic !== 'string') {
    throw new Error('peer document: invalid route')
  }
  const parsed = new URL(route.url)
  if (parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error('peer document: invalid route URL')
  if (route.kind === 'quick_tunnel' && (parsed.protocol !== 'https:' || !/^[a-z0-9-]+\.trycloudflare\.com$/i.test(parsed.hostname) || parsed.port || parsed.pathname !== '/')) {
    throw new Error('peer document: invalid Quick Tunnel route')
  }
  if (route.kind === 'manual_lan' && !['http:', 'https:'].includes(parsed.protocol)) throw new Error('peer document: invalid LAN route')
}

export async function verifyConnectionBundle(raw: string, now = new Date()): Promise<VerifiedConnectionBundle> {
  let token = raw.trim()
  if (!token.startsWith('atc1.')) {
    const parsed = new URL(token)
    if (parsed.search || !parsed.hash.startsWith('#atc1.')) throw new Error('peer document: invalid connection link')
    token = parsed.hash.slice(1).trim()
  }
  const parsed = parseSigned<ConnectionBundleDocument>('atc1', token)
  const fields = parsed.document as unknown as Record<string, unknown>
  exactKeys(fields, ['v', 'bundle_id', 'genesis', 'issuer_peer_id', 'issuer_membership', 'routes', 'created_at', 'expires_at'], ['ticket'])
  const bundle = parsed.document
  const genesis = await verifyGenesis(bundle.genesis)
  const issuer = await verifyGrant(bundle.issuer_membership, genesis, now)
  let ticket: CapabilityTicketDocument | undefined
  let maxExpiry = issuer.document.expires_at ?? Number.MAX_SAFE_INTEGER
  let minCreated = issuer.document.issued_at
  if (bundle.ticket) {
    const verified = await verifyInvitation(bundle.ticket, genesis, now)
    if (verified.issuer.token !== issuer.token || verified.ticket.redemption_peer_id !== bundle.issuer_peer_id) throw new Error('peer document: ticket issuer mismatch')
    ticket = verified.ticket
    maxExpiry = ticket.expires_at
    minCreated = Math.max(minCreated, ticket.issued_at)
  }
  if (bundle.v !== 1 || !isUUID(bundle.bundle_id) || bundle.issuer_peer_id !== issuer.document.subject_peer_id ||
      !Number.isSafeInteger(bundle.created_at) || !Number.isSafeInteger(bundle.expires_at) || bundle.created_at < minCreated ||
      bundle.created_at > Math.floor(now.getTime() / 1000) + CLOCK_SKEW_SECONDS || bundle.expires_at <= bundle.created_at ||
      bundle.expires_at > maxExpiry || Math.floor(now.getTime() / 1000) >= bundle.expires_at || !Array.isArray(bundle.routes) ||
      bundle.routes.length === 0 || bundle.routes.length > 8) {
    throw new Error('peer document: invalid connection bundle')
  }
  bundle.routes.forEach(validateRoute)
  await verifySignature(issuer.publicKey, parsed.raw, parsed.signature)
  const route = bundle.routes.find((candidate) => candidate.kind === 'quick_tunnel') ?? bundle.routes.find((candidate) => candidate.kind === 'manual_lan')
  if (!route) throw new Error('peer document: no client route')
  return { token, document: bundle, genesis, issuer, ticket, route }
}

export async function verifyMembershipForPeer(token: string, genesis: VerifiedGenesis, peerId: string, now = new Date()): Promise<VerifiedGrant> {
  const membership = await verifyGrant(token, genesis, now)
  if (membership.document.subject_peer_id !== peerId) throw new Error('peer document: membership subject mismatch')
  return membership
}

export async function verifyRevocation(token: string, genesis: VerifiedGenesis): Promise<VerifiedRevocation> {
  const parsed = parseSigned<RevocationDocument>('arv1', token)
  const fields = parsed.document as unknown as Record<string, unknown>
  exactKeys(fields, [
    'v', 'revocation_id', 'space_id', 'space_genesis_hash', 'kind', 'target_id',
    'actor_peer_id', 'actor_membership', 'created_at',
  ])
  const doc = parsed.document
  if (doc.v !== 1 || !isUUID(doc.revocation_id) || doc.space_id !== genesis.document.space_id ||
      doc.space_genesis_hash !== genesis.hash || !['member', 'grant', 'invitation_batch'].includes(doc.kind) ||
      !isDigest(doc.actor_peer_id) || typeof doc.actor_membership !== 'string' ||
      !Number.isSafeInteger(doc.created_at) || doc.created_at <= 0 ||
      (doc.kind === 'member' ? !isDigest(doc.target_id) : !isUUID(doc.target_id))) {
    throw new Error('peer document: invalid revocation')
  }
  const actor = await verifyGrant(doc.actor_membership, genesis, new Date(doc.created_at * 1000))
  const allowed = doc.kind === 'invitation_batch'
    ? actor.document.can_invite
    : actor.document.permission === 'full' && actor.document.can_invite
  if (!allowed || actor.document.subject_peer_id !== doc.actor_peer_id || actor.document.issued_at > doc.created_at) {
    throw new Error('peer document: revocation denied')
  }
  await verifySignature(actor.publicKey, parsed.raw, parsed.signature)
  return { token, document: doc, hash: bytesToBase64URL(sha256(encoder.encode(token))) }
}

export async function activeMemberships(
  tokens: string[],
  revocationTokens: string[],
  genesis: VerifiedGenesis,
  now = new Date(),
): Promise<VerifiedGrant[]> {
  const byPeer = new Map<string, VerifiedGrant>()
  const bySerial = new Map<string, string>()
  for (const token of new Set(tokens)) {
    const grant = await verifyGrant(token, genesis, now)
    const serialToken = bySerial.get(grant.document.serial)
    if (serialToken && serialToken !== token) throw new Error('peer document: membership serial fork')
    bySerial.set(grant.document.serial, token)
    const current = byPeer.get(grant.document.subject_peer_id)
    if (!current || grant.document.issued_at > current.document.issued_at ||
        grant.document.issued_at === current.document.issued_at && (grant.document.serial < current.document.serial ||
          grant.document.serial === current.document.serial && grant.token < current.token)) {
      byPeer.set(grant.document.subject_peer_id, grant)
    }
  }
  const revocationIDs = new Map<string, string>()
  const revokedMembers = new Set<string>()
  const revokedGrants = new Set<string>()
  for (const token of new Set(revocationTokens)) {
    const revocation = await verifyRevocation(token, genesis)
    const current = revocationIDs.get(revocation.document.revocation_id)
    if (current && current !== revocation.hash) throw new Error('peer document: revocation fork')
    revocationIDs.set(revocation.document.revocation_id, revocation.hash)
    if (revocation.document.created_at > Math.floor(now.getTime() / 1000)) continue
    if (revocation.document.kind === 'member') revokedMembers.add(revocation.document.target_id)
    if (revocation.document.kind === 'grant') revokedGrants.add(revocation.document.target_id)
  }
  return [...byPeer.values()]
    .filter((grant) => !revokedMembers.has(grant.document.subject_peer_id) && !revokedGrants.has(grant.document.serial))
    .sort((left, right) => left.document.subject_peer_id.localeCompare(right.document.subject_peer_id))
}
