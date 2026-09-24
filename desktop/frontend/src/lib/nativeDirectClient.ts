import type { DirectClientOptions, DirectTransportDiagnostics } from './directClient'
import type { DirectTransport } from './connection'

interface NativeDirectEvent {
  kind: string
  frame_base64?: string
  last_replayed_seq?: number
  ice_state?: string
  candidate_type?: string
  error?: string
}

export interface NativeDirectBridge {
  on(event: string, handler: (data: unknown) => void): () => void
  start(req: { id: string; session_id: string; since_seq: number; client_instance_id: string }): Promise<void>
  send(id: string, frame: number[]): Promise<void>
  stop(id: string): Promise<void>
}

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
  const state = event.ice_state
  if (state !== 'new' && state !== 'checking' && state !== 'connected' && state !== 'completed' &&
      state !== 'failed' && state !== 'disconnected' && state !== 'closed') return null
  const candidate = event.candidate_type
  const candidateType = candidate === 'host' || candidate === 'srflx' || candidate === 'prflx' || candidate === 'relay'
    ? candidate
    : undefined
  return { iceState: state, ...(candidateType ? { candidateType } : {}) }
}

/** Wails adapter for the Go/Pion direct client. SessionConnection still owns
 * route switching, replay sequencing, and Relay fallback. */
export class NativeDirectClientTransport implements DirectTransport {
  private readonly id = crypto.randomUUID()
  private off: (() => void) | null = null
  private sendChain = Promise.resolve()
  private started = false
  private authenticated = false
  private closed = false

  constructor(
    private readonly options: DirectClientOptions,
    private readonly bridge: NativeDirectBridge,
  ) {}

  start(): void {
    if (this.started || this.closed) return
    this.started = true
    this.off = this.bridge.on(`native-direct:event:${this.id}`, (data) => this.handleEvent(data))
    void this.bridge.start({
      id: this.id,
      session_id: this.options.sessionId,
      since_seq: this.options.sinceSeq,
      client_instance_id: this.options.clientInstanceId,
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
