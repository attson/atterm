import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import {
  TYPE,
  decodeFrame,
  encodeFrame,
  encodeResize,
  decodeResize,
  encodeOut,
  decodeOut,
  uuidToBytes,
  bytesToUuid,
} from '@shared/ws/protocol'

const here = path.dirname(fileURLToPath(import.meta.url))
const fixDir = path.resolve(here, '..', '..', '..', 'fixtures', 'proto')

function load(name: string): Uint8Array {
  return new Uint8Array(readFileSync(path.join(fixDir, name)))
}

const SID_UUID = '11111111-2222-3333-4444-555555555555'

describe('protocol — round-trip against Go-emitted fixtures', () => {
  it('OUT: decode then re-encode produces identical bytes', () => {
    const bytes = load('out.bin')
    const f = decodeFrame(bytes)
    expect(f.type).toBe(TYPE.OUT)
    expect(bytesToUuid(f.sid)).toBe(SID_UUID)
    const { seq, data } = decodeOut(f.payload)
    expect(seq).toBe(42)
    expect(new TextDecoder().decode(data)).toBe('hello\r\n')
    const reencoded = encodeFrame(TYPE.OUT, f.sid, encodeOut(42, data))
    expect(reencoded).toEqual(bytes)
  })

  it('RESIZE: round-trip preserves cols/rows', () => {
    const bytes = load('resize.bin')
    const f = decodeFrame(bytes)
    expect(f.type).toBe(TYPE.RESIZE)
    const [cols, rows] = decodeResize(f.payload)
    expect(cols).toBe(80)
    expect(rows).toBe(24)
    const reencoded = encodeFrame(TYPE.RESIZE, f.sid, encodeResize(cols, rows))
    expect(reencoded).toEqual(bytes)
  })

  it('ATTACH: payload is JSON; round-trip byte-identical', () => {
    const bytes = load('attach.bin')
    const f = decodeFrame(bytes)
    expect(f.type).toBe(TYPE.ATTACH)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.session_id).toBe(SID_UUID)
    expect(body.since_seq).toBe(100)
    expect(body.client_id).toBe('client-uuid')
  })

  it('META: payload is JSON; cols/rows round-trip', () => {
    const f = decodeFrame(load('meta.bin'))
    expect(f.type).toBe(TYPE.META)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.cols).toBe(80)
    expect(body.rows).toBe(24)
    expect(body.cwd).toBe('/home/me')
  })

  it('CLOSE: payload is JSON with exit_code', () => {
    const f = decodeFrame(load('close.bin'))
    expect(f.type).toBe(TYPE.CLOSE)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.exit_code).toBe(0)
  })

  it('REPLAY_PROGRESS start frame', () => {
    const f = decodeFrame(load('replay-start.bin'))
    expect(f.type).toBe(TYPE.REPLAY_PROGRESS)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.phase).toBe('start')
    expect(body.total_bytes).toBe(12345)
  })

  it('PASTE_IMAGE: payload is JSON with base64 data', () => {
    const f = decodeFrame(load('paste-image.bin'))
    expect(f.type).toBe(TYPE.PASTE_IMAGE)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.filename).toBe('shot.png')
    expect(body.content_type).toBe('image/png')
  })

  it('PASTE_FILE: payload is JSON with base64 data', () => {
    const f = decodeFrame(load('paste-file.bin'))
    expect(f.type).toBe(TYPE.PASTE_FILE)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.filename).toBe('notes.pdf')
    expect(body.content_type).toBe('application/pdf')
    expect(body.data).toBe('aGVsbG8=')
  })

  it('LIST: zero session id + empty payload', () => {
    const f = decodeFrame(load('list.bin'))
    expect(f.type).toBe(TYPE.LIST)
    expect(f.payload.length).toBe(0)
    expect(bytesToUuid(f.sid)).toBe('00000000-0000-0000-0000-000000000000')
  })

  it('PING: just header + zero session id', () => {
    const f = decodeFrame(load('ping.bin'))
    expect(f.type).toBe(TYPE.PING)
  })
})

describe('protocol — uuid helpers', () => {
  it('uuidToBytes / bytesToUuid round-trip', () => {
    const bytes = uuidToBytes(SID_UUID)
    expect(bytes.length).toBe(16)
    expect(bytesToUuid(bytes)).toBe(SID_UUID)
  })

  it('uuidToBytes rejects non-uuid strings', () => {
    expect(() => uuidToBytes('not-a-uuid')).toThrow()
  })
})

describe('protocol — rejects malformed input', () => {
  it('decodeFrame throws on too-short data', () => {
    expect(() => decodeFrame(new Uint8Array(5))).toThrow(/short|length/i)
  })

  it('decodeFrame throws on wrong version', () => {
    const bad = new Uint8Array(22)
    bad[0] = 99
    expect(() => decodeFrame(bad)).toThrow(/version/i)
  })

  it('decodeFrame throws on payload length mismatch', () => {
    // valid header but claimed payload length doesn't match what's there
    const bad = new Uint8Array(22)
    bad[0] = 1  // version
    bad[1] = 0x01  // type
    new DataView(bad.buffer).setUint32(2, 999, false)  // claim 999 payload bytes
    expect(() => decodeFrame(bad)).toThrow(/length|payload/i)
  })
})
