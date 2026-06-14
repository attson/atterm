// Mirror of internal/connhealth/connhealth.go — feeds the ConnHealthPill /
// ConnHealthDrawer for web/PWA/mobile clients. The semantics (RTT ring size,
// reconnect window, EMA alpha) MUST match the Go side so both desktop uplink
// and web/mobile show the same numbers.

export const RTT_RING_SIZE = 60
export const RECONNECT_HISTORY_SIZE = 5
export const RECONNECT_WINDOW_MS = 60 * 60 * 1000
const BYTES_EMA_ALPHA = 0.2

export type ConnState = 'closed' | 'connecting' | 'connected' | 'reconnecting'

export interface RTTSample {
  at_ms: number
  rtt_ms: number
}

export interface ReconnectEvent {
  at_ms: number
  reason: string
  duration_ms: number
}

export interface ConnHealthSnapshot {
  state: ConnState
  rtt: {
    last_ms: number | null
    p50_ms: number | null
    p95_ms: number | null
  }
  rtt_samples: RTTSample[]
  reconnect: {
    count_last_hour: number
    last_at_ms: number | null
    last_reason: string
    history: ReconnectEvent[]
  }
  bytes: {
    in_per_sec: number
    out_per_sec: number
  }
  seq_gaps: number
}

type Listener = () => void

export class Tracker {
  private state: ConnState = 'closed'
  private rttRing: RTTSample[]
  private rttHead = 0
  private rttFilled = false

  private reconnects: ReconnectEvent[] = []
  private pendingReason = ''
  private pendingReconnect = false
  private reconnectStartAt = 0

  private bytesInBucket = 0
  private bytesOutBucket = 0
  private bytesBucketSec = 0
  private bytesInPerSec = 0
  private bytesOutPerSec = 0

  private seqGaps = 0

  private listeners: Set<Listener> = new Set()

  constructor() {
    this.rttRing = new Array<RTTSample>(RTT_RING_SIZE)
    for (let i = 0; i < RTT_RING_SIZE; i++) {
      this.rttRing[i] = { at_ms: 0, rtt_ms: 0 }
    }
  }

  onChange(fn: Listener): () => void {
    this.listeners.add(fn)
    return () => { this.listeners.delete(fn) }
  }

  private emit(): void {
    for (const fn of this.listeners) {
      try { fn() } catch { /* swallow */ }
    }
  }

  setState(s: ConnState, nowMS: number): void {
    if (this.state === s) return
    if (s === 'reconnecting') {
      this.pendingReconnect = true
      this.reconnectStartAt = nowMS
    } else if (this.pendingReconnect && s === 'connected') {
      const ev: ReconnectEvent = {
        at_ms: this.reconnectStartAt,
        reason: this.pendingReason,
        duration_ms: nowMS - this.reconnectStartAt,
      }
      this.reconnects.push(ev)
      if (this.reconnects.length > RECONNECT_HISTORY_SIZE) {
        this.reconnects = this.reconnects.slice(-RECONNECT_HISTORY_SIZE)
      }
      this.pendingReconnect = false
      this.pendingReason = ''
    }
    this.state = s
    this.emit()
  }

  setReconnectReason(reason: string, _nowMS: number): void {
    this.pendingReason = reason
  }

  onPongRTT(rttMS: number, nowMS: number): void {
    this.rttRing[this.rttHead] = { at_ms: nowMS, rtt_ms: rttMS }
    this.rttHead = (this.rttHead + 1) % RTT_RING_SIZE
    if (this.rttHead === 0) this.rttFilled = true
  }

  onBytesIn(n: number, nowMS: number): void {
    this.rollover(nowMS)
    this.bytesInBucket += n
  }

  onBytesOut(n: number, nowMS: number): void {
    this.rollover(nowMS)
    this.bytesOutBucket += n
  }

  tick(nowMS: number): void {
    this.rollover(nowMS)
  }

  private rollover(nowMS: number): void {
    const sec = Math.floor(nowMS / 1000)
    if (sec === this.bytesBucketSec) return
    this.bytesInPerSec = BYTES_EMA_ALPHA * this.bytesInBucket + (1 - BYTES_EMA_ALPHA) * this.bytesInPerSec
    this.bytesOutPerSec = BYTES_EMA_ALPHA * this.bytesOutBucket + (1 - BYTES_EMA_ALPHA) * this.bytesOutPerSec
    this.bytesInBucket = 0
    this.bytesOutBucket = 0
    this.bytesBucketSec = sec
  }

  onSeqGap(): void {
    this.seqGaps += 1
  }

  snapshot(nowMS: number): ConnHealthSnapshot {
    const samples = this.orderedRTT()
    const out: ConnHealthSnapshot = {
      state: this.state,
      rtt: { last_ms: null, p50_ms: null, p95_ms: null },
      rtt_samples: samples,
      reconnect: {
        count_last_hour: 0,
        last_at_ms: null,
        last_reason: '',
        history: this.reconnects.slice(),
      },
      bytes: {
        in_per_sec: Math.round(this.bytesInPerSec),
        out_per_sec: Math.round(this.bytesOutPerSec),
      },
      seq_gaps: this.seqGaps,
    }
    if (samples.length > 0) {
      out.rtt.last_ms = samples[samples.length - 1]!.rtt_ms
      const sorted = samples.map(s => s.rtt_ms).sort((a, b) => a - b)
      out.rtt.p50_ms = nearestRank(sorted, 50)
      out.rtt.p95_ms = nearestRank(sorted, 95)
    }
    const cutoff = nowMS - RECONNECT_WINDOW_MS
    out.reconnect.count_last_hour = this.reconnects.filter(e => e.at_ms >= cutoff).length
    if (this.reconnects.length > 0) {
      const last = this.reconnects[this.reconnects.length - 1]!
      out.reconnect.last_at_ms = last.at_ms
      out.reconnect.last_reason = last.reason
    }
    return out
  }

  private orderedRTT(): RTTSample[] {
    if (!this.rttFilled) {
      return this.rttRing.slice(0, this.rttHead)
    }
    return this.rttRing.slice(this.rttHead).concat(this.rttRing.slice(0, this.rttHead))
  }
}

function nearestRank(sorted: number[], p: number): number {
  if (sorted.length === 0) return 0
  if (p <= 0) return sorted[0]!
  if (p >= 100) return sorted[sorted.length - 1]!
  const idx = Math.floor((p * sorted.length) / 100)
  return sorted[Math.min(idx, sorted.length - 1)]!
}
