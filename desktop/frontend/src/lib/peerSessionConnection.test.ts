import { beforeEach, describe, expect, it, vi } from 'vitest'
import { decodeFrame, decodeText, encodeFrame, encodeText, TYPE, uuidParse } from './proto'
import type { DirectTransport } from './connection'
import { decodeSegments, encodeSegments } from './fsSegments'
import type { NativeDirectClientOptions } from './nativeDirectClient'
import { PeerSessionConnection } from './peerSessionConnection'
import { Terminal } from 'xterm'
import { createTerminalWriteQueue } from './terminalWriteQueue'
import { createReplayInputGuard } from './replayInputGuard'

const sessionID = '11111111-2222-3333-4444-555555555555'

function testBlob(value: string, type: string): Blob {
  const bytes = encodeText(value)
  return {
    size: bytes.length,
    type,
    arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
  } as Blob
}

function out(seq: number, text: string): Uint8Array {
  const data = encodeText(text)
  const payload = new Uint8Array(8 + data.length)
  const view = new DataView(payload.buffer)
  view.setUint32(0, Math.floor(seq / 0x100000000), false)
  view.setUint32(4, seq >>> 0, false)
  payload.set(data, 8)
  return encodeFrame(TYPE.OUT, uuidParse(sessionID), payload)
}

function meta(driverClientID: string, driverClientName = ''): Uint8Array {
  return encodeFrame(
    TYPE.META,
    uuidParse(sessionID),
    encodeText(JSON.stringify({
      driver_client_id: driverClientID,
      driver_client_name: driverClientName,
    })),
  )
}

class FakeTransport implements DirectTransport {
  sent: Uint8Array[] = []
  closed = false
  failSend = false
  readonly id = `attempt-${crypto.randomUUID()}`

  constructor(readonly options: NativeDirectClientOptions) {}
  start(): void {}
  sendFrame(frame: Uint8Array): boolean {
    if (this.closed || this.failSend) return false
    this.sent.push(frame)
    return true
  }
  close(): void { this.closed = true }
  nativeAttemptId(): string { return this.id }
}

describe('PeerSessionConnection', () => {
  for (const route of ['direct', 'quick_tunnel'] as const) {
    for (const newOutput of [false, true]) {
      it(`scrolls a resumed ${route} terminal to the latest line ${newOutput ? 'after replay' : 'without new output'}`, async () => {
        vi.useRealTimers()
        vi.stubGlobal('matchMedia', () => ({ matches: false, addListener() {}, removeListener() {} }))
        const terminal = new Terminal({ rows: 5, cols: 40, scrollback: 1000 })
        const container = document.createElement('div')
        document.body.append(container)
        terminal.open(container)
        const queue = createTerminalWriteQueue(terminal)
        const guard = createReplayInputGuard()
        const transports: FakeTransport[] = []
        const connection = new PeerSessionConnection(sessionID, {
          onOutput: (data) => queue.write(data),
          onReplayProgress: (progress) => guard.onProgress(progress.phase, (release) => {
            queue.drain(() => {
              terminal.scrollToBottom()
              release()
            })
          }),
        }, {
          route,
          transportFactory: (options) => {
            const transport = new FakeTransport(options)
            transports.push(transport)
            return transport
          },
        })

        try {
          connection.attach()
          const initial = transports[0]
          initial.options.callbacks.onAuthenticated?.()
          initial.options.callbacks.onFrame(out(1, 'older output\r\n'.repeat(20) + 'latest before switching'))
          initial.options.callbacks.onReady(1)
          await new Promise<void>((resolve) => queue.drain(resolve))
          expect(terminal.buffer.active.baseY).toBeGreaterThan(0)
          terminal.scrollToTop()
          expect(terminal.buffer.active.viewportY).toBe(0)
          connection.suspend()

          connection.attach()
          const resumed = transports[1]
          resumed.options.callbacks.onAuthenticated?.()
          if (newOutput) resumed.options.callbacks.onFrame(out(2, '\r\nlatest while hidden'))
          resumed.options.callbacks.onReady(newOutput ? 2 : 1)
          await new Promise<void>((resolve) => queue.drain(resolve))

          const buffer = terminal.buffer.active
          expect(buffer.viewportY).toBe(buffer.baseY)
          expect(buffer.getLine(buffer.baseY + buffer.cursorY)?.translateToString(true))
            .toBe(newOutput ? 'latest while hidden' : 'latest before switching')

          // Completing replay is a one-time snap; subsequent output must still
          // let the user read earlier history without pulling them back down.
          terminal.scrollToTop()
          resumed.options.callbacks.onFrame(out(3, '\r\nlive output'))
          await new Promise<void>((resolve) => queue.drain(resolve))
          expect(buffer.viewportY).toBe(0)
        } finally {
          connection.detach()
          terminal.dispose()
          container.remove()
          vi.unstubAllGlobals()
        }
      })
    }
  }

  it('holds resumed resize and input until a manual driver claim is confirmed', () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const initial = transports[0]
    const clientID = initial.options.clientInstanceId
    initial.options.callbacks.onAuthenticated?.()
    initial.options.callbacks.onFrame(meta(clientID, 'this-device'))
    initial.options.callbacks.onReady(0)

    expect(connection.beginHistoryPaste()).toBeTypeOf('function')

    connection.suspend()
    connection.sendResize(132, 41)
    connection.sendInput('typed-during-reveal')
    connection.attach()
    const resumed = transports[1]
    resumed.options.callbacks.onAuthenticated?.()
    resumed.options.callbacks.onFrame(meta('other-client', 'phone'))
    resumed.options.callbacks.onReady(0)

    expect(connection.beginHistoryPaste()).toBeNull()

    expect(resumed.sent).toHaveLength(0)

    expect(connection.claimDriver()).toBe(true)
    resumed.options.callbacks.onFrame(meta(clientID, 'this-device'))

    const sent = resumed.sent.map(decodeFrame)
    expect(sent.map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
      TYPE.RESIZE,
      TYPE.IN,
    ])
    expect(decodeText(sent[2].payload)).toBe('typed-during-reveal')
  })

  it('reclaims a previously owned Peer driver only when replay confirms it is vacant', () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const initial = transports[0]
    const clientID = initial.options.clientInstanceId
    initial.options.callbacks.onAuthenticated?.()
    initial.options.callbacks.onFrame(meta(clientID, 'this-device'))
    initial.options.callbacks.onReady(0)

    connection.suspend()
    connection.sendResize(132, 41)
    connection.sendInput('typed-during-reveal')
    connection.attach()
    const resumed = transports[1]
    resumed.options.callbacks.onAuthenticated?.()
    resumed.options.callbacks.onFrame(meta(''))
    resumed.options.callbacks.onReady(0)

    expect(resumed.sent.map(decodeFrame).map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
    ])

    resumed.options.callbacks.onFrame(meta(clientID, 'this-device'))
    const sent = resumed.sent.map(decodeFrame)
    expect(sent.map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
      TYPE.RESIZE,
      TYPE.IN,
    ])
    expect(decodeText(sent[2].payload)).toBe('typed-during-reveal')
  })

  it('opens and closes Preview over Peer control frames without Relay keys or tickets', async () => {
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
    transports[0].options.callbacks.onReady(0)

    const opened = connection.openService(3000, '127.0.0.1')
    const requestFrame = decodeFrame(transports[0].sent.at(-1)!)
    const request = JSON.parse(decodeText(requestFrame.payload))
    expect(requestFrame.type).toBe(TYPE.SERVICE_OPEN)
    expect(request).toEqual({
      request_id: expect.any(String),
      service_id: expect.any(String),
      peer_fields: { port: 3000, scheme: 'http', host: '127.0.0.1' },
    })
    expect(request).not.toHaveProperty('sealed')
    expect(request).not.toHaveProperty('host_ticket')

    transports[0].options.callbacks.onFrame(encodeFrame(
      TYPE.SERVICE_OPENED,
      uuidParse(sessionID),
      encodeText(JSON.stringify({ request_id: request.request_id, service_id: request.service_id, ok: true })),
    ))
    await expect(opened).resolves.toEqual({
      serviceId: request.service_id,
      clientTicket: '',
      clientToHostKey: new Uint8Array(),
      hostToClientKey: new Uint8Array(),
      peerAttemptId: transports[0].id,
    })

    connection.closeService(request.service_id)
    const closeFrame = decodeFrame(transports[0].sent.at(-1)!)
    expect(closeFrame.type).toBe(TYPE.SERVICE_CLOSE)
    expect(JSON.parse(decodeText(closeFrame.payload))).toEqual({ service_id: request.service_id })
  })

  it('rejects a pending Peer Preview open when its route generation changes', async () => {
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
    transports[0].options.callbacks.onReady(0)
    const pending = connection.openService(3000)
    connection.setRoute('quick_tunnel')
    await expect(pending).rejects.toThrow(/route changed/i)
  })

  it('routes plaintext FS responses and events over the encrypted Peer record', async () => {
    const transports: FakeTransport[] = []
    const events: unknown[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })
    connection.onFSEvent((event) => events.push(event))
    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)

    const response = connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'peer-fs-1' })
    const requestFrame = decodeFrame(transports[0].sent[0])
    expect(requestFrame.type).toBe(TYPE.FS_REQUEST)
    expect(JSON.parse(decodeText(decodeSegments(requestFrame.payload)![0]))).toEqual({
      op: 'list_dir',
      path: '/tmp',
      request_id: 'peer-fs-1',
    })

    transports[0].options.callbacks.onFrame(encodeFrame(
      TYPE.FS_EVENT,
      uuidParse(sessionID),
      encodeSegments([encodeText(JSON.stringify({ watch_id: 'watch-1', path: '/tmp', event: 'changed' }))]),
    ))
    transports[0].options.callbacks.onFrame(encodeFrame(
      TYPE.FS_RESPONSE,
      uuidParse(sessionID),
      encodeSegments([encodeText(JSON.stringify({ request_id: 'peer-fs-1', ok: true, entries: [] }))]),
    ))

    await expect(response).resolves.toEqual({ request_id: 'peer-fs-1', ok: true, entries: [] })
    expect(events).toEqual([{ watch_id: 'watch-1', path: '/tmp', event: 'changed' }])
  })

  it('rejects duplicate, timed-out, and failed Peer FS sends without leaking request IDs', async () => {
    vi.useFakeTimers()
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
    transports[0].options.callbacks.onReady(0)

    const first = connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'peer-duplicate' }, 10)
    await expect(connection.sendFSRequest({ op: 'file_meta', path: '/tmp/a', request_id: 'peer-duplicate' }, 10))
      .rejects.toThrow(/duplicate/i)
    const firstRejection = expect(first).rejects.toThrow(/timed out/i)
    await vi.advanceTimersByTimeAsync(10)
    await firstRejection
    await expect(connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'peer-duplicate' }, 10))
      .rejects.toThrow(/retired/i)

    transports[0].failSend = true
    await expect(connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'peer-send-fail' }))
      .rejects.toThrow(/send failed/i)
    transports[0].failSend = false
    const retry = connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'peer-send-fail' })
    transports[0].options.callbacks.onFrame(encodeFrame(
      TYPE.FS_RESPONSE,
      uuidParse(sessionID),
      encodeSegments([encodeText(JSON.stringify({ request_id: 'peer-send-fail', ok: true, entries: [] }))]),
    ))
    await expect(retry).resolves.toMatchObject({ request_id: 'peer-send-fail', ok: true })
  })

  it('rejects in-flight FS requests and drops stale responses across route generations', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })
    connection.attach()
    const oldRoute = transports[0]
    oldRoute.options.callbacks.onAuthenticated?.()
    oldRoute.options.callbacks.onReady(0)
    const pending = connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'old-route' })

    connection.setRoute('quick_tunnel')
    await expect(pending).rejects.toThrow(/route changed/i)
    oldRoute.options.callbacks.onFrame(encodeFrame(
      TYPE.FS_RESPONSE,
      uuidParse(sessionID),
      encodeSegments([encodeText(JSON.stringify({ request_id: 'old-route', ok: true, entries: [] }))]),
    ))

    const replacement = transports[1]
    replacement.options.callbacks.onAuthenticated?.()
    replacement.options.callbacks.onReady(0)
    const current = connection.sendFSRequest({ op: 'list_dir', path: '/tmp', request_id: 'new-route' })
    replacement.options.callbacks.onFrame(encodeFrame(
      TYPE.FS_RESPONSE,
      uuidParse(sessionID),
      encodeSegments([encodeText(JSON.stringify({ request_id: 'new-route', ok: true, entries: [] }))]),
    ))
    await expect(current).resolves.toMatchObject({ request_id: 'new-route', ok: true })
  })

  it('sends image and file paste frames only after the Peer route is ready', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    await expect(connection.sendPasteImage(testBlob('png', 'image/png'), 'clip.png'))
      .rejects.toThrow(/not ready/i)
    expect(transports[0].sent).toHaveLength(0)

    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)
    await expect(connection.sendPasteImage(testBlob('png', 'image/png'), 'clip.png'))
      .resolves.toBe(true)
    await expect(connection.sendPasteFile(testBlob('notes', 'text/plain'), 'notes.txt'))
      .resolves.toBe(true)

    const frames = transports[0].sent.map(decodeFrame)
    expect(frames.map((frame) => frame.type)).toEqual([TYPE.PASTE_IMAGE, TYPE.PASTE_FILE])
    expect(JSON.parse(decodeText(frames[0].payload))).toEqual({
      filename: 'clip.png',
      content_type: 'image/png',
      data: 'cG5n',
    })
    expect(JSON.parse(decodeText(frames[1].payload))).toEqual({
      filename: 'notes.txt',
      content_type: 'text/plain',
      data: 'bm90ZXM=',
    })
  })

  it('rejects oversized paste and does not report a failed transport send as success', async () => {
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
    transports[0].options.callbacks.onReady(0)

    await expect(connection.sendPasteFile(new Blob([new Uint8Array(10 * 1024 * 1024 + 1)]), 'large.bin'))
      .rejects.toThrow(/too large/i)
    expect(transports[0].sent).toHaveLength(0)

    transports[0].closed = true
    await expect(connection.sendPasteImage(testBlob('png', 'image/png'), 'clip.png'))
      .rejects.toThrow(/send failed/i)
    expect(transports[0].sent).toHaveLength(0)
  })

  it('does not move an in-flight paste across a route generation change', async () => {
    const transports: FakeTransport[] = []
    let finishRead!: (buffer: ArrayBuffer) => void
    const blob = {
      size: 3,
      type: 'image/png',
      arrayBuffer: () => new Promise<ArrayBuffer>((resolve) => { finishRead = resolve }),
    } as Blob
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)
    const pending = connection.sendPasteImage(blob, 'clip.png')
    connection.setRoute('quick_tunnel')
    finishRead(encodeText('png').buffer as ArrayBuffer)

    await expect(pending).rejects.toThrow(/send failed/i)
    expect(transports).toHaveLength(2)
    expect(transports[0].sent).toHaveLength(0)
    expect(transports[1].sent).toHaveLength(0)
  })

  it('switches routes only when explicitly requested and restarts immediately', () => {
    vi.useFakeTimers()
    const transports: FakeTransport[] = []
    const routes: Array<string | undefined> = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      transportFactory: (options) => {
        routes.push(options.route)
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error('ICE failed'))
    expect(routes).toEqual(['direct'])

    connection.setRoute('quick_tunnel')
    expect(routes).toEqual(['direct', 'quick_tunnel'])
    expect(transports[0].closed).toBe(true)
    vi.advanceTimersByTime(10_000)
    expect(routes).toEqual(['direct', 'quick_tunnel'])

    connection.detach()
    vi.useRealTimers()
  })

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
    expect(connection.claimDriver()).toBe(false)
    connection.sendInput('queued')
    expect(transports).toHaveLength(1)
    expect(transports[0].options.accountKey).toBeUndefined()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onFrame(out(4, 'hello'))
    transports[0].options.callbacks.onFrame(out(4, 'duplicate'))
    transports[0].options.callbacks.onFrame(meta(''))
    transports[0].options.callbacks.onReady(4)
    transports[0].options.callbacks.onFrame(meta(transports[0].options.clientInstanceId, 'client-a'))

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

  it('falls back to an available Quick Tunnel without duplicate output or input', async () => {
    const transports: FakeTransport[] = []
    const outputs: string[] = []
    const routes: Array<{ route: string; fallbackReason?: string }> = []
    const resolveQuickTunnelFallback = vi.fn().mockResolvedValue(true)
    const connection = new PeerSessionConnection(sessionID, {
      onOutput: (data) => outputs.push(decodeText(data)),
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      resolveQuickTunnelFallback,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const direct = transports[0]
    direct.options.callbacks.onAuthenticated?.()
    direct.options.callbacks.onFrame(out(8, 'direct-eight'))
    direct.options.callbacks.onFrame(meta(direct.options.clientInstanceId))
    direct.options.callbacks.onReady(8)
    direct.options.callbacks.onFailure(new Error('rendezvous client: ICE failed'))
    connection.sendInput('during-handover')

    await vi.advanceTimersByTimeAsync(0)
    expect(resolveQuickTunnelFallback).toHaveBeenCalledOnce()
    expect(transports).toHaveLength(2)
    const quickTunnel = transports[1]
    expect(quickTunnel.options).toMatchObject({
      route: 'quick_tunnel',
      sinceSeq: 8,
      clientInstanceId: direct.options.clientInstanceId,
    })
    expect(routes.at(-1)).toMatchObject({
      route: 'connecting-quick-tunnel',
      fallbackReason: 'ice_failed',
    })

    direct.options.callbacks.onFrame(out(9, 'late-direct-nine'))
    quickTunnel.options.callbacks.onAuthenticated?.()
    quickTunnel.options.callbacks.onFrame(out(8, 'duplicate-eight'))
    quickTunnel.options.callbacks.onFrame(out(9, 'quick-nine'))
    quickTunnel.options.callbacks.onFrame(meta(''))
    quickTunnel.options.callbacks.onReady(9)
    quickTunnel.options.callbacks.onFrame(meta(quickTunnel.options.clientInstanceId))

    expect(outputs).toEqual(['direct-eight', 'quick-nine'])
    const quickFrames = quickTunnel.sent.map(decodeFrame)
    expect(quickFrames.filter((frame) => frame.type === TYPE.IN)).toHaveLength(1)
    expect(decodeText(quickFrames.find((frame) => frame.type === TYPE.IN)!.payload)).toBe('during-handover')
    expect(direct.sent.map(decodeFrame).some((frame) => frame.type === TYPE.IN)).toBe(false)
    expect(routes.at(-1)).toMatchObject({ route: 'quick-tunnel' })
  })

  it.each([
    'Peer authentication failed',
    'unsupported Peer record',
    'Peer backpressure exceeded',
  ])('does not downgrade %s to Quick Tunnel', async (failure) => {
    const transports: FakeTransport[] = []
    const resolveQuickTunnelFallback = vi.fn().mockResolvedValue(true)
    const connection = new PeerSessionConnection(sessionID, {}, {
      resolveQuickTunnelFallback,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error(failure))
    await Promise.resolve()
    expect(resolveQuickTunnelFallback).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(500)
    expect(transports).toHaveLength(2)
    expect(transports[1].options.route).toBe('direct')
  })

  it('resumes Direct retry when the Quick Tunnel availability check stalls', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      resolveQuickTunnelFallback: () => new Promise<boolean>(() => {}),
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error('ICE failed'))
    await vi.advanceTimersByTimeAsync(1000)
    expect(transports).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(500)
    expect(transports).toHaveLength(2)
    expect(transports[1].options.route).toBe('direct')
  })

  it('ignores a fallback decision that arrives after the pane is suspended', async () => {
    const transports: FakeTransport[] = []
    let resolveFallback!: (available: boolean) => void
    const connection = new PeerSessionConnection(sessionID, {}, {
      resolveQuickTunnelFallback: () => new Promise<boolean>((resolve) => { resolveFallback = resolve }),
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error('ICE failed'))
    connection.suspend()
    resolveFallback(true)
    await vi.advanceTimersByTimeAsync(0)

    expect(transports).toHaveLength(1)
    connection.attach()
    expect(transports).toHaveLength(2)
    expect(transports[1].options.route).toBe('direct')
  })

  it('keeps retrying Quick Tunnel after fallback instead of route flapping', async () => {
    const transports: FakeTransport[] = []
    const resolveQuickTunnelFallback = vi.fn().mockResolvedValue(true)
    const connection = new PeerSessionConnection(sessionID, {}, {
      resolveQuickTunnelFallback,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error('ICE failed'))
    await vi.advanceTimersByTimeAsync(0)
    transports[1].options.callbacks.onAuthenticated?.()
    transports[1].options.callbacks.onReady(0)
    transports[1].options.callbacks.onFailure(new Error('Quick Tunnel disconnected'))

    await vi.advanceTimersByTimeAsync(500)
    expect(transports).toHaveLength(3)
    expect(transports[2].options.route).toBe('quick_tunnel')
    expect(resolveQuickTunnelFallback).toHaveBeenCalledOnce()
  })

  it('fails back to Direct after cooldown and promotes only after replay ready', async () => {
    const transports: FakeTransport[] = []
    const outputs: string[] = []
    const routes: Array<{ route: string }> = []
    const resolveDirectFailback = vi.fn().mockResolvedValue(true)
    const connection = new PeerSessionConnection(sessionID, {
      onOutput: (data) => outputs.push(decodeText(data)),
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      route: 'quick_tunnel',
      resolveDirectFailback,
      directFailbackCooldownMs: 1000,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const quickTunnel = transports[0]
    quickTunnel.options.callbacks.onAuthenticated?.()
    quickTunnel.options.callbacks.onFrame(out(4, 'quick-four'))
    quickTunnel.options.callbacks.onFrame(meta(quickTunnel.options.clientInstanceId))
    quickTunnel.options.callbacks.onReady(4)

    await vi.advanceTimersByTimeAsync(999)
    expect(transports).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(resolveDirectFailback).toHaveBeenCalledOnce()
    expect(transports).toHaveLength(2)

    const direct = transports[1]
    expect(direct.options).toMatchObject({
      route: 'direct',
      sinceSeq: 4,
      clientInstanceId: quickTunnel.options.clientInstanceId,
    })
    expect(quickTunnel.closed).toBe(false)
    expect(routes.at(-1)).toMatchObject({ route: 'connecting-direct' })

    quickTunnel.options.callbacks.onFrame(out(5, 'quick-five'))
    connection.sendInput('during-failback')
    expect(quickTunnel.sent.map(decodeFrame).some((frame) => frame.type === TYPE.IN)).toBe(false)
    direct.options.callbacks.onAuthenticated?.()
    direct.options.callbacks.onFrame(out(5, 'duplicate-five'))
    direct.options.callbacks.onFrame(out(6, 'direct-six'))
    // META identifies the logical client, not the concrete subscriber. The
    // promoted Direct route must still claim its own WithoutAutoDrive sub.
    direct.options.callbacks.onFrame(meta(direct.options.clientInstanceId))
    direct.options.callbacks.onReady(6)
    expect(direct.sent.map(decodeFrame).map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
    ])
    direct.options.callbacks.onFrame(meta(direct.options.clientInstanceId))

    expect(outputs).toEqual(['quick-four', 'quick-five', 'direct-six'])
    expect(quickTunnel.closed).toBe(true)
    expect(routes.at(-1)).toMatchObject({ route: 'direct' })
    const directFrames = direct.sent.map(decodeFrame)
    expect(directFrames.filter((frame) => frame.type === TYPE.IN)).toHaveLength(1)
    expect(decodeText(directFrames.find((frame) => frame.type === TYPE.IN)!.payload)).toBe('during-failback')
  })

  it('reconnects Quick Tunnel after a failed Direct candidate and backs off', async () => {
    const transports: FakeTransport[] = []
    const resolveDirectFailback = vi.fn().mockResolvedValue(true)
    const connection = new PeerSessionConnection(sessionID, {}, {
      route: 'quick_tunnel',
      resolveDirectFailback,
      directFailbackCooldownMs: 1000,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const quickTunnel = transports[0]
    quickTunnel.options.callbacks.onAuthenticated?.()
    quickTunnel.options.callbacks.onFrame(meta(quickTunnel.options.clientInstanceId))
    quickTunnel.options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(1000)

    const direct = transports[1]
    connection.sendInput('held-during-candidate')
    direct.options.callbacks.onFailure(new Error('ICE failed'))
    expect(direct.closed).toBe(true)
    expect(quickTunnel.closed).toBe(true)
    expect(quickTunnel.sent).toHaveLength(0)

    await vi.advanceTimersByTimeAsync(500)
    const replacementQuickTunnel = transports[2]
    expect(replacementQuickTunnel.options.route).toBe('quick_tunnel')
    replacementQuickTunnel.options.callbacks.onAuthenticated?.()
    replacementQuickTunnel.options.callbacks.onFrame(meta(''))
    replacementQuickTunnel.options.callbacks.onReady(0)
    replacementQuickTunnel.options.callbacks.onFrame(meta(replacementQuickTunnel.options.clientInstanceId))
    expect(replacementQuickTunnel.sent.map(decodeFrame).map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
      TYPE.IN,
    ])

    await vi.advanceTimersByTimeAsync(1999)
    expect(transports).toHaveLength(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(transports).toHaveLength(4)
    expect(resolveDirectFailback).toHaveBeenCalledTimes(2)
  })

  it('lets the manual Quick Tunnel action cancel an in-flight Direct failback', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      route: 'quick_tunnel',
      resolveDirectFailback: vi.fn().mockResolvedValue(true),
      directFailbackCooldownMs: 1000,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const quickTunnel = transports[0]
    quickTunnel.options.callbacks.onAuthenticated?.()
    quickTunnel.options.callbacks.onFrame(meta(quickTunnel.options.clientInstanceId))
    quickTunnel.options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(1000)

    const direct = transports[1]
    connection.sendInput('held-before-cancel')
    connection.setRoute('quick_tunnel')

    expect(direct.closed).toBe(true)
    expect(quickTunnel.closed).toBe(true)
    expect(quickTunnel.sent).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(500)
    const replacementQuickTunnel = transports[2]
    replacementQuickTunnel.options.callbacks.onAuthenticated?.()
    replacementQuickTunnel.options.callbacks.onFrame(meta(''))
    replacementQuickTunnel.options.callbacks.onReady(0)
    replacementQuickTunnel.options.callbacks.onFrame(meta(replacementQuickTunnel.options.clientInstanceId))
    expect(replacementQuickTunnel.sent.map(decodeFrame).map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
      TYPE.IN,
    ])
  })

  it('lets a ready Direct candidate win when the replaced Quick Tunnel closes first', async () => {
    const transports: FakeTransport[] = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      route: 'quick_tunnel',
      resolveDirectFailback: vi.fn().mockResolvedValue(true),
      directFailbackCooldownMs: 1000,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const quickTunnel = transports[0]
    quickTunnel.options.callbacks.onAuthenticated?.()
    quickTunnel.options.callbacks.onFrame(meta(quickTunnel.options.clientInstanceId))
    quickTunnel.options.callbacks.onReady(3)
    await vi.advanceTimersByTimeAsync(1000)

    const direct = transports[1]
    quickTunnel.options.callbacks.onFailure(new Error('Peer direct host closed route'))
    connection.sendInput('after-old-close')
    direct.options.callbacks.onAuthenticated?.()
    direct.options.callbacks.onFrame(meta(''))
    direct.options.callbacks.onReady(3)
    direct.options.callbacks.onFrame(meta(direct.options.clientInstanceId))

    expect(transports).toHaveLength(2)
    expect(direct.sent.map(decodeFrame).map((frame) => frame.type)).toEqual([
      TYPE.CLAIM_DRIVER,
      TYPE.IN,
    ])
  })

  it('ignores a late Direct failback capability result after suspend', async () => {
    const transports: FakeTransport[] = []
    let resolveDirect!: (available: boolean) => void
    const connection = new PeerSessionConnection(sessionID, {}, {
      route: 'quick_tunnel',
      resolveDirectFailback: () => new Promise<boolean>((resolve) => { resolveDirect = resolve }),
      directFailbackCooldownMs: 1000,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onFrame(meta(transports[0].options.clientInstanceId))
    transports[0].options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(1000)
    connection.sendInput('probe-keeps-old-route-live')
    expect(transports[0].sent.map(decodeFrame).map((frame) => frame.type)).toEqual([TYPE.IN])
    connection.suspend()
    resolveDirect(true)
    await vi.advanceTimersByTimeAsync(0)

    expect(transports).toHaveLength(1)
    expect(transports[0].closed).toBe(true)
  })

  it('does not let an old capability probe unlock a resumed pane probe', async () => {
    const transports: FakeTransport[] = []
    const resolvers: Array<(available: boolean) => void> = []
    const connection = new PeerSessionConnection(sessionID, {}, {
      route: 'quick_tunnel',
      resolveDirectFailback: () => new Promise<boolean>((resolve) => { resolvers.push(resolve) }),
      directFailbackCooldownMs: 100,
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(100)
    connection.suspend()
    connection.attach()
    transports[1].options.callbacks.onAuthenticated?.()
    transports[1].options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(100)
    expect(resolvers).toHaveLength(2)

    resolvers[0](true)
    await vi.advanceTimersByTimeAsync(0)
    transports[1].options.callbacks.onReady(0)
    await vi.advanceTimersByTimeAsync(100)
    expect(resolvers).toHaveLength(2)

    resolvers[1](true)
    await vi.advanceTimersByTimeAsync(0)
    expect(transports).toHaveLength(3)
    expect(transports[2].options.route).toBe('direct')
  })

  it('preserves cursor and single-writer semantics across 100 forced route handovers', () => {
    const transports: FakeTransport[] = []
    const outputs: string[] = []
    const connection = new PeerSessionConnection(sessionID, {
      onOutput: (data) => outputs.push(decodeText(data)),
    }, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    const clientInstanceId = transports[0].options.clientInstanceId
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onFrame(meta(clientInstanceId))
    transports[0].options.callbacks.onReady(0)

    for (let seq = 1; seq <= 100; seq++) {
      const previous = transports[seq - 1]
      previous.options.callbacks.onFrame(out(seq, `out-${seq}`))

      const route = seq % 2 === 0 ? 'direct' : 'quick_tunnel'
      connection.setRoute(route)
      connection.claimDriver()
      connection.sendResize(80 + seq, 24)
      connection.sendInput(`in-${seq}`)

      const replacement = transports[seq]
      expect(previous.closed).toBe(true)
      expect(replacement.options).toMatchObject({ route, sinceSeq: seq, clientInstanceId })
      previous.options.callbacks.onFrame(out(1000 + seq, `late-${seq}`))
      replacement.options.callbacks.onAuthenticated?.()
      replacement.options.callbacks.onFrame(out(seq, `duplicate-${seq}`))
      replacement.options.callbacks.onReady(seq)
      replacement.options.callbacks.onFrame(meta(clientInstanceId))

      const sent = replacement.sent.map(decodeFrame)
      expect(sent.map((frame) => frame.type)).toEqual([
        TYPE.CLAIM_DRIVER,
        TYPE.RESIZE,
        TYPE.IN,
      ])
      expect(decodeText(sent[2].payload)).toBe(`in-${seq}`)
    }

    expect(outputs).toEqual(Array.from({ length: 100 }, (_, index) => `out-${index + 1}`))
    connection.detach()
  })

  it('keeps an honest ICE failure visible while retrying the Rendezvous route', async () => {
    const transports: FakeTransport[] = []
    const routes: Array<{ route: string; fallbackReason?: string }> = []
    const connection = new PeerSessionConnection(sessionID, {
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    transports[0].options.callbacks.onFailure(new Error('rendezvous client: ICE failed'))

    expect(routes.at(-1)).toMatchObject({
      route: 'connecting-direct',
      fallbackReason: 'ice_failed',
    })
    await vi.advanceTimersByTimeAsync(500)
    expect(transports).toHaveLength(2)
    expect(routes.at(-1)).toMatchObject({
      route: 'connecting-direct',
      fallbackReason: 'ice_failed',
    })

    transports[1].options.callbacks.onAuthenticated?.()
    transports[1].options.callbacks.onReady(0)
    expect(routes.at(-1)).toEqual(expect.objectContaining({ route: 'direct' }))
    expect(routes.at(-1)?.fallbackReason).toBeUndefined()
  })

  it.each([
    ['rendezvous client: service unavailable', 'signal_endpoint_unavailable'],
    ['rendezvous client: peer offline', 'host_unavailable'],
    ['rendezvous client: authentication failed', 'authentication_failed'],
  ])('preserves the Rendezvous failure category for %s', (message, fallbackReason) => {
    const routes: Array<{ route: string; fallbackReason?: string }> = []
    let transport: FakeTransport | undefined
    const connection = new PeerSessionConnection(sessionID, {
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      transportFactory: (options) => (transport = new FakeTransport(options)),
    })

    connection.attach()
    transport!.options.callbacks.onFailure(new Error(message))

    expect(routes.at(-1)).toMatchObject({ route: 'connecting-direct', fallbackReason })
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

  it('soaks a large replay across repeated mobile-style suspend and resume', () => {
    const transports: FakeTransport[] = []
    let outputFrames = 0
    let outputBytes = 0
    const connection = new PeerSessionConnection(sessionID, {
      onOutput: (data) => {
        outputFrames++
        outputBytes += data.length
      },
    }, {
      clientName: 'mobile-soak',
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    let current = transports[0]
    const clientInstanceId = current.options.clientInstanceId
    current.options.callbacks.onAuthenticated?.()
    current.options.callbacks.onFrame(meta(clientInstanceId))

    const replayChunk = 'x'.repeat(2 * 1024)
    const replayFrames = 4096
    let seq = 0
    for (; seq < replayFrames; seq++) {
      current.options.callbacks.onFrame(out(seq + 1, replayChunk))
    }
    current.options.callbacks.onReady(seq)

    expect(outputFrames).toBe(replayFrames)
    expect(outputBytes).toBe(replayFrames * replayChunk.length)

    const resumeCycles = 32
    for (let cycle = 0; cycle < resumeCycles; cycle++) {
      connection.suspend()
      expect(current.closed).toBe(true)

      expect(connection.claimDriver()).toBe(false)
      connection.sendResize(80 + cycle, 24 + cycle)
      connection.sendInput(`mobile-input-${cycle}`)
      current.options.callbacks.onFrame(out(seq + 1, `late-${cycle}`))

      connection.attach()
      const replacement = transports.at(-1)!
      expect(replacement.options).toMatchObject({ sinceSeq: seq, clientInstanceId })
      expect(replacement.sent).toHaveLength(0)

      replacement.options.callbacks.onAuthenticated?.()
      replacement.options.callbacks.onFrame(out(seq, `duplicate-${cycle}`))
      seq++
      const resumedOutput = `resumed-${cycle}`
      replacement.options.callbacks.onFrame(out(seq, resumedOutput))
      replacement.options.callbacks.onReady(seq)
      replacement.options.callbacks.onFrame(meta(clientInstanceId))

      const sent = replacement.sent.map(decodeFrame)
      expect(sent.map((frame) => frame.type)).toEqual([
        TYPE.CLAIM_DRIVER,
        TYPE.RESIZE,
        TYPE.IN,
      ])
      expect(decodeText(sent[2].payload)).toBe(`mobile-input-${cycle}`)
      current = replacement
    }

    expect(transports).toHaveLength(resumeCycles + 1)
    expect(outputFrames).toBe(replayFrames + resumeCycles)
    expect(outputBytes).toBe(replayFrames * replayChunk.length +
      Array.from({ length: resumeCycles }, (_, cycle) => `resumed-${cycle}`.length)
        .reduce((total, length) => total + length, 0))
    connection.detach()
  }, 10_000)

  it('labels an explicitly selected Quick Tunnel route without automatic fallback', () => {
    const transports: FakeTransport[] = []
    const routes: Array<{ route: string }> = []
    const connection = new PeerSessionConnection(sessionID, {
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      route: 'quick_tunnel',
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    expect(transports[0].options.route).toBe('quick_tunnel')
    expect(routes.at(-1)).toMatchObject({ route: 'connecting-quick-tunnel' })
    transports[0].options.callbacks.onDiagnostics?.({ route: 'quick_tunnel' })
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)
    expect(routes.at(-1)).toMatchObject({ route: 'quick-tunnel' })
  })

  it('labels an authenticated Manual LAN route', () => {
    const transports: FakeTransport[] = []
    const routes: Array<{ route: string }> = []
    const connection = new PeerSessionConnection(sessionID, {
      onRouteChange: (diagnostics) => routes.push(diagnostics),
    }, {
      route: 'lan',
      transportFactory: (options) => {
        const transport = new FakeTransport(options)
        transports.push(transport)
        return transport
      },
    })

    connection.attach()
    expect(transports[0].options.route).toBe('lan')
    expect(routes.at(-1)).toMatchObject({ route: 'connecting-lan' })
    transports[0].options.callbacks.onDiagnostics?.({ route: 'lan' })
    transports[0].options.callbacks.onAuthenticated?.()
    transports[0].options.callbacks.onReady(0)
    expect(routes.at(-1)).toMatchObject({ route: 'lan' })
  })

  it('does not initiate retired profile sessions', () => {
    const connection = new PeerSessionConnection(sessionID, {}, { transportFactory: (options) => new FakeTransport(options) })
    expect(connection).not.toHaveProperty('createSessionWithProfile')
    expect(TYPE).not.toHaveProperty('SESSION_CREATE')
    expect(TYPE).not.toHaveProperty('SESSION_CREATED')
  })
})
