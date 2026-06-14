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
import { Tracker, type ConnHealthSnapshot } from '../connhealth/connhealth'

const PING_INTERVAL_MS = 5000
const TICK_INTERVAL_MS = 1000
const FRAME_HEADER_OVERHEAD = 22 // ver(1) + type(1) + payload_len(4) + sid(16)

export type SessionStatus = 'connecting' | 'attached' | 'reconnecting' | 'ended' | 'lost'

export interface SessionConnectionHandlers {
  onOutput?: (data: Uint8Array, seq: number) => void
  onMeta?: (meta: Record<string, unknown>) => void
  onClose?: (info: { exit_code: number; reason?: string }) => void
  onReplayProgress?: (p: Record<string, unknown>) => void
  onStatus?: (status: SessionStatus) => void
  onDriverChange?: (driverClientID: string, isMe: boolean, driverName: string) => void
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
  private consecutiveFailures = 0
  private firstFailureAt: number | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private lastSentCols: number | null = null
  private lastSentRows: number | null = null
  private readonly clientID = crypto.randomUUID()
  private readonly clientName = 'web'
  private currentDriverClientID = ''

  private readonly health = new Tracker()
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private tickTimer: ReturnType<typeof setInterval> | null = null
  private lastSeqForGap = 0

  constructor(sessionId: string, handlers: SessionConnectionHandlers = {}) {
    this.sessionId = sessionId
    this.sidBytes = uuidToBytes(sessionId)
    this.handlers = handlers
  }

  getHealth(): ConnHealthSnapshot {
    return this.health.snapshot(Date.now())
  }

  onHealthChange(fn: () => void): () => void {
    return this.health.onChange(fn)
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
    this.stopHealthLoops()
    this.health.setState('closed', Date.now())
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
    const frame = encodeFrame(TYPE.IN, this.sidBytes, new TextEncoder().encode(s))
    this.ws.send(frame)
    this.health.onBytesOut(frame.byteLength, Date.now())
  }

  sendResize(cols: number, rows: number): void {
    // Skip-on-match: identical to the last RESIZE we sent. Saves PTY a
    // gratuitous SIGWINCH when the user toggles back to the same size.
    if (cols === this.lastSentCols && rows === this.lastSentRows) return
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      // Defer cols/rows recording until the actual send fires.
      return
    }
    const frame = encodeFrame(TYPE.RESIZE, this.sidBytes, encodeResize(cols, rows))
    this.ws.send(frame)
    this.health.onBytesOut(frame.byteLength, Date.now())
    this.lastSentCols = cols
    this.lastSentRows = rows
  }

  claimDriver(): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    const payload = new TextEncoder().encode(
      JSON.stringify({ client_id: this.clientID, client_name: this.clientName }),
    )
    const frame = encodeFrame(TYPE.CLAIM_DRIVER, this.sidBytes, payload)
    this.ws.send(frame)
    this.health.onBytesOut(frame.byteLength, Date.now())
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
      const frame = encodeFrame(TYPE.PASTE_IMAGE, this.sidBytes, payload)
      this.ws.send(frame)
      this.health.onBytesOut(frame.byteLength, Date.now())
      return true
    })()
  }

  private startHealthLoops(ws: WebSocket): void {
    this.stopHealthLoops()
    this.tickTimer = setInterval(() => { this.health.tick(Date.now()) }, TICK_INTERVAL_MS)
    this.pingTimer = setInterval(() => {
      if (ws.readyState !== WebSocket.OPEN) return
      const nowMS = Date.now()
      const payload = new Uint8Array(8)
      const dv = new DataView(payload.buffer)
      dv.setUint32(0, Math.floor(nowMS / 0x100000000), false)
      dv.setUint32(4, nowMS >>> 0, false)
      const frame = encodeFrame(TYPE.PING, this.sidBytes, payload)
      ws.send(frame)
      this.health.onBytesOut(frame.byteLength, Date.now())
    }, PING_INTERVAL_MS)
  }

  private stopHealthLoops(): void {
    if (this.pingTimer !== null) { clearInterval(this.pingTimer); this.pingTimer = null }
    if (this.tickTimer !== null) { clearInterval(this.tickTimer); this.tickTimer = null }
  }

  private openWS(): void {
    if (this.detached) return
    const url = wsUrl('/client')
    const cfg = loadRelayConfig()
    const subprotocols = cfg?.sessionToken ? [`atterm-token.${cfg.sessionToken}`] : []
    const ws = new WebSocket(url, subprotocols)
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    this.health.setState(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting', Date.now())
    this.handlers.onStatus?.(this.reconnectAttempts === 0 ? 'connecting' : 'reconnecting')

    ws.onopen = () => {
      this.reconnectAttempts = 0
      this.consecutiveFailures = 0
      this.firstFailureAt = null
      this.health.setState('connected', Date.now())
      this.handlers.onStatus?.('attached')
      const attachPayload = new TextEncoder().encode(JSON.stringify({
        session_id: this.sessionId,
        since_seq: this.lastSeq,
        client_id: this.clientID,
        client_name: this.clientName,
      }))
      const attachFrame = encodeFrame(TYPE.ATTACH, this.sidBytes, attachPayload)
      ws.send(attachFrame)
      this.health.onBytesOut(attachFrame.byteLength, Date.now())
      if (this.pendingInputs.length > 0) {
        const queued = this.pendingInputs
        this.pendingInputs = []
        for (const s of queued) {
          const f = encodeFrame(TYPE.IN, this.sidBytes, new TextEncoder().encode(s))
          ws.send(f)
          this.health.onBytesOut(f.byteLength, Date.now())
        }
      }
      this.startHealthLoops(ws)
    }

    ws.onmessage = (ev: MessageEvent) => {
      const raw = new Uint8Array(ev.data as ArrayBuffer)
      this.health.onBytesIn(raw.byteLength, Date.now())
      let f
      try {
        f = decodeFrame(raw)
      } catch {
        return
      }
      switch (f.type) {
        case TYPE.OUT: {
          const { seq, data } = decodeOut(f.payload)
          if (this.lastSeqForGap !== 0 && seq > this.lastSeqForGap + 1) {
            this.health.onSeqGap()
          }
          this.lastSeqForGap = seq
          if (seq > this.lastSeq) this.lastSeq = seq
          this.handlers.onOutput?.(data, seq)
          break
        }
        case TYPE.PONG: {
          if (f.payload.length === 8) {
            const dv = new DataView(f.payload.buffer, f.payload.byteOffset, f.payload.byteLength)
            const hi = dv.getUint32(0, false)
            const lo = dv.getUint32(4, false)
            const sentMS = hi * 0x100000000 + lo
            const rtt = Date.now() - sentMS
            if (rtt >= 0 && rtt < 60_000) {
              this.health.onPongRTT(rtt, Date.now())
            }
          }
          break
        }
        case TYPE.META: {
          try {
            const meta = JSON.parse(new TextDecoder().decode(f.payload))
            this.handlers.onMeta?.(meta)
            const newDriver = String((meta as { driver_client_id?: unknown }).driver_client_id ?? '')
            const newDriverName = String((meta as { driver_client_name?: unknown }).driver_client_name ?? '')
            if (newDriver !== this.currentDriverClientID) {
              this.currentDriverClientID = newDriver
              this.handlers.onDriverChange?.(newDriver, newDriver !== '' && newDriver === this.clientID, newDriverName)
            }
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

    ws.onclose = (ev: CloseEvent) => {
      this.ws = null
      this.stopHealthLoops()
      if (this.detached) {
        this.health.setState('closed', Date.now())
        return
      }
      const reason = ev.code ? `ws_close_${ev.code}` : 'ws_close'
      this.health.setReconnectReason(reason, Date.now())
      this.health.setState('reconnecting', Date.now())
      this.consecutiveFailures += 1
      if (this.firstFailureAt === null) this.firstFailureAt = Date.now()
      const lost = shouldShowReconnectBanner({
        consecutiveFailures: this.consecutiveFailures,
        firstFailureAt: this.firstFailureAt,
        now: Date.now(),
      })
      this.handlers.onStatus?.(lost ? 'lost' : 'reconnecting')
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
    const baseStr = cfg?.baseURL
    if (!baseStr) throw new ApiError(0, 'relay_not_configured', null)
    const u = new URL(baseStr)
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

export interface ReconnectStatus {
  consecutiveFailures: number
  firstFailureAt: number | null
  now: number
}

const RECONNECT_BANNER_MIN_FAILURES = 5
const RECONNECT_BANNER_MIN_ELAPSED_MS = 30_000

export function shouldShowReconnectBanner(s: ReconnectStatus): boolean {
  if (s.consecutiveFailures >= RECONNECT_BANNER_MIN_FAILURES) return true
  if (s.firstFailureAt !== null && s.now - s.firstFailureAt >= RECONNECT_BANNER_MIN_ELAPSED_MS) {
    return s.consecutiveFailures > 0
  }
  return false
}
