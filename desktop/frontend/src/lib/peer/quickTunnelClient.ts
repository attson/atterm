import type { DirectTransport } from '../connection'
import type { DirectTransportDiagnostics } from '../directClient'
import { DirectFrameReassembler, fragmentDirectFrame } from '../directFraming'
import { DirectClientRecordCodec, type DirectAuthorization } from '../directHandshake'
import { DirectRecordKind, MAX_DIRECT_RECORD_PLAINTEXT } from '../directCrypto'
import type { NativeDirectClientOptions } from '../nativeDirectClient'
import { bytesToBase64URL, type PeerIdentity } from './identity'
import { PeerClientHandshake, peerPermission } from './handshake'
import { activeMemberships, verifyConnectionBundle, verifyGenesis, type VerifiedGenesis, type VerifiedGrant } from './documents'
import type { PeerClientState } from './clientState'

const SUBPROTOCOL = 'atterm-peer-v1'
const CONTROL_SESSION_ID = 'ffffffff-ffff-4fff-bfff-ffffffffffff'
const SIGNAL_LIMIT = 64 << 10
const CONFIG_LIMIT = 16 << 20
const BUFFERED_HIGH_WATER = 1024 * 1024
const encoder = new TextEncoder()
const decoder = new TextDecoder('utf-8', { fatal: true })

interface PeerContext {
  identity: PeerIdentity
  state: PeerClientState
  genesis: VerifiedGenesis
  localMembership: VerifiedGrant
  hostMembership: VerifiedGrant
  routeURL: string
}

interface SignalMessage {
  v: 1
  type: 'offer' | 'answer' | 'ice_candidate' | 'ice_end' | 'wss_fallback' | 'wss_ready'
  payload: string
}

interface ConfigFragmentState {
  messageId: bigint
  kind: DirectRecordKind
  total: number
  next: number
  startedAt: number
  buffer: Uint8Array
}

interface PeerQuickTunnelControlCallbacks {
  onConfigMessage?(kind: DirectRecordKind, payload: Uint8Array): void
}

function arrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const out = new ArrayBuffer(bytes.length)
  new Uint8Array(out).set(bytes)
  return out
}

function standardBase64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

function asBytes(value: unknown): Uint8Array {
  if (value instanceof ArrayBuffer) return new Uint8Array(value)
  if (value instanceof Blob) throw new Error('peer transport: Blob messages are not supported')
  throw new Error('peer transport: binary message required')
}

function websocketURL(base: string): string {
  const url = new URL(base)
  if (url.protocol === 'https:') url.protocol = 'wss:'
  else if (url.protocol === 'http:') url.protocol = 'ws:'
  else throw new Error('peer transport: invalid route scheme')
  url.pathname = '/peer/v1/connect'
  url.search = ''
  url.hash = ''
  return url.toString()
}

async function loadPeerContext(identity: PeerIdentity, state: PeerClientState): Promise<PeerContext> {
  const bundle = await verifyConnectionBundle(state.route_bundle)
  const genesis = await verifyGenesis(state.genesis)
  if (genesis.hash !== bundle.genesis.hash) throw new Error('peer transport: route belongs to another Peer Space')
  const active = await activeMemberships(state.memberships, state.revocations, genesis)
  const localMembership = active.find((grant) => grant.token === state.membership)
  const hostMembership = active.find((grant) => grant.token === bundle.document.issuer_membership)
  if (!localMembership || localMembership.document.subject_peer_id !== identity.peerId) throw new Error('peer transport: local membership mismatch')
  if (!hostMembership) throw new Error('peer transport: host membership is inactive')
  return { identity, state, genesis, localMembership, hostMembership, routeURL: bundle.route.url }
}

function signalPayload(type: SignalMessage['type'], payload = ''): Uint8Array {
  return encoder.encode(JSON.stringify({ v: 1, type, payload } satisfies SignalMessage))
}

export function fragmentPeerSignal(messageId: bigint, payload: Uint8Array): Uint8Array[] {
  if (payload.length <= MAX_DIRECT_RECORD_PLAINTEXT || payload.length > SIGNAL_LIMIT) {
    throw new Error('peer transport: signal fragmentation bounds')
  }
  const headerSize = 24
  const chunkSize = MAX_DIRECT_RECORD_PLAINTEXT - headerSize
  const records: Uint8Array[] = []
  for (let offset = 0; offset < payload.length; offset += chunkSize) {
    const end = Math.min(payload.length, offset + chunkSize)
    const out = new Uint8Array(headerSize + end - offset)
    out.set(encoder.encode('ASF1'))
    const view = new DataView(out.buffer)
    view.setBigUint64(4, messageId, false)
    view.setUint32(12, 0xfffffffe, false)
    view.setUint32(16, offset, false)
    view.setUint32(20, payload.length, false)
    out.set(payload.subarray(offset, end), headerSize)
    records.push(out)
  }
  return records
}

function parseSignal(value: Uint8Array): SignalMessage {
  if (value.length > SIGNAL_LIMIT) throw new Error('peer transport: signal too large')
  const parsed = JSON.parse(decoder.decode(value)) as Partial<SignalMessage>
  if (parsed.v !== 1 || !['offer', 'answer', 'ice_candidate', 'ice_end', 'wss_fallback', 'wss_ready'].includes(parsed.type ?? '') || typeof parsed.payload !== 'string') {
    throw new Error('peer transport: invalid signal')
  }
  return parsed as SignalMessage
}

function configFragment(kind: DirectRecordKind, messageId: bigint, payload: Uint8Array): Uint8Array[] {
  const headerSize = 25
  const chunkSize = MAX_DIRECT_RECORD_PLAINTEXT - headerSize
  const records: Uint8Array[] = []
  for (let offset = 0; offset < payload.length; offset += chunkSize) {
    const end = Math.min(payload.length, offset + chunkSize)
    const out = new Uint8Array(headerSize + end - offset)
    out.set(encoder.encode('ACF1'))
    const view = new DataView(out.buffer)
    view.setBigUint64(4, messageId, false)
    view.setUint32(12, 0xffffffff, false)
    out[16] = kind
    view.setUint32(17, offset, false)
    view.setUint32(21, payload.length, false)
    out.set(payload.subarray(offset, end), headerSize)
    records.push(out)
  }
  return records
}

/** Browser/WKWebView accountless transport. A `direct` instance prefers
 * WebRTC. A `quick_tunnel` instance activates the application-encrypted WSS
 * route only after the caller has obtained user consent. */
export class PeerQuickTunnelTransport implements DirectTransport {
  private ws: WebSocket | null = null
  private pc: RTCPeerConnection | null = null
  private dc: RTCDataChannel | null = null
  private signalHandshake: PeerClientHandshake | null = null
  private dataHandshake: PeerClientHandshake | null = null
  private signalCodec: DirectClientRecordCodec | null = null
  private dataCodec: DirectClientRecordCodec | null = null
  private authorization: DirectAuthorization | null = null
  private context: PeerContext | null = null
  private started = false
  private closed = false
  private authenticated = false
  private stage: 'authorization' | 'signal_handshake_host' | 'signal_handshake_ok' | 'signals' | 'data_handshake_host' | 'data_handshake_ok' | 'records' = 'authorization'
  private timeout: number | null = null
  private receiveChain = Promise.resolve()
  private signalFragment: { id: bigint; total: number; next: number; startedAt: number; buffer: Uint8Array } | null = null
  private configFragment: ConfigFragmentState | null = null
  private readonly frameReassembler = new DirectFrameReassembler()
  private nextMessageId = 1n
  private pendingTicket: Uint8Array | null = null
  private pendingAttemptId = ''

  constructor(
    private readonly options: NativeDirectClientOptions,
    private readonly identity: PeerIdentity,
    private readonly state: PeerClientState,
    private readonly control: PeerQuickTunnelControlCallbacks = {},
  ) {}

  start(): void {
    if (this.started || this.closed) return
    this.started = true
    void this.open().catch((error) => this.fail(error))
  }

  sendFrame(frame: Uint8Array): boolean {
    const codec = this.activeCodec()
    const target = this.options.route === 'quick_tunnel' ? this.ws : this.dc
    const open = target instanceof WebSocket ? target.readyState === WebSocket.OPEN : target?.readyState === 'open'
    if (!this.authenticated || !codec || !target || !open) return false
    if ('bufferedAmount' in target && target.bufferedAmount > BUFFERED_HIGH_WATER) {
      this.fail(new Error('peer transport: backpressure limit'))
      return false
    }
    try {
      for (const record of codec.sealFrame(frame)) target.send(arrayBuffer(record))
      return true
    } catch (error) {
      this.fail(error)
      return false
    }
  }

  close(): void { this.finish() }

  private async open(): Promise<void> {
    this.context = await loadPeerContext(this.identity, this.state)
    if (this.closed) return
    const ws = new WebSocket(websocketURL(this.context.routeURL), [SUBPROTOCOL])
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    this.timeout = window.setTimeout(() => this.fail(new Error('peer transport: timeout')), this.options.timeoutMs ?? 30_000)
    ws.onopen = () => {
      try {
        const ticket = crypto.getRandomValues(new Uint8Array(32))
        const attemptId = crypto.randomUUID()
        this.pendingTicket = ticket
        this.pendingAttemptId = attemptId
        ws.send(JSON.stringify({
          v: 1,
          kind: 'open',
          attempt_id: attemptId,
          ticket: standardBase64(ticket),
          session_id: this.options.sessionId,
          client_peer_id: this.identity.peerId,
          client_instance_id: this.options.clientInstanceId,
        }))
      } catch (error) { this.fail(error) }
    }
    ws.onmessage = (event) => {
      this.receiveChain = this.receiveChain.then(() => this.handleWSMessage(event.data)).catch((error) => this.fail(error))
    }
    ws.onerror = () => {}
    ws.onclose = () => { if (!this.closed) this.fail(new Error('peer transport: signaling endpoint unavailable')) }
  }

  private async handleWSMessage(data: unknown): Promise<void> {
    if (!this.context || !this.ws) throw new Error('peer transport: state unavailable')
    if (this.stage === 'authorization') {
      if (typeof data !== 'string') throw new Error('peer transport: authorization response must be text')
      const claims = JSON.parse(data) as Record<string, unknown>
      if (claims.v !== 1 || claims.kind !== 'authorized' || claims.user_id !== this.context.genesis.document.space_id ||
          claims.host_id !== this.context.hostMembership.document.subject_peer_id ||
          typeof claims.permission !== 'number' || !Number.isSafeInteger(claims.permission) ||
          typeof claims.expires_at_unix_millis !== 'number' || !Number.isSafeInteger(claims.expires_at_unix_millis) ||
          claims.expires_at_unix_millis <= Date.now() || !this.pendingTicket || !this.pendingAttemptId) {
        throw new Error('peer transport: authorization rejected')
      }
      const permission = peerPermission(claims.permission)
      const localAllowed = this.context.localMembership.document.allowed_session_ids
      const hostAllowed = this.context.hostMembership.document.allowed_session_ids
      const controlSession = this.options.sessionId === CONTROL_SESSION_ID
      if (!controlSession && (localAllowed.length > 0 && !localAllowed.includes(this.options.sessionId) ||
          hostAllowed.length > 0 && !hostAllowed.includes(this.options.sessionId)) ||
          permission > peerPermission(this.context.localMembership.document.permission) ||
          permission > peerPermission(this.context.hostMembership.document.permission)) {
        throw new Error('peer transport: authorization rejected')
      }
      this.authorization = {
        attemptId: this.pendingAttemptId,
        ticket: this.pendingTicket,
        sessionId: this.options.sessionId,
        userId: claims.user_id,
        hostId: claims.host_id,
        clientInstanceId: this.options.clientInstanceId,
        permission,
        expiresAtUnixMs: BigInt(claims.expires_at_unix_millis),
      }
      this.signalHandshake = await PeerClientHandshake.create(this.authorization, this.identity, this.context.genesis, this.context.localMembership, this.context.hostMembership)
      this.ws.send(arrayBuffer(this.signalHandshake.clientHello()))
      this.stage = 'signal_handshake_host'
      return
    }
    const bytes = asBytes(data)
    if (this.stage === 'signal_handshake_host') {
      this.ws.send(arrayBuffer(await this.signalHandshake!.handleHostHello(bytes)))
      this.stage = 'signal_handshake_ok'
      return
    }
    if (this.stage === 'signal_handshake_ok') {
      this.signalCodec = new DirectClientRecordCodec(this.signalHandshake!.handleAuthOK(bytes))
      this.stage = 'signals'
      if (this.options.route === 'quick_tunnel') this.sendSignal('wss_fallback')
      else await this.startWebRTC()
      return
    }
    if (!this.signalCodec) throw new Error('peer transport: signal codec unavailable')
    const opened = this.signalCodec.open(bytes)
    if (this.options.route === 'quick_tunnel' && this.authenticated) {
      this.handleRecord(opened.kind, opened.plaintext)
      return
    }
    if (opened.kind !== DirectRecordKind.Signal && opened.kind !== DirectRecordKind.SignalFragment) throw new Error('peer transport: unexpected signaling record')
    const payload = opened.kind === DirectRecordKind.Signal ? opened.plaintext : this.addSignalFragment(opened.plaintext)
    if (!payload) return
    const signal = parseSignal(payload)
    if (signal.type === 'wss_ready' && this.options.route === 'quick_tunnel') {
      this.authenticated = true
      this.stage = 'records'
      this.clearTimeout()
      this.options.callbacks.onDiagnostics?.({ route: 'quick_tunnel' })
      this.options.callbacks.onAuthenticated?.()
      return
    }
    await this.applySignal(signal)
  }

  private async startWebRTC(): Promise<void> {
    if (!this.authorization || !this.context || typeof RTCPeerConnection !== 'function') throw new Error('peer transport: WebRTC unavailable')
    const pc = new RTCPeerConnection({ iceServers: this.options.iceServers ?? [{ urls: ['stun:stun.cloudflare.com:3478'] }] })
    this.pc = pc
    pc.onicecandidate = (event) => {
      if (event.candidate) this.sendSignal('ice_candidate', JSON.stringify(event.candidate.toJSON()))
      else this.sendSignal('ice_end')
    }
    pc.oniceconnectionstatechange = () => {
      this.options.callbacks.onDiagnostics?.({ route: 'direct', iceState: pc.iceConnectionState })
      if (['failed', 'disconnected', 'closed'].includes(pc.iceConnectionState)) this.fail(new Error(`peer transport: ICE ${pc.iceConnectionState}`))
    }
    pc.onconnectionstatechange = () => {
      if (['failed', 'disconnected', 'closed'].includes(pc.connectionState)) this.fail(new Error(`peer transport: direct ${pc.connectionState}`))
    }
    const dc = pc.createDataChannel('atterm-terminal-v1', { ordered: true })
    dc.binaryType = 'arraybuffer'
    this.dc = dc
    this.dataHandshake = await PeerClientHandshake.create(this.authorization, this.identity, this.context.genesis, this.context.localMembership, this.context.hostMembership)
    dc.onopen = () => { try { dc.send(arrayBuffer(this.dataHandshake!.clientHello())); this.stage = 'data_handshake_host' } catch (error) { this.fail(error) } }
    dc.onmessage = (event) => { this.receiveChain = this.receiveChain.then(() => this.handleDataMessage(asBytes(event.data))).catch((error) => this.fail(error)) }
    dc.onerror = () => this.fail(new Error('peer transport: data channel error'))
    dc.onclose = () => { if (!this.closed) this.fail(new Error('peer transport: data channel closed')) }
    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    this.sendSignal('offer', JSON.stringify(pc.localDescription))
  }

  private async applySignal(signal: SignalMessage): Promise<void> {
    if (!this.pc) throw new Error('peer transport: WebRTC signal without peer connection')
    if (signal.type === 'answer') await this.pc.setRemoteDescription(JSON.parse(signal.payload) as RTCSessionDescriptionInit)
    else if (signal.type === 'ice_candidate') await this.pc.addIceCandidate(JSON.parse(signal.payload) as RTCIceCandidateInit)
    else if (signal.type !== 'ice_end') throw new Error(`peer transport: unexpected signal ${signal.type}`)
  }

  private async handleDataMessage(bytes: Uint8Array): Promise<void> {
    if (!this.dc || !this.dataHandshake) throw new Error('peer transport: data handshake unavailable')
    if (this.stage === 'data_handshake_host') {
      this.dc.send(arrayBuffer(await this.dataHandshake.handleHostHello(bytes)))
      this.stage = 'data_handshake_ok'
      return
    }
    if (this.stage === 'data_handshake_ok') {
      this.dataCodec = new DirectClientRecordCodec(this.dataHandshake.handleAuthOK(bytes))
      this.stage = 'records'
      this.authenticated = true
      this.clearTimeout()
      this.options.callbacks.onAuthenticated?.()
      return
    }
    if (!this.dataCodec) throw new Error('peer transport: data codec unavailable')
    const opened = this.dataCodec.open(bytes)
    this.handleRecord(opened.kind, opened.plaintext)
  }

  private handleRecord(kind: DirectRecordKind, plaintext: Uint8Array): void {
    if (kind === DirectRecordKind.ConfigFragment) {
      const complete = this.addConfigFragment(plaintext)
      if (complete) this.handleRecord(complete.kind, complete.payload)
    } else if (kind === DirectRecordKind.Frame) this.options.callbacks.onFrame(plaintext)
    else if (kind === DirectRecordKind.Fragment) {
      const frame = this.frameReassembler.add(plaintext)
      if (frame) this.options.callbacks.onFrame(frame)
    } else if (kind === DirectRecordKind.DirectReady) {
      if (plaintext.length !== 8) throw new Error('peer transport: invalid ready record')
      const seq = new DataView(plaintext.buffer, plaintext.byteOffset, plaintext.byteLength).getBigUint64(0, false)
      if (seq > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('peer transport: replay cursor out of range')
      this.options.callbacks.onReady(Number(seq))
    } else if (kind === DirectRecordKind.Ping) {
      const codec = this.activeCodec()
      const target = this.options.route === 'quick_tunnel' ? this.ws : this.dc
      if (!codec || !target) throw new Error('peer transport: pong route unavailable')
      target.send(arrayBuffer(codec.seal(DirectRecordKind.Pong, plaintext)))
    } else if (kind === DirectRecordKind.ConfigInventory || kind === DirectRecordKind.ConfigBatch ||
        kind === DirectRecordKind.ConfigAck || kind === DirectRecordKind.CatalogResponse) {
      if (!this.control.onConfigMessage) throw new Error(`peer transport: unexpected control record ${kind}`)
      this.control.onConfigMessage(kind, plaintext)
    } else if (kind === DirectRecordKind.Close) throw new Error('peer transport: host closed route')
  }

  private addConfigFragment(fragment: Uint8Array): { kind: DirectRecordKind; payload: Uint8Array } | null {
    if (fragment.length <= 25 || decoder.decode(fragment.subarray(0, 4)) !== 'ACF1') throw new Error('peer transport: invalid control fragment')
    const view = new DataView(fragment.buffer, fragment.byteOffset, fragment.byteLength)
    if (view.getUint32(12, false) !== 0xffffffff) throw new Error('peer transport: invalid control fragment namespace')
    const messageId = view.getBigUint64(4, false)
    const kind = fragment[16] as DirectRecordKind
    const offset = view.getUint32(17, false)
    const total = view.getUint32(21, false)
    const chunk = fragment.subarray(25)
    if (kind < DirectRecordKind.ConfigInventory || kind > DirectRecordKind.CatalogResponse || kind === DirectRecordKind.ConfigFragment ||
        total <= MAX_DIRECT_RECORD_PLAINTEXT || total > CONFIG_LIMIT || offset + chunk.length > total) {
      throw new Error('peer transport: invalid control fragment bounds')
    }
    if (!this.configFragment) {
      if (offset !== 0) throw new Error('peer transport: invalid first control fragment')
      this.configFragment = { messageId, kind, total, next: 0, startedAt: Date.now(), buffer: new Uint8Array(total) }
    }
    const state = this.configFragment
    if (Date.now() - state.startedAt > 10_000 || state.messageId !== messageId || state.kind !== kind || state.total !== total || state.next !== offset) {
      throw new Error('peer transport: control fragment ordering')
    }
    state.buffer.set(chunk, offset)
    state.next += chunk.length
    if (state.next !== state.total) return null
    this.configFragment = null
    return { kind: state.kind, payload: state.buffer }
  }

  private sendSignal(type: SignalMessage['type'], payload = ''): void {
    if (!this.ws || !this.signalCodec) throw new Error('peer transport: signaling unavailable')
    const data = signalPayload(type, payload)
    if (data.length <= MAX_DIRECT_RECORD_PLAINTEXT) this.ws.send(arrayBuffer(this.signalCodec.seal(DirectRecordKind.Signal, data)))
    else {
      if (data.length > SIGNAL_LIMIT) throw new Error('peer transport: oversized browser signal')
      for (const fragment of fragmentPeerSignal(this.nextMessageId++, data)) {
        this.ws.send(arrayBuffer(this.signalCodec.seal(DirectRecordKind.SignalFragment, fragment)))
      }
    }
  }

  private addSignalFragment(fragment: Uint8Array): Uint8Array | null {
    if (fragment.length <= 24 || decoder.decode(fragment.subarray(0, 4)) !== 'ASF1') throw new Error('peer transport: invalid signal fragment')
    const view = new DataView(fragment.buffer, fragment.byteOffset, fragment.byteLength)
    if (view.getUint32(12, false) !== 0xfffffffe) throw new Error('peer transport: invalid signal fragment namespace')
    const id = view.getBigUint64(4, false)
    const offset = view.getUint32(16, false)
    const total = view.getUint32(20, false)
    const chunk = fragment.subarray(24)
    if (total <= MAX_DIRECT_RECORD_PLAINTEXT || total > SIGNAL_LIMIT || offset + chunk.length > total) throw new Error('peer transport: invalid signal fragment bounds')
    if (!this.signalFragment) {
      if (offset !== 0) throw new Error('peer transport: invalid first signal fragment')
      this.signalFragment = { id, total, next: 0, startedAt: Date.now(), buffer: new Uint8Array(total) }
    }
    const state = this.signalFragment
    if (Date.now() - state.startedAt > 10_000 || state.id !== id || state.total !== total || state.next !== offset) throw new Error('peer transport: signal fragment ordering')
    state.buffer.set(chunk, offset)
    state.next += chunk.length
    if (state.next !== state.total) return null
    this.signalFragment = null
    return state.buffer
  }

  private activeCodec(): DirectClientRecordCodec | null { return this.options.route === 'quick_tunnel' ? this.signalCodec : this.dataCodec }
  private clearTimeout(): void { if (this.timeout !== null) window.clearTimeout(this.timeout); this.timeout = null }
  private fail(value: unknown): void { if (!this.closed) { const error = value instanceof Error ? value : new Error(String(value)); this.finish(); this.options.callbacks.onFailure(error) } }
  private finish(): void {
    if (this.closed) return
    this.closed = true
    this.authenticated = false
    this.clearTimeout()
    this.signalHandshake?.dispose()
    this.dataHandshake?.dispose()
    try { this.dc?.close() } catch {}
    try { this.pc?.close() } catch {}
    try { this.ws?.close() } catch {}
    this.dc = null
    this.pc = null
    this.ws = null
  }
}

export async function listQuickTunnelPeerSessions(identity: PeerIdentity, state: PeerClientState): Promise<unknown[]> {
  const context = await loadPeerContext(identity, state)
  return new Promise<unknown[]>((resolve, reject) => {
    let resolved = false
    let transport: PeerQuickTunnelTransport
    const finish = (fn: () => void) => { if (!resolved) { resolved = true; transport.close(); fn() } }
    const options: NativeDirectClientOptions = {
      signalURL: '', sessionId: CONTROL_SESSION_ID, sinceSeq: 0, clientInstanceId: crypto.randomUUID(), route: 'quick_tunnel',
      callbacks: {
        onFrame: () => {}, onReady: () => {},
        onAuthenticated: () => {
          const internals = transport as unknown as { signalCodec: DirectClientRecordCodec; ws: WebSocket; nextMessageId: bigint }
          const payload = encoder.encode(JSON.stringify({ v: 1 }))
          const records = payload.length <= MAX_DIRECT_RECORD_PLAINTEXT
            ? [internals.signalCodec.seal(DirectRecordKind.CatalogRequest, payload)]
            : configFragment(DirectRecordKind.CatalogRequest, internals.nextMessageId++, payload).map((part) => internals.signalCodec.seal(DirectRecordKind.ConfigFragment, part))
          for (const record of records) internals.ws.send(arrayBuffer(record))
        },
        onFailure: (error) => finish(() => reject(error)),
      },
    }
    transport = new PeerQuickTunnelTransport(options, context.identity, context.state, {
      onConfigMessage(kind, plaintext) {
        if (kind !== DirectRecordKind.CatalogResponse) return finish(() => reject(new Error('peer catalog: unexpected control response')))
        const body = JSON.parse(decoder.decode(plaintext)) as { v?: number; sessions?: unknown[] }
        if (body.v !== 1 || !Array.isArray(body.sessions)) return finish(() => reject(new Error('peer catalog: invalid response')))
        finish(() => resolve(body.sessions!))
      },
    })
    transport.start()
    window.setTimeout(() => finish(() => reject(new Error('peer catalog: timeout'))), 8_000)
  })
}

export function quickTunnelRouteAvailable(state: PeerClientState): boolean {
  return state.route_bundle.startsWith('atc1.') || state.route_bundle.includes('#atc1.')
}

export { CONTROL_SESSION_ID, loadPeerContext }
