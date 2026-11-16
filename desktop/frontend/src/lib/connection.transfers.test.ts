import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SessionConnection, type FSRequest } from './connection'
import { PeerSessionConnection } from './peerSessionConnection'
import { NativeDirectClientTransport, type NativeDirectBridge } from './nativeDirectClient'
import { TYPE, decodeFrame, decodeText, encodeFrame, encodeText, uuidParse } from './proto'
import { decodeSegments, encodeSegments } from './fsSegments'
import { flushLogs } from './log'
import { createRemoteSessionFS } from '../plugins/fileExplorer/remoteSessionFS'
const sid = '11111111-2222-3333-4444-555555555555'
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
class Socket {
  static OPEN = 1
  static CONNECTING = 0
  static instances: Socket[] = []
  readyState = 0
  bufferedAmount = 0
  binaryType = ''
  sent: Uint8Array[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  constructor() { Socket.instances.push(this) }
  send(frame: Uint8Array) { if (this.readyState !== 1) throw new Error('closed'); this.sent.push(frame) }
  close() { this.readyState = 3; this.onclose?.() }
  emit(type: typeof TYPE.FS_RESPONSE | typeof TYPE.META, value: unknown) {
    const bytes = encodeText(JSON.stringify(value))
    this.onmessage?.({ data: encodeFrame(type, uuidParse(sid), type === TYPE.FS_RESPONSE ? encodeSegments([bytes]) : bytes).buffer } as MessageEvent)
  }
}
function setup(route: 'relay' | 'peer') {
  if (route === 'relay') {
    const conn = new SessionConnection({ url: 'ws://localhost:8080', session_token: '' }, sid)
    conn.attach()
    const ws = Socket.instances.at(-1)!
    ws.readyState = 1
    ws.onopen?.()
    const attach = JSON.parse(decodeText(decodeFrame(ws.sent[0]).payload))
    ws.emit(TYPE.META, { driver_client_id: attach.client_id })
    return { conn, sent: ws.sent, closed: () => ws.readyState === 3,
      respond: (request: FSRequest, extra: object = {}) => ws.emit(TYPE.FS_RESPONSE, { request_id: request.request_id, ok: true, ...extra }),
      blockNativeDrain: () => () => {},
      fail: () => ws.close(),
      blockDrain: () => { ws.bufferedAmount = 10; return () => { ws.bufferedAmount = 0 } } }
  }
  let handler: ((event: unknown) => void) | null = null
  let stopped = false
  let blocked: ReturnType<typeof deferred<void>> | null = null
  let nativeBuffered: ReturnType<typeof deferred<void>> | null = null
  const sent: Uint8Array[] = []
  const bridge: NativeDirectBridge = {
    on: (_name, next) => { handler = next; return () => {} }, start: async () => {},
    send: async (_id, bytes) => { sent.push(new Uint8Array(bytes)); await blocked?.promise },
    drain: async () => { await nativeBuffered?.promise },
    stop: async () => { stopped = true },
  }
  let clientID = ''
  const conn = new PeerSessionConnection(sid, {}, { transportFactory: (options) => {
    clientID = options.clientInstanceId
    return new NativeDirectClientTransport(options, bridge)
  } })
  conn.attach()
  const emit = (type: typeof TYPE.FS_RESPONSE | typeof TYPE.META, value: unknown) => {
    const bytes = encodeText(JSON.stringify(value))
    const frame = encodeFrame(type, uuidParse(sid), type === TYPE.FS_RESPONSE ? encodeSegments([bytes]) : bytes)
    handler!({ kind: 'frame', frame_base64: btoa(String.fromCharCode(...frame)) })
  }
  handler!({ kind: 'authenticated' }); emit(TYPE.META, { driver_client_id: clientID }); handler!({ kind: 'ready', last_replayed_seq: 0 })
  return { conn, sent, closed: () => stopped,
    respond: (request: FSRequest, extra: object = {}) => emit(TYPE.FS_RESPONSE, { request_id: request.request_id, ok: true, ...extra }),
    blockNativeDrain: () => { nativeBuffered = deferred<void>(); return () => nativeBuffered!.resolve() },
    fail: () => handler!({ kind: 'failure', error: 'network failed' }),
    blockDrain: () => { blocked = deferred<void>(); return () => blocked!.resolve() } }
}
function lastRequest(sent: Uint8Array[]): FSRequest {
  const frame = [...sent].reverse().map(decodeFrame).find((frame) => frame.type === TYPE.FS_REQUEST)!
  return JSON.parse(decodeText(decodeSegments(frame.payload)![0]))
}
async function flush() { for (let i = 0; i < 12; i++) await Promise.resolve() }
const connections: Array<{ detach(): void }> = []
beforeEach(() => { vi.useFakeTimers(); Socket.instances = []; vi.stubGlobal('WebSocket', Socket) })
afterEach(async () => { connections.splice(0).forEach((conn) => conn.detach()); await flushLogs(); vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals() })
for (const route of ['relay', 'peer'] as const) describe(`${route} transfers during automatic suspension`, () => {
  function open() { const result = setup(route); connections.push(result.conn); return result }
  for (const kind of ['file', 'image'] as const) it(`keeps deferred ${kind} encoding and queued bytes alive until drained`, async () => {
    const { conn, sent, closed, blockDrain } = open()
    const bytes = deferred<ArrayBuffer>()
    const blob = { size: 3, type: 'application/octet-stream', arrayBuffer: () => bytes.promise } as Blob
    const unblock = blockDrain()
    const pending = kind === 'file' ? conn.sendPasteFile(blob, 'a.bin') : conn.sendPasteImage(blob)
    conn.suspend()
    expect(closed()).toBe(false)
    bytes.resolve(new Uint8Array([1, 2, 3]).buffer)
    await flush()
    expect(sent.map(decodeFrame).some((frame) => frame.type === (kind === 'file' ? TYPE.PASTE_FILE : TYPE.PASTE_IMAGE))).toBe(true)
    expect(closed()).toBe(false)
    unblock()
    await vi.advanceTimersByTimeAsync(25)
    await expect(pending).resolves.toBe(true)
    expect(closed()).toBe(true)
    await flushLogs()
    expect(vi.getTimerCount()).toBe(0)
  })

  if (route === 'relay') it('keeps a slow relay upload alive while its browser buffer makes progress', async () => {
    const { conn, closed, blockDrain } = open()
    blockDrain()
    const ws = Socket.instances.at(-1)!
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'a')
    conn.suspend(); await flush()
    for (const remaining of [9, 8, 7]) {
      await vi.advanceTimersByTimeAsync(20_000)
      expect(closed()).toBe(false)
      ws.bufferedAmount = remaining
      await vi.advanceTimersByTimeAsync(25)
    }
    ws.bufferedAmount = 0
    await vi.advanceTimersByTimeAsync(25)
    await expect(pending).resolves.toBe(true)
    expect(closed()).toBe(true)
  })
  it('waits for the last concurrent filesystem response', async () => {
    const { conn, sent, respond, closed } = open()
    const first = conn.sendFSRequest({ op: 'file_meta', path: '/a' }); await flush(); const reqA = lastRequest(sent)
    const second = conn.sendFSRequest({ op: 'file_meta', path: '/b' }); await flush(); const reqB = lastRequest(sent)
    conn.suspend(); expect(closed()).toBe(false)
    respond(reqA); await first; expect(closed()).toBe(false)
    respond(reqB); await second; expect(closed()).toBe(true)
  })
  it('keeps chunked download alive across individual response completion', async () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:download')
    const { conn, sent, respond, closed } = open()
    const fs = createRemoteSessionFS(conn)
    const pending = fs.assetUrlFor('/a'); await flush(); conn.suspend(); expect(closed()).toBe(false)
    respond(lastRequest(sent), { chunk: { path: '/a', offset: 0, length: 1, data: 'YQ==', eof: false } }); await flush()
    expect(closed()).toBe(false); expect(lastRequest(sent).offset).toBe(1)
    respond(lastRequest(sent), { chunk: { path: '/a', offset: 1, length: 1, data: 'Yg==', eof: true } })
    await expect(pending).resolves.toBe('blob:download'); expect(closed()).toBe(true); fs.dispose()
  })
  it('releases a rejected blob and a timed out filesystem request', async () => {
    const { conn, closed } = open()
    const bytes = deferred<ArrayBuffer>()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: () => bytes.promise } as Blob, 'a')
    const rejected = expect(pending).rejects.toThrow('read failed')
    const fs = conn.sendFSRequest({ op: 'read_file', path: '/a' }, 20)
    const timedOut = expect(fs).rejects.toThrow('timed out')
    conn.suspend(); bytes.reject(new Error('read failed')); await rejected; expect(closed()).toBe(false)
    await vi.advanceTimersByTimeAsync(20); await timedOut; expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('reactivation cancels the late suspension', async () => {
    const { conn, sent, respond, closed } = open()
    const pending = conn.sendFSRequest({ op: 'read_file', path: '/a' }); await flush()
    conn.suspend(); conn.attach(); respond(lastRequest(sent)); await pending; expect(closed()).toBe(false)
  })
  it('detach forces cleanup and cancels a hung encoding timeout', async () => {
    const { conn, closed } = open()
    const bytes = deferred<ArrayBuffer>()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: () => bytes.promise } as Blob, 'a')
    const rejected = expect(pending).rejects.toThrow()
    conn.suspend(); conn.detach(); expect(closed()).toBe(true); await rejected; expect(vi.getTimerCount()).toBe(0)
  })
  if (route === 'relay') it('bounds a stalled upload drain instead of keeping a hidden subscription forever', async () => {
    const { conn, closed, blockDrain } = open(); blockDrain()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'a')
    const rejected = expect(pending).rejects.toThrow('timed out')
    conn.suspend(); await flush(); await vi.advanceTimersByTimeAsync(30_000); await rejected; expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })



  if (route === 'peer') {
    it('keeps native transfers busy until the Go buffer drains after sendChain completion', async () => {
      const { conn, sent, closed, blockNativeDrain } = open()
      const unblock = blockNativeDrain()
      const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'a')
      conn.suspend(); await flush()
      expect(sent.map(decodeFrame).some((frame) => frame.type === TYPE.PASTE_FILE)).toBe(true)
      await vi.advanceTimersByTimeAsync(60_000)
      expect(closed()).toBe(false)
      unblock(); await expect(pending).resolves.toBe(true)
      expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
    })
    it('reactivation cancels the pause while the native Go buffer is draining', async () => {
      const { conn, closed, blockNativeDrain } = open()
      const unblock = blockNativeDrain()
      const pending = conn.sendPasteImage({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob)
      conn.suspend(); await flush(); conn.attach(); unblock(); await pending
      expect(closed()).toBe(false)
    })
    it('forced detach aborts the wait for the native Go buffer', async () => {
      const { conn, closed, blockNativeDrain } = open()
      const unblock = blockNativeDrain()
      const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'a')
      const rejected = expect(pending).rejects.toThrow('cancelled')
      conn.suspend(); await flush(); conn.detach(); await rejected; unblock()
      expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
    })
  }
  if (route === 'peer') it('lets a native send queue make slow progress without a total transfer deadline', async () => {
    const { conn, closed, blockDrain } = open()
    const unblock = blockDrain()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'slow.bin')
    conn.suspend(); await flush(); await vi.advanceTimersByTimeAsync(60_000)
    expect(closed()).toBe(false)
    unblock(); await expect(pending).resolves.toBe(true)
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('releases a failed remote asset response and its outer download lease', async () => {
    const { conn, sent, respond, closed } = open()
    const fs = createRemoteSessionFS(conn)
    const pending = fs.assetUrlFor('/a')
    const rejected = expect(pending).rejects.toThrow('denied')
    await flush(); conn.suspend()
    respond(lastRequest(sent), { ok: false, error: 'denied' })
    await rejected; expect(closed()).toBe(true); fs.dispose()
  })
  it('bounds a blob encoder that never settles', async () => {
    const { conn, closed } = open()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: () => new Promise<ArrayBuffer>(() => {}) } as Blob, 'a')
    const rejected = expect(pending).rejects.toThrow('timed out')
    conn.suspend(); await vi.advanceTimersByTimeAsync(30_000); await rejected
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('cancels drain timers immediately on a forced detach', async () => {
    const { conn, closed, blockDrain } = open(); blockDrain()
    const pending = conn.sendPasteFile({ size: 1, type: '', arrayBuffer: async () => new Uint8Array([1]).buffer } as Blob, 'a')
    const rejected = expect(pending).rejects.toThrow()
    conn.suspend(); await flush(); conn.detach(); await rejected
    await flushLogs(); expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('honors a pending pause on network failure without waiting for file timeout', async () => {
    const { conn, closed, fail } = open()
    const pending = conn.sendFSRequest({ op: 'read_file', path: '/a' })
    const rejected = expect(pending).rejects.toThrow()
    conn.suspend(); fail(); await rejected; await flushLogs()
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('holds a history paste transaction until all text and images have been sent', async () => {
    const { conn, closed } = open()
    const release = conn.beginHistoryPaste()!; expect(release).toBeTypeOf('function')
    conn.suspend(); expect(closed()).toBe(false); conn.sendInput('last text'); release(); await flush(); expect(closed()).toBe(true)
  })
})
