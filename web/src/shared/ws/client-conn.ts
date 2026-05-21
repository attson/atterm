// client-conn.ts — owns the /client WebSocket lifecycle for a single PTY session.
//
// AGENTS.md red-line 6, web subset: RESIZE skip-on-match (last cols/rows
// memoised; identical resends are dropped) + queue IN while CONNECTING
// (re-flushed in onopen right after ATTACH). The desktop "predict-fork"
// half of the rule doesn't apply here — web only attaches to existing PTYs.
//
// Reconnect is exponential 500ms → 8s. detach() cancels the timer and
// closes the underlying socket; subsequent close events are ignored.

import {
  TYPE,
  decodeFrame,
  decodeOut,
  encodeFrame,
  encodeResize,
  uuidToBytes,
} from './protocol'
import { isMobileApp, loadRelayConfig } from '../api/relay-config'
import { ApiError } from '../api/client'

export type SessionStatus = 'connecting' | 'attached' | 'reconnecting' | 'ended'

export interface SessionConnectionHandlers {
  onOutput?: (data: Uint8Array, seq: number) => void
  onMeta?: (meta: Record<string, unknown>) => void
  onClose?: (info: { exit_code: number; reason?: string }) => void
  onReplayProgress?: (p: Record<string, unknown>) => void
  onStatus?: (status: SessionStatus) => void
}

const RECONNECT_INITIAL_MS = 500
const RECONNECT_MAX_MS = 8000

export class SessionConnection {
  private readonly sessionId: string
  private readonly sidBytes: Uint8Array
  private readonly handlers: SessionConnectionHandlers
  private ws: WebSocket | null = null
  private pendingInputs: string[] = []
  private lastSeq = 0
  private detached = false
  private reconnectAttempts = 0
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private lastSentCols: number | null = null
  private lastSentRows: number | null = null

  constructor(sessionId: string, handlers: SessionConnectionHandlers = {}) {
    this.sessionId = sessionId
    this.sidBytes = uuidToBytes(sessionId)
    this.handlers = handlers
  }

  attach(): void {
    if (this.detached) return
    this.openWS()
  }

  detach(): void {
    this.detached = true
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      try {
        this.ws.close()
      } catch {
        /* ignore */
      }
      this.ws = null
    }
  }

  sendInput(s: string): void {
    if (!this.ws || this.ws.readyState === WebSocket.CONNECTING) {
      this.pendingInputs.push(s)
      return
    }
    if (this.ws.readyState !== WebSocket.OPEN) return
    this.ws.send(encodeFrame(TYPE.IN, this.sidBytes, new TextEncoder().encode(s)))
  }

  sendResize(cols: number, rows: number): void {
    // Skip-on-match: identical to the last RESIZE we sent. Saves PTY a
    // gratuitous SIGWINCH when the user toggles back to the same size.
    if (cols === this.lastSentCols && rows === this.lastSentRows) return
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      // Defer cols/rows recording until the actual send fires.
      return
    }
    this.ws.send(encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows)))
    this.lastSentCols = cols
    this.lastSentRows = rows
  }

  sendPasteImage(blob: Blob, filename = 'clipboard-image'): Promise<boolean> {
    return (async () => {
      if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false
      const buf = await blob.arrayBuffer()
      const data = btoaBytes(new Uint8Array(buf))
      const payload = new TextEncoder().encode(JSON.stringify({
        filename,
        content_type: blob.type || 'image/png',
        data,
      }))
      this.ws.send(encodeFrame(TYPE.PASTE_IMAGE, this.sidBytes, payload))
      return true
    })()
  }

  private openWS(): void {
    if (this.detached) return
    const url = wsUrl('/client')
    const cfg = isMobileApp() ? loadRelayConfig() : null
    const ws = cfg ? new WebSocket(url, [cfg.token]) : new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting')

    ws.onopen = () => {
      this.reconnectAttempts = 0
      this.handlers.onStatus?.('attached')
      const attachPayload = new TextEncoder().encode(JSON.stringify({
        session_id: this.sessionId,
        since_seq: this.lastSeq,
      }))
      ws.send(encodeFrame(TYPE.ATTACH, this.sidBytes, attachPayload))
      if (this.pendingInputs.length > 0) {
        const queued = this.pendingInputs
        this.pendingInputs = []
        for (const s of queued) {
          ws.send(encodeFrame(TYPE.IN, this.sidBytes, new TextEncoder().encode(s)))
        }
      }
    }

    ws.onmessage = (ev: MessageEvent) => {
      let f
      try {
        f = decodeFrame(new Uint8Array(ev.data as ArrayBuffer))
      } catch {
        return
      }
      switch (f.type) {
        case TYPE.OUT: {
          const { seq, data } = decodeOut(f.payload)
          if (seq > this.lastSeq) this.lastSeq = seq
          this.handlers.onOutput?.(data, seq)
          break
        }
        case TYPE.META: {
          try {
            const meta = JSON.parse(new TextDecoder().decode(f.payload))
            this.handlers.onMeta?.(meta)
          } catch {
            /* ignore malformed META */
          }
          break
        }
        case TYPE.CLOSE: {
          let info: { exit_code: number; reason?: string } = { exit_code: 0 }
          try {
            info = JSON.parse(new TextDecoder().decode(f.payload))
          } catch {
            /* ignore */
          }
          this.handlers.onClose?.(info)
          this.handlers.onStatus?.('ended')
          break
        }
        case TYPE.REPLAY_PROGRESS: {
          try {
            this.handlers.onReplayProgress?.(JSON.parse(new TextDecoder().decode(f.payload)))
          } catch {
            /* ignore */
          }
          break
        }
      }
    }

    ws.onclose = () => {
      this.ws = null
      if (this.detached) return
      this.handlers.onStatus?.('reconnecting')
      const delay = Math.min(RECONNECT_MAX_MS, RECONNECT_INITIAL_MS * Math.pow(2, this.reconnectAttempts++))
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = null
        this.openWS()
      }, delay)
    }

    ws.onerror = () => {
      // onclose follows; nothing to do here.
    }
  }
}

export function wsUrl(path: string): string {
  if (isMobileApp()) {
    const cfg = loadRelayConfig()
    if (!cfg) throw new ApiError(0, 'relay_not_configured', null)
    const u = new URL(cfg.base)
    const proto = u.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${u.host}${path}`
  }
  if (typeof location === 'undefined') return `ws://localhost${path}`
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}${path}`
}

function btoaBytes(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]!)
  return btoa(s)
}
