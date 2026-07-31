import { describe, it, expect, vi } from 'vitest'
import { MockSocket } from './mockSocket'
import { encodeFrame, decodeFrame, decodeOutPayload, decodeText, TYPE, uuidParse, encodeText } from '@/lib/proto'
import { IDLE_SESSION_ID } from './fakeSessions'

function collect(sock: MockSocket): Uint8Array[] {
  const frames: Uint8Array[] = []
  sock.onmessage = (ev: any) => frames.push(new Uint8Array(ev.data))
  return frames
}

describe('MockSocket', () => {
  it('pushes a LIST_RESP snapshot on the session-list connection', async () => {
    const sock = new MockSocket('wss://demo/client-sessions')
    const frames = collect(sock)
    await vi.waitFor(() => expect(frames.length).toBeGreaterThan(0))
    const f = decodeFrame(frames[0])
    expect(f.type).toBe(TYPE.LIST_RESP)
    const sessions = JSON.parse(decodeText(f.payload))
    expect(Array.isArray(sessions)).toBe(true)
    expect(sessions[0].id).toBeTruthy() // SessionInfo uses `id`
  })

  it('replays script frames as OUT after ATTACH on a client connection', async () => {
    const sock = new MockSocket('wss://demo/client')
    const frames = collect(sock)
    // 等 open
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    sock.send(encodeFrame(TYPE.ATTACH, sid, encodeText('{}')))
    await vi.waitFor(() => expect(frames.length).toBeGreaterThan(0), { timeout: 2000 })
    const f = decodeFrame(frames[0])
    expect(f.type).toBe(TYPE.OUT)
    const { data } = decodeOutPayload(f.payload)
    expect(decodeText(data).length).toBeGreaterThan(0)
  })

  it('echoes input on the idle session', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('x')))
    await vi.waitFor(() => expect(frames.length).toBeGreaterThan(0))
    const { data } = decodeOutPayload(decodeFrame(frames[0]).payload)
    expect(decodeText(data)).toBe('x')
  })
})
