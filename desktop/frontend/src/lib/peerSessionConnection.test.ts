import { beforeEach, describe, expect, it, vi } from 'vitest'
import { decodeFrame, decodeText, encodeFrame, encodeText, TYPE, uuidParse } from './proto'
import type { DirectTransport } from './connection'
import type { NativeDirectClientOptions } from './nativeDirectClient'
import { PeerSessionConnection } from './peerSessionConnection'

const sessionID = '11111111-2222-3333-4444-555555555555'

function out(seq: number, text: string): Uint8Array {
  const data = encodeText(text)
  const payload = new Uint8Array(8 + data.length)
  const view = new DataView(payload.buffer)
  view.setUint32(0, Math.floor(seq / 0x100000000), false)
  view.setUint32(4, seq >>> 0, false)
  payload.set(data, 8)
  return encodeFrame(TYPE.OUT, uuidParse(sessionID), payload)
}

class FakeTransport implements DirectTransport {
  sent: Uint8Array[] = []
  closed = false

  constructor(readonly options: NativeDirectClientOptions) {}
  start(): void {}
  sendFrame(frame: Uint8Array): boolean {
    if (this.closed) return false
    this.sent.push(frame)
    return true
  }
  close(): void { this.closed = true }
}

describe('PeerSessionConnection', () => {
  beforeEach(() => { vi.useFakeTimers() })

  it('attaches without Relay, replays once, and flushes queued terminal writes', () => {
    const transports: FakeTransport[] = []
    const outputs: string[] = []
    const statuses: string[] = []
    const connection = new PeerSessionConnection(sessionID, {
      onOutput: (data) => outputs.push(decodeText(data)),
      onStatus: (status) => statuses.push(status),
    }, {
      clientName: 'client-a',
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    connection.sendInput('queued')
    expect(transports).toHaveLength(1)
    expect(transports[0].options.accountKey).toBeUndefined()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onFrame(out(4, 'hello'))
    transports[0].options.callbacks.onFrame(out(4, 'duplicate'))
    transports[0].options.callbacks.onReady(4)

    expect(outputs).toEqual(['hello'])
    expect(statuses).toEqual(['connecting', 'attached'])
    const sent = transports[0].sent.map(decodeFrame)
    expect(sent.map((frame) => frame.type)).toContain(TYPE.IN)
    expect(decodeText(sent.find((frame) => frame.type === TYPE.IN)!.payload)).toBe('queued')
  })

  it('reconnects through Peer transport with the committed output cursor', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })
    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onFrame(out(8, 'latest'))
    transports[0].options.callbacks.onReady(8)
    transports[0].options.callbacks.onFailure(new Error('route lost'))

    await vi.advanceTimersByTimeAsync(500)
    expect(transports).toHaveLength(2)
    expect(transports[1].options.sinceSeq).toBe(8)
  })

  it('resumes a suspended pane and uses the host replay cursor', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })
    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(12)

    connection.suspend()
    expect(transports[0].closed).toBe(true)
    connection.attach()

    expect(transports).toHaveLength(2)
    expect(transports[1].options.sinceSeq).toBe(12)
  })
})
