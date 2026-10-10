import { describe, expect, it } from 'vitest'
import { MAX_DIRECT_RECORD_PLAINTEXT } from '../directCrypto'
import { fragmentPeerSignal } from './quickTunnelClient'

describe('Peer Quick Tunnel signaling', () => {
  it('uses the Go ASF1 fragment layout for large browser SDP', () => {
    const payload = new Uint8Array(MAX_DIRECT_RECORD_PLAINTEXT + 37).map((_, index) => index & 0xff)
    const fragments = fragmentPeerSignal(37n, payload)
    expect(fragments).toHaveLength(2)
    let offset = 0
    const restored = new Uint8Array(payload.length)
    for (const fragment of fragments) {
      const view = new DataView(fragment.buffer, fragment.byteOffset, fragment.byteLength)
      expect(new TextDecoder().decode(fragment.slice(0, 4))).toBe('ASF1')
      expect(view.getBigUint64(4, false)).toBe(37n)
      expect(view.getUint32(12, false)).toBe(0xfffffffe)
      expect(view.getUint32(16, false)).toBe(offset)
      expect(view.getUint32(20, false)).toBe(payload.length)
      const chunk = fragment.slice(24)
      restored.set(chunk, offset)
      offset += chunk.length
    }
    expect(restored).toEqual(payload)
  })

  it('rejects unnecessary and oversized fragmentation', () => {
    expect(() => fragmentPeerSignal(1n, new Uint8Array(MAX_DIRECT_RECORD_PLAINTEXT))).toThrow('bounds')
    expect(() => fragmentPeerSignal(1n, new Uint8Array((64 << 10) + 1))).toThrow('bounds')
  })
})
