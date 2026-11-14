import { describe, expect, it, vi } from 'vitest'
import type { DirectClientOptions } from './directClient'
import { NativeDirectClientTransport, type NativeDirectBridge } from './nativeDirectClient'

function setup(route?: 'direct' | 'quick_tunnel') {
  let eventHandler: ((data: unknown) => void) | null = null
  const off = vi.fn()
  const bridge: NativeDirectBridge = {
    on: vi.fn((_event, handler) => {
      eventHandler = handler
      return off
    }),
    start: vi.fn().mockResolvedValue(undefined),
    send: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn().mockResolvedValue(undefined),
  }
  const callbacks = {
    onAuthenticated: vi.fn(),
    onFrame: vi.fn(),
    onReady: vi.fn(),
    onDiagnostics: vi.fn(),
    onFailure: vi.fn(),
  }
  const options: DirectClientOptions = {
    signalURL: 'wss://relay.example/direct-signal',
    sessionId: '11111111-2222-3333-4444-555555555555',
    sinceSeq: 9,
    clientInstanceId: 'client-1',
    accountKey: new Uint8Array(32).fill(7),
    callbacks,
  }
  const transport = new NativeDirectClientTransport({ ...options, route }, bridge)
  return { bridge, callbacks, transport, off, emit: (data: unknown) => eventHandler?.(data) }
}

describe('NativeDirectClientTransport', () => {
  it('maps native lifecycle events onto the existing direct callbacks', async () => {
    const { bridge, callbacks, transport, emit } = setup()
    transport.start()
    expect(bridge.start).toHaveBeenCalledWith(expect.objectContaining({
      session_id: '11111111-2222-3333-4444-555555555555',
      since_seq: 9,
      client_instance_id: 'client-1',
    }))

    emit({ kind: 'diagnostics', ice_state: 'checking' })
    emit({ kind: 'authenticated' })
    emit({ kind: 'frame', frame_base64: btoa('output') })
    emit({ kind: 'ready', last_replayed_seq: 12 })

    expect(callbacks.onDiagnostics).toHaveBeenCalledWith({ iceState: 'checking' })
    expect(callbacks.onAuthenticated).toHaveBeenCalledOnce()
    expect(Array.from(callbacks.onFrame.mock.calls[0][0])).toEqual(Array.from(new TextEncoder().encode('output')))
    expect(callbacks.onReady).toHaveBeenCalledWith(12)
    expect(transport.sendFrame(new Uint8Array([1, 2, 3]))).toBe(true)
    await vi.waitFor(() => expect(bridge.send).toHaveBeenCalledWith(expect.any(String), [1, 2, 3]))
  })

  it('closes the native attempt and reports asynchronous failures once', async () => {
    const { bridge, callbacks, transport, off, emit } = setup()
    transport.start()
    emit({ kind: 'failure', error: 'ICE failed' })
    emit({ kind: 'failure', error: 'duplicate' })

    expect(callbacks.onFailure).toHaveBeenCalledOnce()
    expect(callbacks.onFailure.mock.calls[0][0]).toEqual(expect.objectContaining({ message: 'ICE failed' }))
    expect(off).toHaveBeenCalledOnce()
    await vi.waitFor(() => expect(bridge.stop).toHaveBeenCalledOnce())
    expect(transport.sendFrame(new Uint8Array([1]))).toBe(false)
  })

  it('selects Quick Tunnel by kind without exposing an endpoint or credential', () => {
    const { bridge, callbacks, transport, emit } = setup('quick_tunnel')
    transport.start()

    const request = vi.mocked(bridge.start).mock.calls[0][0]
    expect(request).toMatchObject({ route: 'quick_tunnel' })
    expect(request).not.toHaveProperty('url')
    expect(request).not.toHaveProperty('token')
    expect(request).not.toHaveProperty('membership')
    expect(request).not.toHaveProperty('account_key')

    emit({ kind: 'diagnostics', route: 'quick_tunnel' })
    expect(callbacks.onDiagnostics).toHaveBeenCalledWith({ route: 'quick_tunnel' })
  })
  it('drains the native send queue before allowing its owner to stop the route', async () => {
    const { bridge, transport, emit } = setup()
    let finishSend!: () => void
    vi.mocked(bridge.send).mockImplementationOnce(() => new Promise<void>((resolve) => { finishSend = resolve }))
    transport.start()
    emit({ kind: 'authenticated' })
    transport.sendFrame(new Uint8Array([1]))
    transport.sendFrame(new Uint8Array([2]))
    let drained = false
    const pending = transport.drain().then(() => { drained = true })
    await Promise.resolve()
    expect(bridge.send).toHaveBeenCalledTimes(1)
    expect(drained).toBe(false)
    expect(bridge.stop).not.toHaveBeenCalled()
    finishSend()
    await pending
    expect(bridge.send).toHaveBeenNthCalledWith(2, expect.any(String), [2])
    transport.close()
    expect(bridge.stop).toHaveBeenCalledOnce()
  })

  it('rejects drain on native send failure and drops remaining queued writes', async () => {
    const { bridge, transport, callbacks, emit } = setup()
    vi.mocked(bridge.send).mockRejectedValueOnce(new Error('native write failed'))
    transport.start()
    emit({ kind: 'authenticated' })
    transport.sendFrame(new Uint8Array([1]))
    transport.sendFrame(new Uint8Array([2]))
    await expect(transport.drain()).rejects.toThrow()
    expect(callbacks.onFailure).toHaveBeenCalledOnce()
    expect(bridge.stop).toHaveBeenCalledOnce()
    expect(bridge.send).toHaveBeenCalledTimes(1)
  })

  it('waits for the actual native buffer drain after all bridge writes settle', async () => {
    const { bridge, transport, emit } = setup()
    let finishSend!: () => void
    let finishDrain!: () => void
    vi.mocked(bridge.send).mockImplementationOnce(() => new Promise<void>((resolve) => { finishSend = resolve }))
    bridge.drain = vi.fn(() => new Promise<void>((resolve) => { finishDrain = resolve }))
    transport.start(); emit({ kind: 'authenticated' })
    transport.sendFrame(new Uint8Array([1]))
    let drained = false
    const pending = transport.drain().then(() => { drained = true })
    await Promise.resolve()
    expect(bridge.drain).not.toHaveBeenCalled()
    finishSend()
    for (let index = 0; index < 12; index++) await Promise.resolve()
    expect(bridge.drain).toHaveBeenCalledWith(transport.nativeAttemptId())
    expect(drained).toBe(false)
    finishDrain(); await pending
    expect(drained).toBe(true)
    transport.close()
  })

  it('aborts a pending native buffer drain immediately when forcibly closed', async () => {
    const { bridge, transport, emit } = setup()
    bridge.drain = vi.fn(() => new Promise<void>(() => {}))
    transport.start(); emit({ kind: 'authenticated' })
    const pending = transport.drain()
    const rejected = expect(pending).rejects.toThrow('cancelled')
    for (let index = 0; index < 12; index++) await Promise.resolve()
    expect(bridge.drain).toHaveBeenCalledOnce()
    transport.close(); await rejected
    expect(bridge.stop).toHaveBeenCalledOnce()
  })

})
