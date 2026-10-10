import type { DirectTransport } from '../connection'
import type { NativeDirectClientOptions } from '../nativeDirectClient'
import type {
  PeerBridge,
  PeerConfigSyncStatus,
  PeerLANConfig,
  PeerRendezvousConfig,
  PeerRendezvousStatus,
  PeerRouteImportResult,
  RemoteSession,
} from '../../platform/types'
import type { PeerIdentity } from './identity'
import {
  joinPeerSpace,
  peerClientMembers,
  peerClientStatus,
  previewPeerConnection,
  type PeerClientState,
  type PeerClientStateStore,
} from './clientState'
import { activeMemberships, verifyConnectionBundle, verifyGenesis } from './documents'
import { listQuickTunnelPeerSessions, PeerQuickTunnelTransport } from './quickTunnelClient'

export interface PeerClientBridgeOptions {
  loadIdentity(): Promise<PeerIdentity>
  store: PeerClientStateStore
}

class LazyPeerTransport implements DirectTransport {
  private delegate: DirectTransport | null = null
  private closed = false

  constructor(
    private readonly options: NativeDirectClientOptions,
    private readonly resolve: () => Promise<{ identity: PeerIdentity; state: PeerClientState }>,
  ) {}

  start(): void {
    void this.resolve().then(({ identity, state }) => {
      if (this.closed) return
      const delegate = new PeerQuickTunnelTransport(this.options, identity, state)
      this.delegate = delegate
      delegate.start()
    }).catch((error) => this.options.callbacks.onFailure(error instanceof Error ? error : new Error(String(error))))
  }

  sendFrame(frame: Uint8Array): boolean {
    if (this.closed) return false
    if (this.delegate) return this.delegate.sendFrame(frame)
    // PeerSessionConnection owns the pre-ready queue and releases it only
    // after DIRECT_READY. Reporting success here would silently drop input
    // while identity/state loading or membership authentication is pending.
    return false
  }

  close(): void {
    this.closed = true
    this.delegate?.close()
  }
}

const emptySyncStatus = (configured: boolean): PeerConfigSyncStatus => ({
  configured,
  local_operations: 0,
  pending_operations: 0,
  replica_devices: configured ? 1 : 0,
  active_remote_members: 0,
  acknowledging_peers: 0,
  pending_import_records: 0,
})

function unsupported(): never {
  throw new Error('peer client capability unavailable on this device')
}

function sessionRow(value: unknown): RemoteSession | null {
  if (!value || typeof value !== 'object') return null
  const row = value as Record<string, unknown>
  const id = typeof row.id === 'string' ? row.id : ''
  if (!id) return null
  return {
    session_id: id,
    host_id: typeof row.host_id === 'string' ? row.host_id : '',
    host: typeof row.host === 'string' ? row.host : '',
    user: typeof row.user === 'string' ? row.user : '',
    title: typeof row.title === 'string' ? row.title : '',
    cwd: typeof row.cwd === 'string' ? row.cwd : '',
    cols: typeof row.cols === 'number' ? row.cols : 80,
    rows: typeof row.rows === 'number' ? row.rows : 24,
    started_at: typeof row.started_at === 'number' ? row.started_at : undefined,
    remote_permission: typeof row.permission === 'number'
      ? row.permission === 1 ? 'view' : row.permission === 2 ? 'control' : 'full'
      : typeof row.permission === 'string' ? row.permission : 'view',
    task_state: typeof row.task_state === 'string' ? row.task_state as RemoteSession['task_state'] : undefined,
    current_command: typeof row.current_command === 'string' ? row.current_command : undefined,
    command_started_at: typeof row.command_started_at === 'number' ? row.command_started_at : undefined,
    command_ended_at: typeof row.command_ended_at === 'number' ? row.command_ended_at : undefined,
    command_duration_ms: typeof row.command_duration_ms === 'number' ? row.command_duration_ms : undefined,
    command_exit_code: typeof row.command_exit_code === 'number' ? row.command_exit_code : undefined,
    last_output_at: typeof row.last_output_at === 'number' ? row.last_output_at : undefined,
    type: typeof row.type === 'string' ? row.type : undefined,
    attention_at: typeof row.attention_at === 'number' ? row.attention_at : undefined,
    peer_direct: true,
  }
}

export function createPeerClientBridge(options: PeerClientBridgeOptions): PeerBridge {
  let identityCache: PeerIdentity | null = null
  let stateCache: PeerClientState | null | undefined
  const identity = async () => identityCache ??= await options.loadIdentity()
  const state = async () => {
    if (stateCache === undefined) stateCache = await options.store.load()
    return stateCache
  }
  const configured = async () => {
    const [resolvedIdentity, resolvedState] = await Promise.all([identity(), state()])
    if (!resolvedState) throw new Error('Peer Space is not configured')
    return { identity: resolvedIdentity, state: resolvedState }
  }

  const disabledRendezvous = (): PeerRendezvousConfig => ({
    mode: 'disabled', url: '', websocket_url: '', health_url: '', stun_mode: 'default',
    stun_urls: ['stun:stun.cloudflare.com:3478'], turn_enabled: false, turn_urls: [],
    turn_username: '', turn_credential_configured: false,
  })

  return {
    async status() { return peerClientStatus(await identity(), await state()) },
    async configSyncStatus() { return emptySyncStatus(Boolean(await state())) },
    async acceptPendingConfig() { return emptySyncStatus(Boolean(await state())) },
    async discardPendingConfig() { return emptySyncStatus(Boolean(await state())) },
    async createSpace() { return unsupported() },
    async previewConnectionBundle(raw) { return (await previewPeerConnection(raw)).preview },
    async joinSpace(req) {
      if (await state()) throw new Error('Peer Space already exists')
      const next = await joinPeerSpace(await identity(), req.connection_bundle, req.expected_fingerprint)
      await options.store.save(next)
      stateCache = next
      return peerClientStatus(await identity(), next)
    },
    async importConnectionBundle(raw): Promise<PeerRouteImportResult> {
      const current = await configured()
      const bundle = await verifyConnectionBundle(raw)
      if (bundle.ticket) throw new Error('Peer route import rejected')
      const genesis = await verifyGenesis(current.state.genesis)
      if (bundle.genesis.hash !== genesis.hash) throw new Error('Peer route import rejected')
      const active = await activeMemberships(current.state.memberships, current.state.revocations, genesis)
      if (!active.some((grant) => grant.token === bundle.document.issuer_membership)) throw new Error('Peer route import rejected')
      const next = { ...current.state, route_bundle: bundle.token }
      await options.store.save(next)
      stateCache = next
      return {
        issuer_peer_id: bundle.document.issuer_peer_id,
        quick_tunnel: bundle.route.kind === 'quick_tunnel',
        manual_lan: bundle.route.kind === 'manual_lan',
        expires_at: bundle.document.expires_at,
      }
    },
    async createInvitations() { return unsupported() },
    async listInvitations() { return [] },
    async revokeInvitation() { return unsupported() },
    async revokeInvitationBatch() { return unsupported() },
    async listMembers() { return peerClientMembers((await configured()).state) },
    async revokeMember() { return unsupported() },
    async getRendezvousConfig() { return disabledRendezvous() },
    async setRendezvousConfig() { return unsupported() },
    async getRendezvousStatus(): Promise<PeerRendezvousStatus> { return { mode: 'disabled', state: 'disabled', reachable_peers: 0 } },
    async reconnectRendezvous(): Promise<PeerRendezvousStatus> { return { mode: 'disabled', state: 'disabled', reachable_peers: 0 } },
    async getLANConfig(): Promise<PeerLANConfig> {
      return { lan_only: false, enabled: false, auto_discovery: false, advertise_host: '', port: 8484, routes: [], running: false, discovery_running: false } as unknown as PeerLANConfig
    },
    async setLANConfig() { return unsupported() },
    async syncConfigNow() { return emptySyncStatus(Boolean(await state())) },
    async listSessions() {
      const current = await configured()
      return (await listQuickTunnelPeerSessions(current.identity, current.state)).map(sessionRow).filter((row): row is RemoteSession => row !== null)
    },
    async getSessionRouteStatus() {
      const current = await configured()
      return { direct: typeof RTCPeerConnection === 'function', lan: false, quick_tunnel: current.state.fallback_consent }
    },
    createSessionTransport: (transportOptions) => new LazyPeerTransport(transportOptions, configured),
    async getQuickTunnelFallbackConsent() { return (await state())?.fallback_consent ?? false },
    async setQuickTunnelFallbackConsent(allowed) {
      const current = await configured()
      const next = { ...current.state, fallback_consent: allowed }
      await options.store.save(next)
      stateCache = next
    },
  }
}
