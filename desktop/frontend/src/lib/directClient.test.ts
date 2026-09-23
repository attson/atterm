import { describe, expect, it, vi } from 'vitest'
import { sha256 } from '@noble/hashes/sha2.js'
import {
  buildDirectProof,
  deriveDirectTrafficKeys,
  deriveP256SharedSecret,
  DirectPermission,
  DirectRecordKind,
  DirectRole,
  marshalDirectTranscript,
  openDirectRecord,
  sealDirectRecord,
} from './directCrypto'
import { DirectClientTransport, type DirectSignalMessage } from './directClient'

function dataBuffer(bytes: Uint8Array): ArrayBuffer {
  const buffer = new ArrayBuffer(bytes.length)
  new Uint8Array(buffer).set(bytes)
  return buffer
}

function join(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let offset = 0
  for (const part of parts) {
    out.set(part, offset)
    offset += part.length
  }
  return out
}

function ticketText(ticket: Uint8Array): string {
  let binary = ''
  for (const byte of ticket) binary += String.fromCharCode(byte)
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '')
}

async function keyPair(): Promise<{ publicKey: Uint8Array; privateJWK: JsonWebKey }> {
  const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']) as CryptoKeyPair
  return {
    publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
    privateJWK: await crypto.subtle.exportKey('jwk', pair.privateKey),
  }
}

async function waitFor(predicate: () => boolean): Promise<void> {
  for (let i = 0; i < 100; i++) {
    if (predicate()) return
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
  throw new Error('condition not reached')
}

class FakeWebSocket {
  readyState: number = WebSocket.CONNECTING
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  sent: string[] = []

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    if (typeof data !== 'string') throw new Error('expected text signaling message')
    this.sent.push(data)
  }

  open(): void {
    this.readyState = WebSocket.OPEN
    this.onopen?.(new Event('open'))
  }

  message(message: DirectSignalMessage): void {
    this.onmessage?.({ data: JSON.stringify(message) } as MessageEvent)
  }

  close(): void {
    if (this.readyState === WebSocket.CLOSED) return
    this.readyState = WebSocket.CLOSED
    this.onclose?.({} as CloseEvent)
  }
}

class FakeDataChannel {
  readonly ordered = true
  readonly maxPacketLifeTime = null
  readonly maxRetransmits = null
  readyState: RTCDataChannelState = 'connecting'
  bufferedAmount = 0
  binaryType: BinaryType = 'blob'
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onclose: ((event: Event) => void) | null = null
  sent: ArrayBuffer[] = []

  send(data: string | Blob | ArrayBuffer | ArrayBufferView): void {
    if (!(data instanceof ArrayBuffer)) throw new Error('expected ArrayBuffer data channel message')
    this.sent.push(data)
  }

  open(): void {
    this.readyState = 'open'
    this.onopen?.(new Event('open'))
  }

  message(data: Uint8Array): void {
    this.onmessage?.({ data: dataBuffer(data) } as MessageEvent)
  }

  close(): void {
    if (this.readyState === 'closed') return
    this.readyState = 'closed'
    this.onclose?.(new Event('close'))
  }
}

class FakePeerConnection {
  readonly dc = new FakeDataChannel()
  iceGatheringState: RTCIceGatheringState = 'complete'
  connectionState: RTCPeerConnectionState = 'new'
  localDescription: RTCSessionDescription | null = null
  remoteDescription: RTCSessionDescription | null = null
  onconnectionstatechange: ((event: Event) => void) | null = null

  createDataChannel(): RTCDataChannel {
    return this.dc as unknown as RTCDataChannel
  }

  async createOffer(): Promise<RTCSessionDescriptionInit> {
    return { type: 'offer', sdp: 'fake-offer' }
  }

  async setLocalDescription(description: RTCLocalSessionDescriptionInit): Promise<void> {
    this.localDescription = description as RTCSessionDescription
  }

  async setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescription = description as RTCSessionDescription
  }

  async addIceCandidate(): Promise<void> {}
  addEventListener(): void {}
  removeEventListener(): void {}

  close(): void {
    this.connectionState = 'closed'
    this.onconnectionstatechange?.(new Event('connectionstatechange'))
  }
}

describe('browser direct client transport', () => {
  it('falls back when WebSocket construction throws synchronously', () => {
    const onFailure = vi.fn()
    const transport = new DirectClientTransport({
      signalURL: 'bad://url',
      sessionId: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
      sinceSeq: 0,
      clientInstanceId: 'client-1',
      accountKey: new Uint8Array(32),
      callbacks: { onFrame: vi.fn(), onReady: vi.fn(), onFailure },
      webSocketFactory: () => { throw new DOMException('bad URL', 'SyntaxError') },
    })
    expect(() => transport.start()).not.toThrow()
    expect(onFailure).toHaveBeenCalledOnce()
    expect(onFailure.mock.calls[0][0]).toBeInstanceOf(Error)
  })

  it('falls back when RTCPeerConnection construction throws synchronously', async () => {
    const ws = new FakeWebSocket()
    const onFailure = vi.fn()
    const transport = new DirectClientTransport({
      signalURL: 'wss://relay.example/direct-signal',
      sessionId: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
      sinceSeq: 0,
      clientInstanceId: 'client-1',
      accountKey: new Uint8Array(32).fill(0x42),
      callbacks: { onFrame: vi.fn(), onReady: vi.fn(), onFailure },
      webSocketFactory: () => ws as unknown as WebSocket,
      peerConnectionFactory: () => { throw new DOMException('unavailable', 'NotSupportedError') },
    })
    transport.start()
    ws.open()
    ws.message({ version: 1, kind: 'hello_ok' })
    const request = JSON.parse(ws.sent[1]) as DirectSignalMessage
    ws.message({
      version: 1,
      kind: 'direct_attempt',
      request_id: request.request_id,
      attempt_id: '11111111-2222-4333-8444-555555555555',
      ticket: ticketText(new Uint8Array(32).fill(0x73)),
      session_id: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
      user_id: 'user-1',
      host_id: 'host-1',
      client_instance_id: 'client-1',
      permission: 'control',
      expires_at_unix_ms: Date.now() + 60_000,
    })
    await waitFor(() => onFailure.mock.calls.length === 1)
    expect(onFailure.mock.calls[0][0].name).toBe('NotSupportedError')
  })

  it('completes signaling, WebRTC auth, encrypted records, and route loss fallback', async () => {
    const ws = new FakeWebSocket()
    const pc = new FakePeerConnection()
    const onAuthenticated = vi.fn()
    const onFrame = vi.fn()
    const onReady = vi.fn()
    const onFailure = vi.fn()
    const accountKey = new Uint8Array(32).fill(0x51)
    const sessionId = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
    const attemptId = '11111111-2222-4333-8444-555555555555'
    const ticket = new Uint8Array(32).fill(0x73)
    const expiresAt = Date.now() + 60_000
    const transport = new DirectClientTransport({
      signalURL: 'wss://relay.example/direct-signal',
      signalProtocols: ['atterm-token.token'],
      sessionId,
      sinceSeq: 7,
      clientInstanceId: 'client-1',
      accountKey,
      callbacks: { onAuthenticated, onFrame, onReady, onFailure },
      webSocketFactory: () => ws as unknown as WebSocket,
      peerConnectionFactory: () => pc as unknown as RTCPeerConnection,
    })
    transport.start()
    ws.open()
    expect(JSON.parse(ws.sent[0])).toMatchObject({ version: 1, kind: 'hello', role: 'client', client_instance_id: 'client-1' })

    ws.message({ version: 1, kind: 'hello_ok' })
    const request = JSON.parse(ws.sent[1]) as DirectSignalMessage
    expect(request).toMatchObject({ kind: 'direct_request', session_id: sessionId, client_instance_id: 'client-1', since_seq: 7 })
    ws.message({
      version: 1,
      kind: 'direct_attempt',
      request_id: request.request_id,
      attempt_id: attemptId,
      ticket: ticketText(ticket),
      session_id: sessionId,
      user_id: 'user-1',
      host_id: 'host-1',
      client_instance_id: 'client-1',
      permission: 'full',
      expires_at_unix_ms: expiresAt,
    })
    await waitFor(() => ws.sent.some((raw) => JSON.parse(raw).signal_type === 'offer'))
    const offer = ws.sent.map((raw) => JSON.parse(raw) as DirectSignalMessage).find((message) => message.signal_type === 'offer')
    expect(JSON.parse(offer?.payload ?? '{}')).toEqual({ type: 'offer', sdp: 'fake-offer' })

    ws.message({
      version: 1,
      kind: 'signal',
      attempt_id: attemptId,
      signal_type: 'answer',
      payload: JSON.stringify({ type: 'answer', sdp: 'fake-answer' }),
    })
    await waitFor(() => pc.remoteDescription !== null)

    pc.dc.open()
    expect(pc.dc.sent).toHaveLength(1)
    const clientHello = new Uint8Array(pc.dc.sent[0])
    expect(clientHello.slice(18, 50)).toEqual(ticket)
    const clientPublicKey = clientHello.slice(50)
    const host = await keyPair()
    const transcript = marshalDirectTranscript({
      attemptId,
      ticket,
      sessionId,
      userId: 'user-1',
      hostId: 'host-1',
      clientInstanceId: 'client-1',
      permission: DirectPermission.Full,
      expiresAtUnixMs: BigInt(expiresAt),
      clientPublicKey,
      hostPublicKey: host.publicKey,
    })
    const hostProof = buildDirectProof(accountKey, transcript, DirectRole.Host)
    pc.dc.message(join(new Uint8Array([1, 2]), host.publicKey, hostProof))
    await waitFor(() => pc.dc.sent.length === 2)
    expect(new Uint8Array(pc.dc.sent[1])).toHaveLength(66)

    const hostShared = await deriveP256SharedSecret(host.privateJWK, clientPublicKey)
    const keys = deriveDirectTrafficKeys(accountKey, hostShared, transcript)
    const transcriptHash = sha256(transcript)
    pc.dc.message(new Uint8Array([1, 4]))
    await waitFor(() => onAuthenticated.mock.calls.length === 1)

    const frame = new Uint8Array([1, 3, 3, 7])
    pc.dc.message(sealDirectRecord(
      keys.hostToClientKey,
      keys.hostToClientNoncePrefix,
      transcriptHash,
      DirectRecordKind.Frame,
      0n,
      frame,
    ))
    const ready = new Uint8Array(8)
    new DataView(ready.buffer).setBigUint64(0, 7n, false)
    pc.dc.message(sealDirectRecord(
      keys.hostToClientKey,
      keys.hostToClientNoncePrefix,
      transcriptHash,
      DirectRecordKind.DirectReady,
      1n,
      ready,
    ))
    await waitFor(() => onFrame.mock.calls.length === 1 && onReady.mock.calls.length === 1)
    expect(onFrame).toHaveBeenCalledWith(frame)
    expect(onReady).toHaveBeenCalledWith(7)

    expect(transport.sendFrame(new Uint8Array([9, 8, 7]))).toBe(true)
    const clientRecord = new Uint8Array(pc.dc.sent[2])
    expect(openDirectRecord(
      keys.clientToHostKey,
      keys.clientToHostNoncePrefix,
      transcriptHash,
      0n,
      clientRecord,
    ).plaintext).toEqual(new Uint8Array([9, 8, 7]))

    ws.close()
    expect(onFailure).not.toHaveBeenCalled()
    pc.dc.close()
    expect(onFailure).toHaveBeenCalledOnce()
    expect(onFailure.mock.calls[0][0].message).toContain('data channel closed')
  })
})
