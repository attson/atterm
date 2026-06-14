import { describe, it, expect, beforeEach, vi } from 'vitest'
import { Tracker, RTT_RING_SIZE } from '@shared/connhealth/connhealth'

describe('Tracker', () => {
  let tr: Tracker
  beforeEach(() => {
    tr = new Tracker()
  })

  it('starts in the closed state with empty RTT', () => {
    const s = tr.snapshot(0)
    expect(s.state).toBe('closed')
    expect(s.rtt.last_ms).toBeNull()
    expect(s.rtt_samples).toEqual([])
    expect(s.reconnect.count_last_hour).toBe(0)
  })

  it('records RTT samples and computes p50/p95', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < 10; i++) {
      tr.onPongRTT(50 + i * 10, i * 1000)
    }
    const s = tr.snapshot(10_000)
    expect(s.rtt.last_ms).toBe(140)
    expect(s.rtt.p50_ms!).toBeGreaterThanOrEqual(90)
    expect(s.rtt.p50_ms!).toBeLessThanOrEqual(100)
    expect(s.rtt.p95_ms!).toBeGreaterThanOrEqual(130)
    expect(s.rtt.p95_ms!).toBeLessThanOrEqual(140)
    expect(s.rtt_samples.length).toBe(10)
  })

  it('evicts oldest RTT samples past RTT_RING_SIZE', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < RTT_RING_SIZE + 5; i++) {
      tr.onPongRTT(i, i * 1000)
    }
    const s = tr.snapshot(0)
    expect(s.rtt_samples.length).toBe(RTT_RING_SIZE)
    expect(s.rtt_samples[0].rtt_ms).toBe(5)
  })

  it('tracks reconnect events with duration and reason', () => {
    tr.setState('connected', 0)
    tr.setState('reconnecting', 60_000)
    tr.setReconnectReason('ws_close_1006', 60_000)
    tr.setState('connected', 62_000)
    const s = tr.snapshot(120_000)
    expect(s.reconnect.count_last_hour).toBe(1)
    expect(s.reconnect.history).toHaveLength(1)
    expect(s.reconnect.history[0].reason).toBe('ws_close_1006')
    expect(s.reconnect.history[0].duration_ms).toBe(2000)
  })

  it('windows reconnect count at one hour', () => {
    tr.setState('connected', 0)
    tr.setState('reconnecting', 100_000)
    tr.setState('connected', 101_000)
    tr.setState('reconnecting', 200_000)
    tr.setState('connected', 201_000)
    tr.setState('reconnecting', 5_000_000)
    tr.setState('connected', 5_001_000)
    const s = tr.snapshot(5_500_000)
    expect(s.reconnect.count_last_hour).toBe(1)
  })

  it('computes byte EMAs after sustained ticks', () => {
    tr.setState('connected', 0)
    for (let i = 0; i < 30; i++) {
      tr.onBytesIn(1000, i * 1000)
      tr.onBytesOut(500, i * 1000)
      tr.tick((i + 1) * 1000)
    }
    const s = tr.snapshot(30_000)
    expect(s.bytes.in_per_sec).toBeGreaterThanOrEqual(800)
    expect(s.bytes.in_per_sec).toBeLessThanOrEqual(1100)
    expect(s.bytes.out_per_sec).toBeGreaterThanOrEqual(400)
    expect(s.bytes.out_per_sec).toBeLessThanOrEqual(550)
  })

  it('counts seq gaps via onSeqGap', () => {
    tr.onSeqGap()
    tr.onSeqGap()
    tr.onSeqGap()
    expect(tr.snapshot(0).seq_gaps).toBe(3)
  })

  it('emits onChange listeners on state transitions', () => {
    const listener = vi.fn()
    tr.onChange(listener)
    tr.setState('connecting', 0)
    tr.setState('connected', 100)
    expect(listener).toHaveBeenCalledTimes(2)
  })
})
