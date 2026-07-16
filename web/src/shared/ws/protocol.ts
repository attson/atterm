// protocol.ts — pure-TS port of internal/proto/codec.go.
//
// Wire compatibility with the relay is AGENTS.md red-line 4 — every
// change here MUST be paired with a regeneration of
// web/tests/fixtures/proto/*.bin via `go run ./cmd/proto-fixtures`,
// and the round-trip tests in protocol.test.ts must stay green.

export const VERSION = 1
export const HEADER_LEN = 6   // ver(1) + type(1) + payload_len(4 BE)
export const SID_LEN = 16     // uuid bytes
export const MIN_FRAME_LEN = HEADER_LEN + SID_LEN

// Frame types — every value mirrors internal/proto/frame.go.
export const TYPE = {
  OPEN:             0x01,
  IN:               0x02,
  OUT:              0x03,
  RESIZE:           0x04,
  META:             0x05,
  CLOSE:            0x06,
  ATTACH:           0x10,
  LIST:             0x11,
  LIST_RESP:        0x12,
  REPLAY_PROGRESS:  0x13,
  PING:             0x20,
  PONG:             0x21,
  ANNOUNCE:         0x30,
  STREAM_REQUEST:   0x31,
  STREAM_STOP:      0x32,
  PASTE_IMAGE:      0x33,
  CLAIM_DRIVER:     0x34,
  COMMAND_EVENT:    0x35,
  PASTE_FILE:       0x37,
  FS_REQUEST:       0x38,
  FS_RESPONSE:      0x39,
  FS_EVENT:         0x3a,
} as const

export type FrameType = (typeof TYPE)[keyof typeof TYPE]

export interface DecodedFrame {
  type: number
  sid: Uint8Array  // 16 bytes
  payload: Uint8Array
}

const NIL_SID = new Uint8Array(16)

export function encodeFrame(type: number, sid: Uint8Array | null, payload?: Uint8Array): Uint8Array {
  const p = payload ?? new Uint8Array(0)
  const buf = new Uint8Array(HEADER_LEN + SID_LEN + p.length)
  const dv = new DataView(buf.buffer)
  buf[0] = VERSION
  buf[1] = type
  dv.setUint32(2, p.length, false)
  buf.set(sid ?? NIL_SID, HEADER_LEN)
  buf.set(p, HEADER_LEN + SID_LEN)
  return buf
}

export function decodeFrame(data: Uint8Array): DecodedFrame {
  if (data.length < MIN_FRAME_LEN) {
    throw new Error('proto: frame too short')
  }
  if (data[0] !== VERSION) {
    throw new Error(`proto: unsupported version ${data[0]}`)
  }
  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength)
  const plen = dv.getUint32(2, false)
  if (MIN_FRAME_LEN + plen !== data.length) {
    throw new Error(`proto: payload length mismatch (claimed ${plen}, have ${data.length - MIN_FRAME_LEN})`)
  }
  return {
    type: data[1]!,
    sid: data.slice(HEADER_LEN, HEADER_LEN + SID_LEN),
    payload: data.slice(MIN_FRAME_LEN),
  }
}

// OUT payload is a u64 BE seq prefix followed by the byte data.
export function encodeOut(seq: number, data: Uint8Array): Uint8Array {
  const buf = new Uint8Array(8 + data.length)
  const dv = new DataView(buf.buffer)
  // Split u64 across two u32 to dodge JS number precision (safe through 2^53).
  dv.setUint32(0, Math.floor(seq / 0x100000000), false)
  dv.setUint32(4, seq >>> 0, false)
  buf.set(data, 8)
  return buf
}

export function decodeOut(payload: Uint8Array): { seq: number; data: Uint8Array } {
  if (payload.length < 8) {
    throw new Error('proto: OUT payload too short')
  }
  const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  const hi = dv.getUint32(0, false)
  const lo = dv.getUint32(4, false)
  return { seq: hi * 0x100000000 + lo, data: payload.slice(8) }
}

// RESIZE payload is two u16 BE: cols, rows.
export function encodeResize(cols: number, rows: number): Uint8Array {
  const buf = new Uint8Array(4)
  const dv = new DataView(buf.buffer)
  dv.setUint16(0, cols, false)
  dv.setUint16(2, rows, false)
  return buf
}

export function decodeResize(payload: Uint8Array): [number, number] {
  if (payload.length < 4) {
    throw new Error('proto: RESIZE payload too short')
  }
  const dv = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  return [dv.getUint16(0, false), dv.getUint16(2, false)]
}

// UUID helpers — sessions travel as 16 raw bytes on the wire but strings
// in JSON payloads. uuidToBytes parses canonical 8-4-4-4-12 hex; bytesToUuid
// formats raw 16-byte arrays into that hex form.
export function uuidToBytes(s: string): Uint8Array {
  const hex = s.replace(/-/g, '')
  if (!/^[0-9a-fA-F]{32}$/.test(hex)) {
    throw new Error(`proto: invalid uuid ${s}`)
  }
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) {
    out[i] = parseInt(hex.substr(i * 2, 2), 16)
  }
  return out
}

export function bytesToUuid(bytes: Uint8Array): string {
  if (bytes.length !== 16) {
    throw new Error('proto: uuid bytes must be length 16')
  }
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20, 32),
  ].join('-')
}
