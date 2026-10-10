import type { DirectClientOptions, DirectTransportDiagnostics } from './directClient'
import type { DirectTransport } from './connection'

interface NativeDirectEvent {
  kind: string
  frame_base64?: string
  last_replayed_seq?: number
  route?: string
  ice_state?: string
  candidate_type?: string
  error?: string
}

export interface NativeDirectBridge {
  on(event: string, handler: (data: unknown) => void): () => void
  start(req: { id: string; session_id: string; since_seq: number; client_instance_id: string; route?: NativePeerRoute }): Promise<void>
  send(id: string, frame: number[]): Promise<void>
  stop(id: string): Promise<void>
}

/** Go/Pion transports authenticate natively. Relay direct callers may still
 * include accountKey, while accountless Peer callers intentionally omit it. */
export type NativeDirectClientOptions = Omit<DirectClientOptions, 'accountKey'> & {
  accountKey?: Uint8Array
  route?: NativePeerRoute
}

export type NativePeerRoute = 'direct' | 'lan' | 'quick_tunnel'

function asError(value: unknown): Error {
  if (value instanceof Error) return value
  if (typeof value === 'string') return new Error(value)
  return new Error(String(value))
}

function decodeBase64(value: string): Uint8Array {
  const binary = atob(value)
  const out = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
  return out
}

function diagnostics(event: NativeDirectEvent): DirectTransportDiagnostics | null {
  const route = event.route === 'direct' || event.route === 'lan' || event.route === 'quick_tunnel' ? event.route : undefined
  const state = event.ice_state
  const iceState = state === 'new' || state === 'checking' || state === 'connected' || state === 'completed' ||
      state === 'failed' || state === 'disconnected' || state === 'closed' ? state : undefined
  if (!route && !iceState) return null
  const candidate = event.candidate_type
  const candidateType = candidate === 'host' || candidate === 'srflx' || candidate === 'prflx' || candidate === 'relay'
    ? candidate
    : undefined
  return { ...(route ? { route } : {}), ...(iceState ? { iceState } : {}), ...(candidateType ? { candidateType } : {}) }
}

/** Wails adapter for Go/Pion direct clients. Its owner supplies either the
 * Relay fallback lifecycle or the accountless Peer reconnect lifecycle. */
export class NativeDirectClientTransport implements DirectTransport {
  private readonly id = crypto.randomUUID()
  private off: (() => void) | null = null
  private sendChain = Promise.resolve()
  private started = false
  private authenticated = false
  private closed = false

  constructor(
    private readonly options: NativeDirectClientOptions,
    private readonly bridge: NativeDirectBridge,
    private readonly eventPrefix = 'native-direct:event:',
  ) {}

  start(): void {
    if (this.started || this.closed) return
    this.started = true
    this.off = this.bridge.on(`${this.eventPrefix}${this.id}`, (data) => this.handleEvent(data))
    void this.bridge.start({
      id: this.id,
      session_id: this.options.sessionId,
      since_seq: this.options.sinceSeq,
      client_instance_id: this.options.clientInstanceId,
      ...(this.options.route ? { route: this.options.route } : {}),
    }).catch((error) => this.fail(error))
  }

  sendFrame(frame: Uint8Array): boolean {
    if (this.closed || !this.authenticated) return false
    const copy = Array.from(frame)
    this.sendChain = this.sendChain
      .then(() => this.bridge.send(this.id, copy))
      .catch((error) => this.fail(error))
    return true
  }

  nativeAttemptId(): string {
    return this.id
  }

  close(): void {
    this.finish()
  }

  private handleEvent(data: unknown): void {
    if (this.closed || !data || typeof data !== 'object') return
    const event = data as NativeDirectEvent
    try {
      switch (event.kind) {
        case 'authenticated':
          this.authenticated = true
          this.options.callbacks.onAuthenticated?.()
          return
        case 'frame':
          if (typeof event.frame_base64 !== 'string') throw new Error('native direct frame is missing')
          this.options.callbacks.onFrame(decodeBase64(event.frame_base64))
          return
        case 'ready':
          if (!Number.isSafeInteger(event.last_replayed_seq) || (event.last_replayed_seq ?? -1) < 0) {
            throw new Error('invalid native direct replay cursor')
          }
          this.options.callbacks.onReady(event.last_replayed_seq!)
          return
        case 'diagnostics': {
          const value = diagnostics(event)
          if (value) this.options.callbacks.onDiagnostics?.(value)
          return
        }
        case 'failure':
          this.fail(new Error(event.error || 'native direct connection failed'))
          return
        default:
          throw new Error(`unexpected native direct event: ${event.kind}`)
      }
    } catch (error) {
      this.fail(error)
    }
  }

  private fail(value: unknown): void {
    if (this.closed) return
    const error = asError(value)
    this.finish()
    try { this.options.callbacks.onFailure(error) } catch { /* route fallback callbacks stay isolated */ }
  }

  private finish(): void {
    if (this.closed) return
    this.closed = true
    this.authenticated = false
    this.off?.()
    this.off = null
    void this.bridge.stop(this.id).catch(() => {})
  }
}
