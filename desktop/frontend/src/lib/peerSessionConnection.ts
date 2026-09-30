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
  DirectTransport,
  FSEvent,
  FSResponse,
  ServiceOpenResult,
} from './connection'
import type { DirectTransportDiagnostics } from './directClient'
import type { NativeDirectClientOptions } from './nativeDirectClient'

export interface PeerSessionConnectionOptions {
  clientName?: string
  transportFactory: (options: NativeDirectClientOptions) => DirectTransport
}

/** Direct-only terminal connection for accountless Peer sessions. It keeps
 * the Relay-oriented SessionConnection untouched and reconnects exclusively
 * through the native Rendezvous transport. */
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
  private pendingInputs: string[] = []
  private pendingResize: { cols: number; rows: number } | null = null
  private pendingDriverClaim = false
  private currentDriverClientID = ''
  private diagnostics: DirectTransportDiagnostics | null = null
  private startedAt = 0

  constructor(
    private readonly sessionID: string,
    private readonly handlers: ConnectionHandlers,
    private readonly options: PeerSessionConnectionOptions,
  ) {
    this.sidBytes = uuidParse(sessionID)
    this.clientName = options.clientName?.trim() || 'desktop'
  }

  attach(): void {
    if (this.detached) return
    this.suspended = false
    if (this.transport || this.reconnectTimer !== null) return
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

  openService(_port: number, _host = 'localhost'): Promise<ServiceOpenResult> {
    return Promise.reject(new Error('service preview is unavailable for Peer sessions'))
  }

  closeService(_serviceID: string): void {}

  onFSEvent(_handler: (event: FSEvent) => void): () => void {
    return () => {}
  }

  sendFSRequest(_request: unknown): Promise<FSResponse> {
    return Promise.reject(new Error('filesystem access is unavailable for Peer sessions'))
  }

  sendPasteImage(_blob: Blob, _filename = 'clipboard-image'): Promise<boolean> {
    return Promise.reject(new Error('image paste is unavailable for Peer sessions'))
  }

  sendPasteFile(_blob: Blob, _filename: string): Promise<boolean> {
    return Promise.reject(new Error('file paste is unavailable for Peer sessions'))
  }

  private start(): void {
    const generation = ++this.generation
    this.authenticated = false
    this.ready = false
    this.diagnostics = null
    this.startedAt = performance.now()
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting')
    this.emitRoute('connecting-direct')
    let transport: DirectTransport
    try {
      transport = this.options.transportFactory({
        signalURL: '',
        sessionId: this.sessionID,
        sinceSeq: this.lastSeq,
        clientInstanceId: this.clientID,
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
            this.handlers.onStatus?.('attached')
            this.emitRoute('direct')
            this.flush()
          },
          onDiagnostics: (diagnostics) => {
            if (!this.isCurrent(generation, transport)) return
            this.diagnostics = diagnostics
            this.emitRoute(this.ready ? 'direct' : 'connecting-direct')
          },
          onFailure: (error) => this.fail(generation, transport, error),
        },
      })
      this.transport = transport
      transport.start()
    } catch (value) {
      this.transport = null
      this.scheduleReconnect(value instanceof Error ? value : new Error(String(value)))
    }
  }

  private handleFrame(frame: Frame): void {
    if (!this.sameSession(frame.sid)) throw new Error('Peer frame session mismatch')
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
    throw new Error(`unsupported Peer frame type 0x${frame.type.toString(16).padStart(2, '0')}`)
  }

  private send(frame: Uint8Array): boolean {
    return Boolean(this.ready && this.transport?.sendFrame(frame))
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
    this.scheduleReconnect(error)
  }

  private scheduleReconnect(_error: Error): void {
    if (this.detached || this.suspended) return
    this.authenticated = false
    this.ready = false
    this.handlers.onStatus?.('reconnecting')
    this.emitRoute('connecting-direct')
    const delay = Math.min(8000, 500 * Math.pow(2, this.reconnectAttempts++))
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      if (!this.detached && !this.suspended) this.start()
    }, delay)
  }

  private closeTransport(): void {
    this.generation++
    const transport = this.transport
    this.transport = null
    this.authenticated = false
    this.ready = false
    transport?.close()
  }

  private clearReconnect(): void {
    if (this.reconnectTimer !== null) window.clearTimeout(this.reconnectTimer)
    this.reconnectTimer = null
  }

  private isCurrent(generation: number, transport: DirectTransport): boolean {
    return generation === this.generation && this.transport === transport && !this.detached && !this.suspended
  }

  private sameSession(other: Uint8Array): boolean {
    return other.length === this.sidBytes.length && other.every((value, index) => value === this.sidBytes[index])
  }

  private emitRoute(route: 'connecting-direct' | 'direct'): void {
    const setupTimeMs = route === 'direct' ? Math.max(0, Math.round(performance.now() - this.startedAt)) : undefined
    this.handlers.onRouteChange?.({
      route,
      ...(this.diagnostics?.iceState ? { iceState: this.diagnostics.iceState } : {}),
      ...(this.diagnostics?.candidateType ? { candidateType: this.diagnostics.candidateType } : {}),
      ...(setupTimeMs !== undefined ? { setupTimeMs } : {}),
    })
  }
}
