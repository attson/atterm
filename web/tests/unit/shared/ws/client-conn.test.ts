import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { SessionConnection } from '@shared/ws/client-conn'
import { TYPE, decodeFrame, encodeFrame, encodeOut, encodeResize, uuidToBytes } from '@shared/ws/protocol'
import { saveRelayConfig, clearRelayConfig } from '@shared/api/relay-config'

const SID = '11111111-2222-3333-4444-555555555555'
const SID_BYTES = uuidToBytes(SID)

class MockWebSocket {
  static OPEN = 1
  static CLOSED = 3
  static CONNECTING = 0
  readyState = MockWebSocket.CONNECTING
  binaryType = 'arraybuffer' as BinaryType
  url: string
  protocols: string | string[] | undefined
  sent: Uint8Array[] = []
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null

  constructor(url: string, protocols?: string | string[]) {
    this.url = url
    this.protocols = protocols
  }

  send(data: ArrayBuffer | ArrayBufferView): void {
    if (data instanceof Uint8Array) {
      this.sent.push(data)
    } else if (data instanceof ArrayBuffer) {
      this.sent.push(new Uint8Array(data))
    } else {
      this.sent.push(new Uint8Array((data as ArrayBufferView).buffer))
    }
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED
    queueMicrotask(() => this.onclose?.(new CloseEvent('close')))
  }

  fireOpen(): void {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  fireMessage(frameBytes: Uint8Array): void {
    this.onmessage?.(new MessageEvent('message', { data: frameBytes.buffer }))
  }

  fireServerClose(): void {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
}

let createdSockets: MockWebSocket[] = []
let originalWS: typeof WebSocket

beforeEach(() => {
  clearRelayConfig()
  createdSockets = []
  originalWS = globalThis.WebSocket
  vi.stubGlobal('WebSocket', class extends MockWebSocket {
    constructor(url: string, protocols?: string | string[]) {
      super(url, protocols)
      createdSockets.push(this)
    }
  } as unknown as typeof WebSocket)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.stubGlobal('WebSocket', originalWS)
})

describe('SessionConnection', () => {
  it('opens /client and sends ATTACH on open', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    expect(ws.url).toMatch(/\/client$/)

    ws.fireOpen()
    expect(ws.sent.length).toBeGreaterThan(0)
    const first = decodeFrame(ws.sent[0]!)
    expect(first.type).toBe(TYPE.ATTACH)
    const body = JSON.parse(new TextDecoder().decode(first.payload))
    expect(body.session_id).toBe(SID)
    expect(body.since_seq).toBe(0)
  })

  it('queues IN sent while CONNECTING and flushes on open', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!

    conn.sendInput('hello')
    expect(ws.sent.length).toBe(0)  // still CONNECTING

    ws.fireOpen()
    expect(ws.sent.length).toBe(2)
    const inFrame = decodeFrame(ws.sent[1]!)
    expect(inFrame.type).toBe(TYPE.IN)
    expect(new TextDecoder().decode(inFrame.payload)).toBe('hello')
  })

  it('drops a RESIZE that matches the last-sent dimensions', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    const sentBeforeResize = ws.sent.length

    conn.sendResize(80, 24)
    conn.sendResize(80, 24)  // duplicate — should NOT produce a second frame

    expect(ws.sent.length).toBe(sentBeforeResize + 1)
    const f = decodeFrame(ws.sent[sentBeforeResize]!)
    expect(f.type).toBe(TYPE.RESIZE)
  })

  it('sends a RESIZE when dimensions actually changed', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    const sentBeforeResize = ws.sent.length

    conn.sendResize(80, 24)
    conn.sendResize(120, 40)  // changed

    expect(ws.sent.length).toBe(sentBeforeResize + 2)
  })

  it('demultiplexes OUT frames to onOutput', () => {
    const onOutput = vi.fn()
    const conn = new SessionConnection(SID, { onOutput })
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()

    const outFrame = encodeFrame(TYPE.OUT, SID_BYTES, encodeOut(7, new TextEncoder().encode('echo')))
    ws.fireMessage(outFrame)

    expect(onOutput).toHaveBeenCalledTimes(1)
    const [data, seq] = onOutput.mock.calls[0]!
    expect(new TextDecoder().decode(data)).toBe('echo')
    expect(seq).toBe(7)
  })

  it('demultiplexes CLOSE frames to onClose and sets status to ended', () => {
    const onClose = vi.fn()
    const onStatus = vi.fn()
    const conn = new SessionConnection(SID, { onClose, onStatus })
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()

    const closePayload = new TextEncoder().encode(JSON.stringify({ exit_code: 0, reason: 'eof' }))
    ws.fireMessage(encodeFrame(TYPE.CLOSE, SID_BYTES, closePayload))

    expect(onClose).toHaveBeenCalledWith({ exit_code: 0, reason: 'eof' })
    expect(onStatus).toHaveBeenCalledWith('ended')
  })

  it('reconnects with exponential backoff on close', () => {
    const onStatus = vi.fn()
    const conn = new SessionConnection(SID, { onStatus })
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    onStatus.mockClear()

    ws.fireServerClose()
    expect(onStatus).toHaveBeenLastCalledWith('reconnecting')
    expect(createdSockets.length).toBe(1)  // no reconnect yet, backoff timer pending

    vi.advanceTimersByTime(500)  // first reconnect after 500ms
    expect(createdSockets.length).toBe(2)

    // Failing again doubles the delay.
    createdSockets[1]!.fireServerClose()
    vi.advanceTimersByTime(999)
    expect(createdSockets.length).toBe(2)  // not yet
    vi.advanceTimersByTime(1)
    expect(createdSockets.length).toBe(3)  // 1000ms total — second backoff
  })

  it('detach cancels the reconnect timer and closes the socket', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    ws.fireServerClose()

    conn.detach()

    vi.advanceTimersByTime(60_000)
    expect(createdSockets.length).toBe(1)  // no reconnects after detach
  })

  it('includes client_id and client_name in ATTACH', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    const body = JSON.parse(new TextDecoder().decode(decodeFrame(ws.sent[0]!).payload))
    expect(typeof body.client_id).toBe('string')
    expect(body.client_id.length).toBeGreaterThan(0)
    expect(body.client_name).toBe('web')
  })

  it('claimDriver() sends a CLAIM_DRIVER frame carrying our client_id', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    const attachBody = JSON.parse(new TextDecoder().decode(decodeFrame(ws.sent[0]!).payload))
    const before = ws.sent.length

    conn.claimDriver()

    expect(ws.sent.length).toBe(before + 1)
    const f = decodeFrame(ws.sent[before]!)
    expect(f.type).toBe(TYPE.CLAIM_DRIVER)
    const body = JSON.parse(new TextDecoder().decode(f.payload))
    expect(body.client_id).toBe(attachBody.client_id)
  })

  it('fires onDriverChange with isMe reflecting our own client_id', () => {
    const onDriverChange = vi.fn()
    const conn = new SessionConnection(SID, { onDriverChange })
    conn.attach()
    const ws = createdSockets[0]!
    ws.fireOpen()
    const ourId = JSON.parse(new TextDecoder().decode(decodeFrame(ws.sent[0]!).payload)).client_id

    const meta = (driver: string, name: string) =>
      encodeFrame(TYPE.META, SID_BYTES, new TextEncoder().encode(JSON.stringify({ driver_client_id: driver, driver_client_name: name })))

    ws.fireMessage(meta('owner-A', 'mac-mini'))
    expect(onDriverChange).toHaveBeenLastCalledWith('owner-A', false, 'mac-mini')

    ws.fireMessage(meta(ourId, 'web'))
    expect(onDriverChange).toHaveBeenLastCalledWith(ourId, true, 'web')
  })

  it('passes sessionToken as atterm-token.* subprotocol when available', () => {
    saveRelayConfig({
      baseURL: '',
      sessionToken: 'test_session_token_123',
      expiresAt: null,
      allowInsecure: false,
    })
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    expect(ws.protocols).toEqual(['atterm-token.test_session_token_123'])
  })

  it('passes empty subprotocols array when no sessionToken', () => {
    const conn = new SessionConnection(SID)
    conn.attach()
    const ws = createdSockets[0]!
    expect(ws.protocols).toEqual([])
  })
})
