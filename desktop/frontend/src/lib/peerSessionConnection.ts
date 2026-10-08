import {
  decodeFrame,
  decodeOutPayload,
  decodeText,
  encodeFrame,
  encodeResize,
  encodeText,
  TYPE,
  uuidParse,
  type Frame,
} from './proto'
import type {
  ClosePayload,
  ConnectionHandlers,
  DirectFallbackReason,
  DirectTransport,
  FSEvent,
  FSRequest,
  FSResponse,
  ServiceOpenResult,
} from './connection'
import {
  DEFAULT_FS_REQUEST_TIMEOUT_MS,
  directFallbackReason,
  encodePastePayload,
  isFSResponse,
  pastePayloadSizeBlockReason,
} from './connection'
import { decodeSegments, encodeSegments } from './fsSegments'
import { errText, logWarn } from './log'
import type { DirectTransportDiagnostics } from './directClient'
import type { NativeDirectClientOptions, NativePeerRoute } from './nativeDirectClient'

export interface PeerSessionConnectionOptions {
  clientName?: string
  route?: NativePeerRoute
  transportFactory: (options: NativeDirectClientOptions) => DirectTransport
  /** Revalidates a Go-owned signed route hint without exposing its endpoint. */
  resolveQuickTunnelFallback?: () => Promise<boolean>
  /** Revalidates that Rendezvous still exposes this authenticated session. */
  resolveDirectFailback?: () => Promise<boolean>
  directFailbackCooldownMs?: number
}

interface SessionCreatedResponse {
  request_id: string
  ok: boolean
  session_id?: string
  error?: string
}

interface ServiceOpenedResponse {
  request_id: string
  service_id: string
  ok: boolean
  error?: string
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

interface PeerHandoverAttempt {
  generation: number
  transport: DirectTransport
  authenticated: boolean
  diagnostics: DirectTransportDiagnostics | null
  startedAt: number
  promoted: boolean
}

const defaultDirectFailbackCooldownMs = 30_000
const maxDirectFailbackCooldownMs = 5 * 60_000

// Reachability failures may change transport; trust/integrity failures must
// fail on the current route instead of being hidden by a fallback.
function allowsQuickTunnelFallback(reason: DirectFallbackReason): boolean {
  return reason === 'signal_endpoint_unavailable' ||
    reason === 'webrtc_unavailable' ||
    reason === 'timeout' ||
    reason === 'host_unavailable' ||
    reason === 'ice_failed' ||
    reason === 'direct_disconnected' ||
    reason === 'transport_error'
}

/** Relay-independent terminal connection for accountless Peer sessions. It
 * keeps the Relay-oriented SessionConnection untouched and uses only native
 * Peer transports. */
export class PeerSessionConnection {
  private readonly sidBytes: Uint8Array
  private readonly clientID = crypto.randomUUID()
  private readonly clientName: string
  private transport: DirectTransport | null = null
  private generation = 0
  private lastSeq = 0
  private authenticated = false
  private ready = false
  private detached = false
  private suspended = false
  private reconnectAttempts = 0
  private reconnectTimer: number | null = null
  private fallbackPending = false
  private handover: PeerHandoverAttempt | null = null
  private failbackTimer: number | null = null
  private failbackProbe: object | null = null
  private failbackAttempts = 0
  private pendingInputs: string[] = []
  private pendingResize: { cols: number; rows: number } | null = null
  private pendingDriverClaim = false
  private pendingFSRequests = new Map<string, {
    generation: number
    resolve: (response: FSResponse) => void
    reject: (error: Error) => void
    timer: number
  }>()
  private retiredFSRequestIDs = new Set<string>()
  private fsEventHandlers = new Set<(event: FSEvent) => void>()
  private pendingSessionCreates = new Map<string, {
    generation: number
    resolve: (sessionID: string) => void
    reject: (error: Error) => void
    timer: number
  }>()
  private pendingServiceOpens = new Map<string, {
    generation: number
    serviceId: string
    peerAttemptId: string
    resolve: (result: ServiceOpenResult) => void
    reject: (error: Error) => void
    timer: number
  }>()
  private currentDriverClientID = ''
  private diagnostics: DirectTransportDiagnostics | null = null
  private lastFailureReason: DirectFallbackReason | undefined
  private startedAt = 0
  private route: NativePeerRoute

  constructor(
    private readonly sessionID: string,
    private readonly handlers: ConnectionHandlers,
    private readonly options: PeerSessionConnectionOptions,
  ) {
    this.sidBytes = uuidParse(sessionID)
    this.clientName = options.clientName?.trim() || 'desktop'
    this.route = options.route ?? 'direct'
  }

  attach(): void {
    if (this.detached) return
    this.suspended = false
    if (this.transport || this.reconnectTimer !== null || this.fallbackPending) return
    this.start()
  }

  suspend(): void {
    if (this.detached) return
    this.suspended = true
    this.clearReconnect()
    this.closeTransport()
  }

  detach(): void {
    this.detached = true
    this.suspended = false
    this.clearReconnect()
    this.closeTransport()
  }

  setPreferDirect(_enabled: boolean): void {}

  setRoute(route: NativePeerRoute): void {
    if (this.detached) return
    if (route === this.route) {
      if (route === 'quick_tunnel' && this.handover) this.cancelHandover(this.handover)
      return
    }
    this.route = route
    this.clearReconnect()
    this.closeTransport()
    if (!this.suspended) this.start()
  }

  sendInput(value: string): void {
    if (!this.send(encodeFrame(TYPE.IN, this.sidBytes, encodeText(value)))) {
      this.pendingInputs.push(value)
    }
  }

  sendResize(cols: number, rows: number): void {
    if (this.send(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)))) {
      this.pendingResize = null
    } else {
      this.pendingResize = { cols, rows }
    }
  }

  claimDriver(): boolean {
    const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }))
    if (this.send(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload))) {
      this.pendingDriverClaim = false
      return true
    }
    this.pendingDriverClaim = true
    return false
  }

  openService(port: number, host = 'localhost', timeoutMs = 30_000): Promise<ServiceOpenResult> {
    const transport = this.transport
    const peerAttemptId = transport?.nativeAttemptId?.() ?? ''
    if (!this.ready || !transport || this.handover || !peerAttemptId) {
      return Promise.reject(new Error('service preview failed: Peer route is not ready'))
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return Promise.reject(new Error('service preview failed: invalid port'))
    }
    if (!['localhost', '127.0.0.1', '::1'].includes(host)) {
      return Promise.reject(new Error('service preview failed: target must be loopback'))
    }
    const requestId = crypto.randomUUID()
    const serviceId = crypto.randomUUID()
    const generation = this.generation
    const payload = encodeText(JSON.stringify({
      request_id: requestId,
      service_id: serviceId,
      peer_fields: { port, scheme: 'http', host },
    }))
    return new Promise<ServiceOpenResult>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pendingServiceOpens.delete(requestId)
        reject(new Error('service preview timed out'))
      }, timeoutMs)
      this.pendingServiceOpens.set(requestId, {
        generation, serviceId, peerAttemptId, resolve, reject, timer,
      })
      try {
        if (!this.isCurrent(generation, transport) || this.handover ||
          !transport.sendFrame(encodeFrame(TYPE.SERVICE_OPEN, this.sidBytes, payload))) {
          throw new Error('service preview failed: Peer route changed')
        }
      } catch (value) {
        window.clearTimeout(timer)
        this.pendingServiceOpens.delete(requestId)
        reject(value instanceof Error ? value : new Error(String(value)))
      }
    })
  }

  closeService(serviceId: string): void {
    if (!UUID_PATTERN.test(serviceId)) return
    this.send(encodeFrame(
      TYPE.SERVICE_CLOSE,
      this.sidBytes,
      encodeText(JSON.stringify({ service_id: serviceId })),
    ))
  }

  createSessionWithProfile(hostID: string, profileID: string, timeoutMs = 30_000): Promise<string> {
    const transport = this.transport
    const targetHostID = hostID.trim()
    const targetProfileID = profileID.trim()
    if (!this.ready || !transport || this.handover) {
      return Promise.reject(new Error('upstream_unavailable'))
    }
    if (!targetHostID || targetHostID.length > 256 || !targetProfileID || targetProfileID.length > 256) {
      return Promise.reject(new Error('invalid_request'))
    }
    const requestID = `sc-${crypto.randomUUID()}`
    const generation = this.generation
    const payload = encodeText(JSON.stringify({
      request_id: requestID,
      host_id: targetHostID,
      profile_id: targetProfileID,
    }))
    return new Promise<string>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pendingSessionCreates.delete(requestID)
        reject(new Error('timeout'))
      }, timeoutMs)
      this.pendingSessionCreates.set(requestID, { generation, resolve, reject, timer })
      try {
        if (!this.isCurrent(generation, transport) || this.handover ||
          !transport.sendFrame(encodeFrame(TYPE.SESSION_CREATE, this.sidBytes, payload))) {
          throw new Error('upstream_unavailable')
        }
      } catch (value) {
        window.clearTimeout(timer)
        this.pendingSessionCreates.delete(requestID)
        reject(value instanceof Error ? value : new Error(String(value)))
      }
    })
  }

  onFSEvent(handler: (event: FSEvent) => void): () => void {
    this.fsEventHandlers.add(handler)
    return () => this.fsEventHandlers.delete(handler)
  }

  sendFSRequest(request: FSRequest, timeoutMs = DEFAULT_FS_REQUEST_TIMEOUT_MS): Promise<FSResponse> {
    const transport = this.transport
    if (!this.ready || !transport || this.handover) {
      return Promise.reject(new Error('filesystem request failed: Peer route is not ready'))
    }
    const requestID = request.request_id || this.newUniqueFSRequestID()
    if (this.pendingFSRequests.has(requestID)) {
      return Promise.reject(new Error(`duplicate filesystem request_id: ${requestID}`))
    }
    if (this.retiredFSRequestIDs.has(requestID)) {
      return Promise.reject(new Error(`retired filesystem request_id after timeout: ${requestID}`))
    }
    const generation = this.generation
    const payload: FSRequest = { ...request, request_id: requestID }
    const encoded = encodeSegments([encodeText(JSON.stringify(payload))])
    return new Promise<FSResponse>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pendingFSRequests.delete(requestID)
        this.retiredFSRequestIDs.add(requestID)
        reject(new Error(`filesystem request timed out: ${requestID}`))
      }, timeoutMs)
      this.pendingFSRequests.set(requestID, { generation, resolve, reject, timer })
      try {
        if (!this.isCurrent(generation, transport) || this.handover ||
          !transport.sendFrame(encodeFrame(TYPE.FS_REQUEST, this.sidBytes, encoded))) {
          throw new Error('Peer filesystem request send failed')
        }
      } catch (value) {
        window.clearTimeout(timer)
        this.pendingFSRequests.delete(requestID)
        reject(value instanceof Error ? value : new Error(String(value)))
      }
    })
  }

  async sendPasteImage(blob: Blob, filename = 'clipboard-image'): Promise<boolean> {
    return this.sendPaste(blob, filename, 'image/png', TYPE.PASTE_IMAGE)
  }

  async sendPasteFile(blob: Blob, filename: string): Promise<boolean> {
    return this.sendPaste(blob, filename, 'application/octet-stream', TYPE.PASTE_FILE)
  }

  private async sendPaste(blob: Blob, filename: string, fallbackContentType: string, type: typeof TYPE.PASTE_IMAGE | typeof TYPE.PASTE_FILE): Promise<boolean> {
    const blocked = pastePayloadSizeBlockReason(blob.size)
    if (blocked) throw new Error(blocked)
    if (!this.ready || !this.transport || this.handover) {
      throw new Error('Peer route is not ready')
    }
    const generation = this.generation
    const transport = this.transport
    const payload = await encodePastePayload(blob, filename, fallbackContentType)
    if (!this.isCurrent(generation, transport) || !this.ready || this.handover || !transport.sendFrame(encodeFrame(type, this.sidBytes, payload))) {
      throw new Error('Peer paste send failed')
    }
    return true
  }

  private start(): void {
    const generation = ++this.generation
    this.fallbackPending = false
    this.authenticated = false
    this.ready = false
    this.diagnostics = null
    this.startedAt = performance.now()
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting')
    this.emitRoute(this.connectingRoute())
    let transport: DirectTransport
    try {
      transport = this.options.transportFactory({
        signalURL: '',
        sessionId: this.sessionID,
        sinceSeq: this.lastSeq,
        clientInstanceId: this.clientID,
        route: this.route,
        callbacks: {
          onAuthenticated: () => {
            if (!this.isCurrent(generation, transport)) return
            this.authenticated = true
          },
          onFrame: (bytes) => {
            if (!this.isCurrent(generation, transport)) return
            this.handleFrame(decodeFrame(bytes))
          },
          onReady: (replayedSeq) => {
            if (!this.isCurrent(generation, transport) || !this.authenticated || replayedSeq < this.lastSeq) {
              this.fail(generation, transport, new Error('invalid Peer replay cursor'))
              return
            }
            this.lastSeq = replayedSeq
            this.ready = true
            this.reconnectAttempts = 0
            this.lastFailureReason = undefined
            this.handlers.onStatus?.('attached')
            this.emitRoute(this.connectedRoute())
            this.flush()
            if (this.route === 'quick_tunnel') this.scheduleDirectFailback()
          },
          onDiagnostics: (diagnostics) => {
            if (!this.isCurrent(generation, transport)) return
            this.diagnostics = diagnostics
            this.emitRoute(this.ready ? this.connectedRoute() : this.connectingRoute())
          },
          onFailure: (error) => this.fail(generation, transport, error),
        },
      })
      this.transport = transport
      transport.start()
    } catch (value) {
      this.transport = null
      this.recover(generation, value instanceof Error ? value : new Error(String(value)))
    }
  }

  private handleFrame(frame: Frame): void {
    if (frame.type === TYPE.SESSION_CREATED) {
      this.handleSessionCreated(frame)
      return
    }
    if (!this.sameSession(frame.sid)) throw new Error('Peer frame session mismatch')
    if (frame.type === TYPE.SERVICE_OPENED) {
      this.handleServiceOpened(frame.payload)
      return
    }
    if (frame.type === TYPE.OUT) {
      const { seq, data } = decodeOutPayload(frame.payload)
      if (seq > 0 && seq <= this.lastSeq) return
      if (seq > 0) this.lastSeq = seq
      this.handlers.onOutput?.(data)
      return
    }
    if (frame.type === TYPE.META) {
      const meta = JSON.parse(decodeText(frame.payload)) as Record<string, unknown>
      this.handlers.onMeta?.(meta)
      const driverID = String(meta.driver_client_id ?? '')
      const driverName = String(meta.driver_client_name ?? '')
      if (driverID !== this.currentDriverClientID) {
        this.currentDriverClientID = driverID
        this.handlers.onDriverChange?.(driverID, driverID !== '' && driverID === this.clientID, driverName)
      }
      return
    }
    if (frame.type === TYPE.CLOSE) {
      let close: ClosePayload = { exit_code: 0 }
      try { close = JSON.parse(decodeText(frame.payload)) as ClosePayload } catch { /* default close */ }
      this.handlers.onClose?.(close)
      this.handlers.onStatus?.('ended')
      this.detach()
      return
    }
    if (frame.type === TYPE.FS_RESPONSE) {
      this.handleFSResponse(frame.payload)
      return
    }
    if (frame.type === TYPE.FS_EVENT) {
      this.handleFSEvent(frame.payload)
      return
    }
    throw new Error(`unsupported Peer frame type 0x${frame.type.toString(16).padStart(2, '0')}`)
  }

  private send(frame: Uint8Array): boolean {
    return Boolean(!this.handover && this.ready && this.transport?.sendFrame(frame))
  }

  private handleFSResponse(payload: Uint8Array): void {
    const segments = decodeSegments(payload)
    if (!segments || segments.length !== 1) return
    let response: FSResponse
    try {
      const parsed = JSON.parse(decodeText(segments[0]))
      if (!isFSResponse(parsed)) return
      response = parsed
    } catch {
      return
    }
    const pending = this.pendingFSRequests.get(response.request_id)
    if (!pending || pending.generation !== this.generation) return
    window.clearTimeout(pending.timer)
    this.pendingFSRequests.delete(response.request_id)
    pending.resolve(response)
  }

  private handleFSEvent(payload: Uint8Array): void {
    const segments = decodeSegments(payload)
    if (!segments || segments.length !== 1) return
    let event: FSEvent
    try {
      const parsed = JSON.parse(decodeText(segments[0])) as Partial<FSEvent>
      if (!parsed || typeof parsed.watch_id !== 'string' || typeof parsed.path !== 'string' ||
        typeof parsed.event !== 'string') return
      event = parsed as FSEvent
    } catch {
      return
    }
    for (const handler of this.fsEventHandlers) {
      try {
        handler(event)
      } catch (error) {
        logWarn('peer', 'fs event handler threw', { error: errText(error) })
      }
    }
  }

  private newUniqueFSRequestID(): string {
    for (let index = 0; index < 5; index++) {
      const requestID = `fs-${crypto.randomUUID()}`
      if (!this.pendingFSRequests.has(requestID) && !this.retiredFSRequestIDs.has(requestID)) return requestID
    }
    return `fs-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  }

  private rejectPendingFSRequests(error: Error): void {
    const pending = Array.from(this.pendingFSRequests.values())
    this.pendingFSRequests.clear()
    for (const request of pending) {
      window.clearTimeout(request.timer)
      request.reject(error)
    }
  }

  private handleSessionCreated(frame: Frame): void {
    let response: SessionCreatedResponse
    try {
      const parsed = JSON.parse(decodeText(frame.payload)) as Partial<SessionCreatedResponse>
      if (!parsed || typeof parsed.request_id !== 'string' || typeof parsed.ok !== 'boolean' ||
        parsed.session_id !== undefined && typeof parsed.session_id !== 'string' ||
        parsed.error !== undefined && typeof parsed.error !== 'string') return
      response = parsed as SessionCreatedResponse
    } catch {
      return
    }
    const pending = this.pendingSessionCreates.get(response.request_id)
    if (!pending || pending.generation !== this.generation) return
    if (response.ok) {
      if (!response.session_id || !UUID_PATTERN.test(response.session_id)) return
      let expected: Uint8Array
      try {
        expected = uuidParse(response.session_id)
      } catch {
        return
      }
      if (!this.sameBytes(frame.sid, expected)) return
    }
    window.clearTimeout(pending.timer)
    this.pendingSessionCreates.delete(response.request_id)
    if (!response.ok) {
      pending.reject(new Error(response.error || 'upstream_unavailable'))
      return
    }
    pending.resolve(response.session_id!)
  }

  private rejectPendingSessionCreates(error: Error): void {
    const pending = Array.from(this.pendingSessionCreates.values())
    this.pendingSessionCreates.clear()
    for (const request of pending) {
      window.clearTimeout(request.timer)
      request.reject(error)
    }
  }

  private handleServiceOpened(payload: Uint8Array): void {
    let response: ServiceOpenedResponse
    try {
      const parsed = JSON.parse(decodeText(payload)) as Partial<ServiceOpenedResponse>
      if (!parsed || typeof parsed.request_id !== 'string' || typeof parsed.service_id !== 'string' ||
        typeof parsed.ok !== 'boolean' || parsed.error !== undefined && typeof parsed.error !== 'string') return
      response = parsed as ServiceOpenedResponse
    } catch {
      return
    }
    const pending = this.pendingServiceOpens.get(response.request_id)
    if (!pending || pending.generation !== this.generation || response.service_id !== pending.serviceId) return
    window.clearTimeout(pending.timer)
    this.pendingServiceOpens.delete(response.request_id)
    if (!response.ok) {
      pending.reject(new Error(response.error || 'service preview rejected'))
      return
    }
    pending.resolve({
      serviceId: pending.serviceId,
      clientTicket: '',
      clientToHostKey: new Uint8Array(),
      hostToClientKey: new Uint8Array(),
      peerAttemptId: pending.peerAttemptId,
    })
  }

  private rejectPendingServiceOpens(error: Error): void {
    const pending = Array.from(this.pendingServiceOpens.values())
    this.pendingServiceOpens.clear()
    for (const request of pending) {
      window.clearTimeout(request.timer)
      request.reject(error)
    }
  }

  private flush(): void {
    if (!this.transport || !this.ready) return
    if (this.pendingDriverClaim) {
      const payload = encodeText(JSON.stringify({ client_id: this.clientID, client_name: this.clientName }))
      if (!this.transport.sendFrame(encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload))) return
      this.pendingDriverClaim = false
    }
    if (this.pendingResize) {
      const resize = this.pendingResize
      if (!this.transport.sendFrame(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(resize.cols, resize.rows)))) return
      this.pendingResize = null
    }
    while (this.pendingInputs.length > 0) {
      const value = this.pendingInputs[0]
      if (!this.transport.sendFrame(encodeFrame(TYPE.IN, this.sidBytes, encodeText(value)))) return
      this.pendingInputs.shift()
    }
  }

  private fail(generation: number, transport: DirectTransport, error: Error): void {
    if (!this.isCurrent(generation, transport)) return
    this.transport = null
    transport.close()
    if (this.handover?.generation === generation) {
      this.authenticated = false
      this.ready = false
      return
    }
    this.recover(generation, error)
  }

  private recover(generation: number, error: Error): void {
    if (this.detached || this.suspended) return
    this.clearDirectFailbackTimer()
    this.rejectPendingFSRequests(new Error('filesystem request failed: Peer route changed'))
    this.rejectPendingSessionCreates(new Error('upstream_unavailable'))
    this.rejectPendingServiceOpens(new Error('service preview failed: Peer route changed'))
    this.retiredFSRequestIDs.clear()
    const wasActive = this.ready
    this.authenticated = false
    this.ready = false
    this.lastFailureReason = directFallbackReason(error, wasActive)
    this.handlers.onStatus?.('reconnecting')
    this.emitRoute(this.connectingRoute())
    if (
      this.route === 'direct' &&
      allowsQuickTunnelFallback(this.lastFailureReason) &&
      this.options.resolveQuickTunnelFallback
    ) {
      this.fallbackPending = true
      void this.tryQuickTunnelFallback(generation)
      return
    }
    this.scheduleReconnect()
  }

  private async tryQuickTunnelFallback(generation: number): Promise<void> {
    let available = false
    let timeout: number | null = null
    try {
      available = await Promise.race([
        this.options.resolveQuickTunnelFallback!(),
        new Promise<boolean>((resolve) => {
          timeout = window.setTimeout(() => resolve(false), 1000)
        }),
      ])
    } catch {
      available = false
    } finally {
      if (timeout !== null) window.clearTimeout(timeout)
    }
    if (
      generation !== this.generation ||
      !this.fallbackPending ||
      this.detached ||
      this.suspended ||
      this.transport !== null
    ) return
    this.fallbackPending = false
    if (available) {
      this.route = 'quick_tunnel'
      this.reconnectAttempts = Math.max(1, this.reconnectAttempts)
      this.start()
      return
    }
    this.scheduleReconnect()
  }

  private scheduleReconnect(): void {
    if (this.detached || this.suspended || this.reconnectTimer !== null) return
    const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++))
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      if (!this.detached && !this.suspended) this.start()
    }, delay)
  }

  private scheduleDirectFailback(): void {
    this.clearDirectFailbackTimer()
    if (
      !this.options.resolveDirectFailback ||
      this.route !== 'quick_tunnel' ||
      !this.ready ||
      this.detached ||
      this.suspended ||
      this.handover ||
      this.failbackProbe
    ) return
    const base = Math.max(1, this.options.directFailbackCooldownMs ?? defaultDirectFailbackCooldownMs)
    const delay = Math.min(maxDirectFailbackCooldownMs, base * Math.pow(2, this.failbackAttempts))
    const generation = this.generation
    this.failbackTimer = window.setTimeout(() => {
      this.failbackTimer = null
      void this.probeDirectFailback(generation)
    }, delay)
  }

  private async probeDirectFailback(generation: number): Promise<void> {
    if (
      generation !== this.generation ||
      this.route !== 'quick_tunnel' ||
      !this.ready ||
      this.detached ||
      this.suspended ||
      this.handover ||
      this.failbackProbe
    ) return
    const probe = {}
    this.failbackProbe = probe
    let available = false
    let timeout: number | null = null
    try {
      available = await Promise.race([
        this.options.resolveDirectFailback!(),
        new Promise<boolean>((resolve) => {
          timeout = window.setTimeout(() => resolve(false), 1000)
        }),
      ])
    } catch {
      available = false
    } finally {
      if (timeout !== null) window.clearTimeout(timeout)
      if (this.failbackProbe === probe) this.failbackProbe = null
    }
    if (
      generation !== this.generation ||
      this.route !== 'quick_tunnel' ||
      !this.ready ||
      this.detached ||
      this.suspended ||
      this.handover
    ) return
    if (!available) {
      this.failbackAttempts++
      this.scheduleDirectFailback()
      return
    }
    this.startDirectHandover(generation)
  }

  private startDirectHandover(generation: number): void {
    if (
      generation !== this.generation ||
      this.route !== 'quick_tunnel' ||
      !this.ready ||
      this.detached ||
      this.suspended ||
      this.handover
    ) return
    this.rejectPendingFSRequests(new Error('filesystem request failed: Peer route changed'))
    this.rejectPendingSessionCreates(new Error('upstream_unavailable'))
    this.rejectPendingServiceOpens(new Error('service preview failed: Peer route changed'))
    this.retiredFSRequestIDs.clear()
    let transport: DirectTransport
    const attempt: PeerHandoverAttempt = {
      generation,
      transport: null as unknown as DirectTransport,
      authenticated: false,
      diagnostics: null,
      startedAt: performance.now(),
      promoted: false,
    }
    try {
      transport = this.options.transportFactory({
        signalURL: '',
        sessionId: this.sessionID,
        sinceSeq: this.lastSeq,
        clientInstanceId: this.clientID,
        route: 'direct',
        callbacks: {
          onAuthenticated: () => {
            if (!this.isHandoverCurrent(attempt)) return
            attempt.authenticated = true
          },
          onFrame: (bytes) => {
            if (!this.isHandoverCurrent(attempt)) return
            this.handleFrame(decodeFrame(bytes))
          },
          onReady: (replayedSeq) => {
            if (!this.isHandoverCurrent(attempt)) return
            if (!attempt.authenticated || replayedSeq < this.lastSeq) {
              this.failHandover(attempt, new Error('invalid Peer replay cursor'))
              return
            }
            this.promoteHandover(attempt, replayedSeq)
          },
          onDiagnostics: (diagnostics) => {
            if (!this.isHandoverCurrent(attempt)) return
            attempt.diagnostics = diagnostics
            if (attempt.promoted) {
              this.diagnostics = diagnostics
              this.emitRoute('direct')
            } else {
              this.emitHandoverRoute(attempt)
            }
          },
          onFailure: (error) => {
            if (!this.isHandoverCurrent(attempt)) return
            if (attempt.promoted) this.fail(generation, attempt.transport, error)
            else this.failHandover(attempt, error)
          },
        },
      })
      attempt.transport = transport
      this.handover = attempt
      this.handlers.onStatus?.('reconnecting')
      this.emitHandoverRoute(attempt)
      transport.start()
    } catch (value) {
      if (this.handover === attempt) this.failHandover(attempt, value instanceof Error ? value : new Error(String(value)))
      else {
        this.failbackAttempts++
        this.scheduleDirectFailback()
      }
    }
  }

  private promoteHandover(attempt: PeerHandoverAttempt, replayedSeq: number): void {
    if (!this.isHandoverCurrent(attempt) || attempt.promoted) return
    const previous = this.transport
    attempt.promoted = true
    this.handover = null
    this.transport = attempt.transport
    this.route = 'direct'
    this.authenticated = true
    this.ready = true
    this.lastSeq = replayedSeq
    this.reconnectAttempts = 0
    this.failbackAttempts = 0
    this.lastFailureReason = undefined
    this.diagnostics = attempt.diagnostics
    this.startedAt = attempt.startedAt
    this.handlers.onStatus?.('attached')
    this.emitRoute('direct')
    previous?.close()
    this.flush()
  }

  private failHandover(attempt: PeerHandoverAttempt, error: Error): void {
    if (!this.isHandoverCurrent(attempt) || attempt.promoted) return
    this.handover = null
    attempt.transport.close()
    const previous = this.transport
    this.transport = null
    previous?.close()
    this.failbackAttempts++
    this.lastFailureReason = directFallbackReason(error, false)
    this.route = 'quick_tunnel'
    this.recover(attempt.generation, error)
  }

  private cancelHandover(attempt: PeerHandoverAttempt): void {
    if (!this.isHandoverCurrent(attempt) || attempt.promoted) return
    this.handover = null
    attempt.transport.close()
    const previous = this.transport
    this.transport = null
    previous?.close()
    this.failbackAttempts++
    this.lastFailureReason = undefined
    this.diagnostics = { route: 'quick_tunnel' }
    this.authenticated = false
    this.ready = false
    this.handlers.onStatus?.('reconnecting')
    this.emitRoute('connecting-quick-tunnel')
    this.scheduleReconnect()
  }

  private emitHandoverRoute(attempt: PeerHandoverAttempt): void {
    this.handlers.onRouteChange?.({
      route: 'connecting-direct',
      ...(attempt.diagnostics?.iceState ? { iceState: attempt.diagnostics.iceState } : {}),
      ...(attempt.diagnostics?.candidateType ? { candidateType: attempt.diagnostics.candidateType } : {}),
      ...(this.lastFailureReason ? { fallbackReason: this.lastFailureReason } : {}),
    })
  }

  private closeTransport(): void {
    this.rejectPendingFSRequests(new Error('filesystem request failed: Peer route changed'))
    this.rejectPendingSessionCreates(new Error('upstream_unavailable'))
    this.rejectPendingServiceOpens(new Error('service preview failed: Peer route changed'))
    this.retiredFSRequestIDs.clear()
    this.generation++
    this.fallbackPending = false
    this.failbackProbe = null
    this.failbackAttempts = 0
    this.clearDirectFailbackTimer()
    const handover = this.handover
    this.handover = null
    const transport = this.transport
    this.transport = null
    this.authenticated = false
    this.ready = false
    handover?.transport.close()
    transport?.close()
  }

  private clearReconnect(): void {
    if (this.reconnectTimer !== null) window.clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private clearDirectFailbackTimer(): void {
    if (this.failbackTimer !== null) window.clearTimeout(this.failbackTimer)
    this.failbackTimer = null
  }

  private isCurrent(generation: number, transport: DirectTransport): boolean {
    return generation === this.generation && this.transport === transport && !this.detached && !this.suspended
  }

  private isHandoverCurrent(attempt: PeerHandoverAttempt): boolean {
    if (attempt.promoted) return this.isCurrent(attempt.generation, attempt.transport)
    return this.handover === attempt &&
      attempt.generation === this.generation &&
      !this.detached &&
      !this.suspended
  }

  private sameSession(other: Uint8Array): boolean {
    return this.sameBytes(other, this.sidBytes)
  }

  private sameBytes(left: Uint8Array, right: Uint8Array): boolean {
    return left.length === right.length && left.every((value, index) => value === right[index])
  }

  private connectingRoute(): 'connecting-direct' | 'connecting-quick-tunnel' {
    return this.diagnostics?.route === 'quick_tunnel' || this.route === 'quick_tunnel'
      ? 'connecting-quick-tunnel'
      : 'connecting-direct'
  }

  private connectedRoute(): 'direct' | 'quick-tunnel' {
    return this.diagnostics?.route === 'quick_tunnel' || this.route === 'quick_tunnel'
      ? 'quick-tunnel'
      : 'direct'
  }

  private emitRoute(route: 'connecting-direct' | 'direct' | 'connecting-quick-tunnel' | 'quick-tunnel'): void {
    const setupTimeMs = route === 'direct' || route === 'quick-tunnel' ? Math.max(0, Math.round(performance.now() - this.startedAt)) : undefined
    this.handlers.onRouteChange?.({
      route,
      ...(this.diagnostics?.iceState ? { iceState: this.diagnostics.iceState } : {}),
      ...(this.diagnostics?.candidateType ? { candidateType: this.diagnostics.candidateType } : {}),
      ...(setupTimeMs !== undefined ? { setupTimeMs } : {}),
      ...(this.lastFailureReason ? { fallbackReason: this.lastFailureReason } : {}),
    })
  }
}
