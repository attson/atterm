// Transfer leases cover work between protocol requests as well as bytes still
// queued locally. A drained send is not an acknowledgement from the remote host.
export class TransferActivity {
  private leases = new Set<symbol>()
  private controller = new AbortController()

  constructor(private readonly onIdle: () => void) {}

  get busy(): boolean { return this.leases.size > 0 }
  get signal(): AbortSignal { return this.controller.signal }

  begin(): () => void {
    const lease = Symbol()
    this.leases.add(lease)
    return () => {
      if (!this.leases.delete(lease)) return
      if (!this.busy) this.onIdle()
    }
  }

  cancel(): void {
    this.leases.clear()
    this.controller.abort()
    this.controller = new AbortController()
  }
}

const TRANSFER_TIMEOUT_MS = 30_000

/** Bound blob decoding, or race transport-owned drains against cancellation
 * without imposing a total deadline on a healthy slow transfer. */
export function waitForTransfer<T>(operation: Promise<T>, signal: AbortSignal, timeoutMs: number | null = TRANSFER_TIMEOUT_MS): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const aborted = () => finish(new Error('transfer cancelled: connection closed'))
    const timer = timeoutMs === null ? undefined : window.setTimeout(() => finish(new Error('transfer timed out')), timeoutMs)
    function finish(error: Error | null, result?: T): void {
      if (timer !== undefined) window.clearTimeout(timer)
      signal.removeEventListener('abort', aborted)
      if (error) reject(error)
      else resolve(result as T)
    }
    signal.addEventListener('abort', aborted, { once: true })
    if (signal.aborted) aborted()
    operation.then((result) => finish(null, result), (error: unknown) => finish(error instanceof Error ? error : new Error(String(error))))
  })
}

interface BufferedTransport {
  readonly bufferedAmount: number
  readonly readyState: number | string
}

/** Browser buffers expose progress but no delivery acknowledgement. Reset the
 * stall deadline on progress so a healthy slow upload can finish. */
export function drainBufferedTransport(target: BufferedTransport, signal: AbortSignal): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    let timer: number | undefined
    let previous = target.bufferedAmount
    let lastProgress = Date.now()
    const aborted = () => finish(new Error('transfer cancelled: connection closed'))
    function finish(error?: Error): void {
      if (timer !== undefined) window.clearTimeout(timer)
      signal.removeEventListener('abort', aborted)
      if (error) reject(error)
      else resolve()
    }
    const check = () => {
      if (signal.aborted) { aborted(); return }
      // WebSocket.OPEN is 1; RTCDataChannel uses the string "open".
      if (target.readyState !== 1 && target.readyState !== 'open') {
        finish(new Error('transfer failed: transport closed'))
        return
      }
      const remaining = target.bufferedAmount
      if (!(remaining > 0)) { finish(); return }
      if (remaining < previous) lastProgress = Date.now()
      previous = remaining
      if (Date.now() - lastProgress >= TRANSFER_TIMEOUT_MS) {
        finish(new Error('transfer timed out'))
        return
      }
      timer = window.setTimeout(check, 25)
    }
    signal.addEventListener('abort', aborted, { once: true })
    check()
  })
}

export function drainWebSocket(ws: WebSocket, signal: AbortSignal): Promise<void> {
  return drainBufferedTransport(ws, signal)
}
