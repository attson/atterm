import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPeerClientBridge } from './clientBridge'
import { PeerSessionConnection } from '../peerSessionConnection'
import { PeerClientHandshake } from './handshake'
import { DirectRecordKind, sealDirectRecord } from '../directCrypto'
import type { DirectHandshakeKeys } from '../directHandshake'
import type { PeerClientState } from './clientState'
import { encodeFrame, encodeText, TYPE, uuidParse } from '../proto'

// Trust and handshake derivation have their own interoperability coverage. Keep
// the browser bridge, encrypted record codec, route lifecycle and buffers real.
vi.mock('./documents', () => ({
  verifyConnectionBundle: async () => ({ genesis: { hash: 'space-hash' }, document: { issuer_membership: 'host-grant' }, route: { url: 'https://peer.example' } }),
  verifyGenesis: async () => ({ hash: 'space-hash', document: { space_id: 'space' } }),
  activeMemberships: async () => [
    { token: 'client-grant', document: { subject_peer_id: 'client', allowed_session_ids: [], permission: 'full' } },
    { token: 'host-grant', document: { subject_peer_id: 'host', allowed_session_ids: [], permission: 'full' } },
  ],
}))
const sid = '11111111-2222-3333-4444-555555555555'
function keys(): DirectHandshakeKeys {
  return { clientToHostKey: new Uint8Array(32).fill(1), hostToClientKey: new Uint8Array(32).fill(2), clientToHostNoncePrefix: new Uint8Array(16).fill(3), hostToClientNoncePrefix: new Uint8Array(16).fill(4), transcriptHash: new Uint8Array(32).fill(5) }
}
function record(kind: DirectRecordKind, seq: bigint, plaintext: Uint8Array): ArrayBuffer {
  const key = keys()
  return sealDirectRecord(key.hostToClientKey, key.hostToClientNoncePrefix, key.transcriptHash, kind, seq, plaintext).slice().buffer as ArrayBuffer
}
class Channel {
  readyState = 'connecting'
  bufferedAmount = 0
  bufferWrites = false
  binaryType = ''
  sent: ArrayBuffer[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  send(bytes: ArrayBuffer) { this.sent.push(bytes); if (this.bufferWrites) this.bufferedAmount += bytes.byteLength }
  open() { this.readyState = 'open'; this.onopen?.() }
  close() { this.readyState = 'closed'; this.onclose?.() }
  emit(data: unknown) { this.onmessage?.({ data } as MessageEvent) }
}
class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 0
  bufferedAmount = 0
  bufferWrites = false
  binaryType = ''
  sent: ArrayBuffer[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor() { Socket.instances.push(this) }
  send(bytes: ArrayBuffer) { this.sent.push(bytes); if (this.bufferWrites) this.bufferedAmount += bytes.byteLength }
  open() { this.readyState = 1; this.onopen?.() }
  close() { this.readyState = 3; this.onclose?.() }
  emit(data: unknown) { this.onmessage?.({ data } as MessageEvent) }
}
class PeerConnection {
  static instances: PeerConnection[] = []
  readonly channel = new Channel()
  localDescription: RTCSessionDescriptionInit | null = null
  onicecandidate: unknown = null
  oniceconnectionstatechange: unknown = null
  onconnectionstatechange: unknown = null
  constructor() { PeerConnection.instances.push(this) }
  createDataChannel() { return this.channel }
  async createOffer() { return { type: 'offer' as const, sdp: 'unit-sdp' } }
  async setLocalDescription(value: RTCSessionDescriptionInit) { this.localDescription = value }
  close() { this.channel.close() }
}
async function flush() { for (let index = 0; index < 60; index++) await Promise.resolve() }
const connections: PeerSessionConnection[] = []
beforeEach(() => {
  vi.useFakeTimers()
  Socket.instances = []; PeerConnection.instances = []
  vi.stubGlobal('WebSocket', Socket); vi.stubGlobal('RTCPeerConnection', PeerConnection)
  vi.spyOn(PeerClientHandshake, 'create').mockImplementation(async () => ({
    clientHello: () => new Uint8Array([1]), handleHostHello: async () => new Uint8Array([3]), handleAuthOK: keys, dispose: () => {},
  }) as unknown as PeerClientHandshake)
})
afterEach(() => { connections.splice(0).forEach((conn) => conn.detach()); vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals() })
async function setup(route: 'quick_tunnel' | 'direct') {
  const state: PeerClientState = { v: 1, genesis: 'genesis', membership: 'client-grant', memberships: ['client-grant', 'host-grant'], revocations: [], epoch_rotations: [], epoch_envelopes: [], route_bundle: 'bundle', joined_at: 1, fallback_consent: true }
  const bridge = createPeerClientBridge({ loadIdentity: async () => ({ peerId: 'client', publicKey: new Uint8Array(), wrappingPublicKey: new Uint8Array(), sign: async () => new Uint8Array(), deriveWrappingSecret: async () => new Uint8Array() }), store: { load: async () => state, save: async () => {}, clear: async () => {} } })
  let clientID = ''
  const conn = new PeerSessionConnection(sid, {}, { route, transportFactory: (options) => {
    clientID = options.clientInstanceId
    return bridge.createSessionTransport!(options)
  } })
  connections.push(conn); conn.attach(); await flush()
  const ws = Socket.instances[0]
  ws.open()
  ws.emit(JSON.stringify({ v: 1, kind: 'authorized', user_id: 'space', host_id: 'host', permission: 3, expires_at_unix_millis: Date.now() + 60_000 }))
  await flush(); ws.emit(new Uint8Array([2]).buffer); await flush(); ws.emit(new Uint8Array([4]).buffer); await flush()
  let target: Socket | Channel = ws
  let seq = 0n
  if (route === 'quick_tunnel') {
    ws.emit(record(DirectRecordKind.Signal, seq++, encodeText(JSON.stringify({ v: 1, type: 'wss_ready', payload: '' }))))
  } else {
    target = PeerConnection.instances[0].channel
    target.open(); await flush(); target.emit(new Uint8Array([2]).buffer); await flush(); target.emit(new Uint8Array([4]).buffer)
  }
  await flush()
  target.emit(record(DirectRecordKind.Frame, seq++, encodeFrame(TYPE.META, uuidParse(sid), encodeText(JSON.stringify({ driver_client_id: clientID })))))
  target.emit(record(DirectRecordKind.DirectReady, seq, new Uint8Array(8)))
  await flush()
  target.bufferWrites = true
  return { conn, target, ws, closed: () => target.readyState === 'closed' || target.readyState === 3 }
}
function file(): Blob { return { size: 3, type: '', arrayBuffer: async () => new Uint8Array([1, 2, 3]).buffer } as Blob }
for (const route of ['quick_tunnel', 'direct'] as const) describe(`browser Peer ${route} upload drain`, () => {
  it('holds automatic suspension through the actual lazy wrapper until buffered file records drain', async () => {
    const { conn, target, closed } = await setup(route)
    const pending = conn.sendPasteFile(file(), 'a.bin')
    conn.suspend(); await flush()
    expect(target.bufferedAmount).toBeGreaterThan(0)
    expect(closed()).toBe(false)
    target.bufferedAmount = 0
    await vi.advanceTimersByTimeAsync(25)
    await expect(pending).resolves.toBe(true)
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })

  it('allows a slow upload lasting longer than thirty seconds while bytes keep draining', async () => {
    const { conn, target, closed } = await setup(route)
    const pending = conn.sendPasteFile(file(), 'slow.bin')
    conn.suspend(); await flush()
    target.bufferedAmount = 1000
    for (const remaining of [900, 800, 700, 600]) {
      await vi.advanceTimersByTimeAsync(20_000)
      expect(closed()).toBe(false)
      target.bufferedAmount = remaining
      await vi.advanceTimersByTimeAsync(25)
    }
    target.bufferedAmount = 0
    await vi.advanceTimersByTimeAsync(25)
    await expect(pending).resolves.toBe(true)
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('cancels all drain timers on forced detach', async () => {
    const { conn, target, closed } = await setup(route)
    const pending = conn.sendPasteFile(file(), 'a.bin')
    const rejected = expect(pending).rejects.toThrow()
    conn.suspend(); await flush(); expect(target.bufferedAmount).toBeGreaterThan(0)
    conn.detach(); await rejected
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('rejects a stalled browser queue after a bounded wait', async () => {
    const { conn, closed } = await setup(route)
    const pending = conn.sendPasteFile(file(), 'a.bin')
    const rejected = expect(pending).rejects.toThrow('timed out')
    conn.suspend(); await flush(); await vi.advanceTimersByTimeAsync(30_000); await rejected
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('rejects a drain on route failure immediately and clears its timers', async () => {
    const { conn, target, closed } = await setup(route)
    const pending = conn.sendPasteFile(file(), 'a.bin')
    const rejected = expect(pending).rejects.toThrow()
    conn.suspend(); await flush(); target.close(); await rejected
    expect(closed()).toBe(true); expect(vi.getTimerCount()).toBe(0)
  })
  it('reactivation keeps the route open after buffered image bytes drain', async () => {
    const { conn, target, closed } = await setup(route)
    const pending = conn.sendPasteImage(file())
    conn.suspend(); await flush(); conn.attach(); target.bufferedAmount = 0
    await vi.advanceTimersByTimeAsync(25); await pending
    expect(closed()).toBe(false)
  })
})
