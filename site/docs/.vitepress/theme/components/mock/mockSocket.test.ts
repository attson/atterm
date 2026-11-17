import { describe, it, expect, vi } from 'vitest'
import { installMockWebSocket, MockSocket } from './mockSocket'
import { encodeFrame, decodeFrame, decodeOutPayload, decodeText, TYPE, uuidParse, encodeText } from '@/lib/proto'
import { IDLE_SESSION_ID } from './fakeSessions'
import { clearMockInputHistory, readMockInputHistory } from './mockInputHistory'

function collect(sock: MockSocket): Uint8Array[] {
  const frames: Uint8Array[] = []
  sock.onmessage = (ev: any) => frames.push(new Uint8Array(ev.data))
  return frames
}

describe('MockSocket', () => {
  it('restores the previous WebSocket and notification callback', () => {
    const g = globalThis as any
    const originalSocket = g.WebSocket
    const originalNotify = MockSocket.onNotify
    const previousSocket = class PreviousSocket {}
    const previousNotify = () => undefined
    g.WebSocket = previousSocket
    MockSocket.onNotify = previousNotify

    const restore = installMockWebSocket()
    MockSocket.onNotify = () => undefined
    expect(g.WebSocket).toBe(MockSocket)

    restore()
    expect(g.WebSocket).toBe(previousSocket)
    expect(MockSocket.onNotify).toBe(previousNotify)

    g.WebSocket = originalSocket
    MockSocket.onNotify = originalNotify
  })

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

  it('finishes initial replay before accepting queued terminal input', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.ATTACH, sid, encodeText('{}')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('help\r')))

    await vi.waitFor(() => {
      const output = frames
        .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
        .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
        .join('')
      expect(output).toContain('demo shell')
      expect(output).toMatch(/^Last login:/)
      expect(output.indexOf('Last login')).toBeLessThan(output.indexOf('demo shell'))
    }, { timeout: 2500 })
    sock.close()
  })

  it('keeps rapid bulk actions on separate command lines', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.IN, sid, encodeText('release')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('journalctl --since "1 hour ago"')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('\r')))

    const output = frames
      .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
      .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
      .join('')
    expect(output).not.toContain('releasejournalctl')
    expect(output).toContain('command not found: release')
    expect(output).toContain('command not found: journalctl')
    sock.close()
  })

  it('edits the input line with left and right arrow sequences', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.IN, sid, encodeText('ac')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('\x1b[D')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('b')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('\x1b[C')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('\b')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('c\r')))

    const output = frames
      .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
      .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
      .join('')
    expect(output).toContain('command not found: abc')
    expect(output).not.toContain('command not found: \x1b')
    sock.close()
  })

  it('enters an interactive agent and returns to the shell on exit', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.IN, sid, encodeText('codex\r')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('review auth\r')))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('/exit\r')))

    const output = frames
      .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
      .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
      .join('')
    expect(output).toContain('Codex CLI')
    expect(output).toContain('interactive demo')
    expect(output).toContain('is working on: review auth')
    expect(output).toContain('Demo response ready')
    expect(output).toContain('Leaving Codex CLI demo')
    expect(output).not.toContain('3 files changed')
    sock.close()
  })

  it('delivers pasted images to an interactive agent without ending the conversation', async () => {
    clearMockInputHistory(IDLE_SESSION_ID)
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.IN, sid, encodeText('codex\r')))
    sock.send(encodeFrame(TYPE.PASTE_IMAGE, sid, encodeText(JSON.stringify({
      filename: '../demo-shot.png',
      content_type: 'image/png',
      data: 'aGVsbG8=',
    }))))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('describe this image\r')))

    const output = frames
      .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
      .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
      .join('')
    expect(output).toContain('[Image #1]')
    expect(output).not.toContain('demo-shot.png')
    expect(output).not.toContain('received the image')
    expect(output).toContain('is working on: describe this image')
    expect(output).not.toContain('../demo-shot.png')
    expect(readMockInputHistory(IDLE_SESSION_ID).items[0]?.text)
      .toBe('[Image #1] describe this image')
    sock.close()
    clearMockInputHistory(IDLE_SESSION_ID)
  })

  it('reports invalid image payloads without crashing the demo session', async () => {
    const sock = new MockSocket('wss://demo/client')
    await vi.waitFor(() => expect(sock.readyState).toBe(MockSocket.OPEN))
    const sid = uuidParse(IDLE_SESSION_ID)
    const frames = collect(sock)

    sock.send(encodeFrame(TYPE.PASTE_IMAGE, sid, encodeText('{bad json')))
    sock.send(encodeFrame(TYPE.PASTE_IMAGE, sid, encodeText(JSON.stringify({
      filename: 'empty.png',
      content_type: 'image/png',
      data: '',
    }))))
    sock.send(encodeFrame(TYPE.PASTE_IMAGE, sid, encodeText(JSON.stringify({
      filename: 'unsafe.png',
      content_type: 'image/png\x1b[2J',
      data: 'aGVsbG8=',
    }))))
    sock.send(encodeFrame(TYPE.IN, sid, encodeText('help\r')))

    const output = frames
      .filter((bytes) => decodeFrame(bytes).type === TYPE.OUT)
      .map((bytes) => decodeText(decodeOutPayload(decodeFrame(bytes).payload).data))
      .join('')
    expect(output.match(/Image paste failed: invalid image payload/g)).toHaveLength(3)
    expect(output).not.toContain('\x1b[2J')
    expect(output).toContain('demo shell')
    sock.close()
  })
})
