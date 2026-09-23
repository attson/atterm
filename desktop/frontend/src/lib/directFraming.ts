import { MAX_DIRECT_RECORD_PLAINTEXT } from './directCrypto'

const FRAGMENT_HEADER_SIZE = 16
const FRAGMENT_DATA_SIZE = MAX_DIRECT_RECORD_PLAINTEXT - FRAGMENT_HEADER_SIZE

export const MAX_DIRECT_FRAME_SIZE = 16 * 1024 * 1024
export const DIRECT_REASSEMBLY_TIMEOUT_MS = 10_000

export function fragmentDirectFrame(messageId: bigint, frame: Uint8Array): Uint8Array[] {
  if (messageId < 0n || messageId > 0xffffffffffffffffn) throw new Error('invalid direct fragment message id')
  if (frame.length <= MAX_DIRECT_RECORD_PLAINTEXT) throw new Error('direct frame does not require fragmentation')
  if (frame.length > MAX_DIRECT_FRAME_SIZE) throw new Error('direct frame too large')
  const fragments: Uint8Array[] = []
  for (let offset = 0; offset < frame.length; offset += FRAGMENT_DATA_SIZE) {
    const end = Math.min(frame.length, offset + FRAGMENT_DATA_SIZE)
    const fragment = new Uint8Array(FRAGMENT_HEADER_SIZE + end - offset)
    const view = new DataView(fragment.buffer)
    view.setBigUint64(0, messageId, false)
    view.setUint32(8, offset, false)
    view.setUint32(12, frame.length, false)
    fragment.set(frame.subarray(offset, end), FRAGMENT_HEADER_SIZE)
    fragments.push(fragment)
  }
  return fragments
}

/** Reassembles one ordered message at a time. A mismatch is terminal for the
 * direct route; reset exists for teardown, not in-band recovery. */
export class DirectFrameReassembler {
  private messageId = 0n
  private total = 0
  private next = 0
  private startedAtMs = 0
  private buffer: Uint8Array | null = null

  add(fragment: Uint8Array, nowMs = Date.now()): Uint8Array | null {
    if (this.buffer && nowMs - this.startedAtMs > DIRECT_REASSEMBLY_TIMEOUT_MS) {
      this.reset()
      throw new Error('direct reassembly timeout')
    }
    if (fragment.length <= FRAGMENT_HEADER_SIZE || fragment.length > MAX_DIRECT_RECORD_PLAINTEXT) {
      throw new Error('invalid direct fragment size')
    }
    const view = new DataView(fragment.buffer, fragment.byteOffset, fragment.byteLength)
    const messageId = view.getBigUint64(0, false)
    const offset = view.getUint32(8, false)
    const total = view.getUint32(12, false)
    const chunk = fragment.subarray(FRAGMENT_HEADER_SIZE)
    if (total <= MAX_DIRECT_RECORD_PLAINTEXT || total > MAX_DIRECT_FRAME_SIZE || offset + chunk.length > total) {
      throw new Error('invalid direct fragment bounds')
    }
    if (!this.buffer) {
      if (offset !== 0) throw new Error('invalid direct fragment offset')
      this.messageId = messageId
      this.total = total
      this.startedAtMs = nowMs
      this.buffer = new Uint8Array(total)
    }
    if (messageId !== this.messageId || total !== this.total || offset !== this.next) {
      throw new Error('direct fragment ordering mismatch')
    }
    this.buffer.set(chunk, offset)
    this.next += chunk.length
    if (this.next !== this.total) return null
    const frame = this.buffer
    this.reset()
    return frame
  }

  reset(): void {
    this.messageId = 0n
    this.total = 0
    this.next = 0
    this.startedAtMs = 0
    this.buffer = null
  }
}
