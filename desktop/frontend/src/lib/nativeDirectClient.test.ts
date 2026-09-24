import { describe, expect, it, vi } from 'vitest'
import type { DirectClientOptions } from './directClient'
import { NativeDirectClientTransport, type NativeDirectBridge } from './nativeDirectClient'

function setup() {
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
  const transport = new NativeDirectClientTransport(options, bridge)
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
})
