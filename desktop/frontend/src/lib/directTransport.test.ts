import { describe, expect, it } from 'vitest'
import { MAX_DIRECT_RECORD_PLAINTEXT } from './directCrypto'
import {
  DIRECT_REASSEMBLY_TIMEOUT_MS,
  DirectFrameReassembler,
  fragmentDirectFrame,
  MAX_DIRECT_FRAME_SIZE,
} from './directFraming'
import { DirectRoute, DirectRouteTracker } from './directRoute'

describe('direct frame fragmentation', () => {
  it('round-trips a 10 MiB frame within record bounds', () => {
    const frame = new Uint8Array(10 * 1024 * 1024)
    for (let i = 0; i < frame.length; i++) frame[i] = i % 251
    const fragments = fragmentDirectFrame(77n, frame)
    expect(fragments.length).toBeGreaterThan(1)
    expect(Math.max(...fragments.map((part) => part.length))).toBeLessThanOrEqual(MAX_DIRECT_RECORD_PLAINTEXT)
    const reassembler = new DirectFrameReassembler()
    let result: Uint8Array | null = null
    for (const fragment of fragments) result = reassembler.add(fragment, 1000)
    expect(result).not.toBeNull()
    expect(result!.length).toBe(frame.length)
    for (let i = 0; i < frame.length; i++) {
      if (result![i] !== frame[i]) throw new Error(`reassembled byte ${i} differs`)
    }
  })

  it('rejects gaps, timeout, and oversized frames', () => {
    const frame = new Uint8Array(MAX_DIRECT_RECORD_PLAINTEXT * 2)
    const fragments = fragmentDirectFrame(1n, frame)
    expect(() => new DirectFrameReassembler().add(fragments[1], 1000)).toThrow('offset')

    const expired = new DirectFrameReassembler()
    expired.add(fragments[0], 1000)
    expect(() => expired.add(fragments[1], 1000 + DIRECT_REASSEMBLY_TIMEOUT_MS + 1)).toThrow('timeout')
    expect(() => fragmentDirectFrame(1n, new Uint8Array(MAX_DIRECT_FRAME_SIZE + 1))).toThrow('too large')
  })
})

describe('direct route handover', () => {
  it('commits output once and keeps exactly one input route over 100 transitions', () => {
    const tracker = new DirectRouteTracker()
    const delivered = new Map<number, number>()
    let nextSeq = 1
    const commit = (generation: number, route: DirectRoute, seq: number) => {
      if (tracker.acceptOutput(generation, route, seq)) delivered.set(seq, (delivered.get(seq) ?? 0) + 1)
    }

    for (let cycle = 0; cycle < 100; cycle++) {
      const generation = tracker.beginDirect()
      expect(tracker.inputRoute()).toBe(DirectRoute.Relay)
      commit(generation, DirectRoute.Relay, nextSeq++)
      tracker.beginDirectReplay(generation)
      commit(generation, DirectRoute.Direct, nextSeq)
      commit(generation, DirectRoute.Relay, nextSeq++)
      tracker.noteDirectReady(generation, tracker.committedSeq)
      tracker.activateDirect(generation)
      expect(tracker.inputRoute()).toBe(DirectRoute.Direct)
      commit(generation, DirectRoute.Relay, nextSeq)
      commit(generation, DirectRoute.Direct, nextSeq++)

      const fallbackGeneration = tracker.directLost(generation)
      expect(tracker.inputRoute()).toBe(DirectRoute.None)
      commit(generation, DirectRoute.Direct, nextSeq)
      commit(fallbackGeneration, DirectRoute.Relay, nextSeq++)
      tracker.relayAttached(fallbackGeneration)
      expect(tracker.inputRoute()).toBe(DirectRoute.Relay)
    }

    for (let seq = 1; seq < nextSeq; seq++) expect(delivered.get(seq)).toBe(1)
  })

  it('waits for direct to catch a Relay cursor that advanced past DIRECT_READY', () => {
    const tracker = new DirectRouteTracker(10)
    const generation = tracker.beginDirect()
    tracker.beginDirectReplay(generation)

    expect(tracker.acceptOutput(generation, DirectRoute.Relay, 12)).toBe(true)
    tracker.noteDirectReady(generation, 10)
    expect(tracker.canActivateDirect(generation)).toBe(false)
    expect(() => tracker.activateDirect(generation)).toThrow('invalid direct route transition')

    // Direct duplicates still advance its observed cursor even though Relay
    // already committed those OUT frames to xterm.
    expect(tracker.acceptOutput(generation, DirectRoute.Direct, 11)).toBe(false)
    expect(tracker.canActivateDirect(generation)).toBe(false)
    expect(tracker.acceptOutput(generation, DirectRoute.Direct, 12)).toBe(false)
    expect(tracker.canActivateDirect(generation)).toBe(true)
    tracker.activateDirect(generation)
    expect(tracker.inputRoute()).toBe(DirectRoute.Direct)
  })
})
