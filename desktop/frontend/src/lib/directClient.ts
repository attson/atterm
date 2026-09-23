import { DirectFrameReassembler } from './directFraming'
import {
  decodeDirectTicket,
  directPermission,
  DirectClientHandshake,
  DirectClientRecordCodec,
  type DirectAuthorization,
} from './directHandshake'
import { DirectRecordKind } from './directCrypto'

const DIRECT_SIGNAL_VERSION = 1
const DIRECT_SIGNAL_LIMIT = 64 * 1024
const DIRECT_ATTEMPT_TIMEOUT_MS = 30_000
const DIRECT_BUFFERED_HIGH_WATER = 1024 * 1024

export interface DirectSignalMessage {
  version: number
  kind: string
  role?: string
  host_id?: string
  session_ids?: string[]
  client_instance_id?: string
  request_id?: string
  session_id?: string
  since_seq?: number
  attempt_id?: string
  ticket?: string
  user_id?: string
  permission?: string
  expires_at_unix_ms?: number
  signal_type?: string
  payload?: string
  code?: string
  message?: string
}

export interface DirectClientCallbacks {
  onAuthenticated?: () => void
  onFrame: (frame: Uint8Array) => void
  onReady: (lastReplayedSeq: number) => void
  onFailure: (error: Error) => void
}

export interface DirectClientOptions {
  signalURL: string
  signalProtocols?: string[]
  sessionId: string
  sinceSeq: number
  clientInstanceId: string
  accountKey: Uint8Array
  callbacks: DirectClientCallbacks
  iceServers?: RTCIceServer[]
  timeoutMs?: number
  webSocketFactory?: (url: string, protocols?: string[]) => WebSocket
  peerConnectionFactory?: (configuration: RTCConfiguration) => RTCPeerConnection
}

function parseSignalMessage(data: unknown): DirectSignalMessage {
  if (typeof data !== 'string' || data.length > DIRECT_SIGNAL_LIMIT) throw new Error('invalid direct signaling message')
  const value: unknown = JSON.parse(data)
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid direct signaling message')
  const message = value as Partial<DirectSignalMessage>
  if (message.version !== DIRECT_SIGNAL_VERSION || typeof message.kind !== 'string') {
    throw new Error('unsupported direct signaling message')
  }
  return message as DirectSignalMessage
}

function signalJSON(message: Omit<DirectSignalMessage, 'version'>): string {
  return JSON.stringify({ version: DIRECT_SIGNAL_VERSION, ...message })
}

function asError(value: unknown): Error {
  if (value instanceof Error) return value
  if (value && typeof value === 'object') {
    const structured = value as { name?: unknown; message?: unknown }
    const error = new Error(typeof structured.message === 'string' ? structured.message : String(value))
    if (typeof structured.name === 'string' && structured.name) error.name = structured.name
    return error
  }
  return new Error(String(value))
}

function dataChannelBuffer(bytes: Uint8Array): ArrayBuffer {
  const buffer = new ArrayBuffer(bytes.byteLength)
  new Uint8Array(buffer).set(bytes)
  return buffer
}

async function waitForICEGathering(pc: RTCPeerConnection): Promise<void> {
  if (pc.iceGatheringState === 'complete') return
  await new Promise<void>((resolve) => {
    const changed = () => {
      if (pc.iceGatheringState !== 'complete') return
      pc.removeEventListener('icegatheringstatechange', changed)
      resolve()
    }
    pc.addEventListener('icegatheringstatechange', changed)
  })
}

/** One Relay-authorized browser direct attempt. It is intentionally one-shot:
 * any signaling, ICE, handshake, record, or callback failure closes only this
 * route and asks SessionConnection to remain on/fall back to Relay. */
export class DirectClientTransport {
  private signalWS: WebSocket | null = null
  private pc: RTCPeerConnection | null = null
  private dc: RTCDataChannel | null = null
  private handshake: DirectClientHandshake | null = null
  private codec: DirectClientRecordCodec | null = null
  private readonly reassembler = new DirectFrameReassembler()
  private readonly requestId = crypto.randomUUID()
  private attemptId = ''
  private started = false
  private helloAccepted = false
  private closed = false
  private authenticated = false
  private hostHelloHandled = false
  private timeout: number | null = null
  private signalChain = Promise.resolve()
  private receiveChain = Promise.resolve()
  private readonly accountKey: Uint8Array

  constructor(private readonly options: DirectClientOptions) {
    if (options.accountKey.length !== 32) throw new Error('direct transport requires a 32-byte account_key')
    if (!Number.isSafeInteger(options.sinceSeq) || options.sinceSeq < 0) throw new Error('invalid direct replay cursor')
    if (!options.clientInstanceId || options.clientInstanceId.length > 128) throw new Error('invalid direct client instance id')
    this.accountKey = options.accountKey.slice()
  }

  start(): void {
    if (this.started || this.closed) return
    this.started = true
    const createWebSocket = this.options.webSocketFactory ?? ((url, protocols) => protocols ? new WebSocket(url, protocols) : new WebSocket(url))
    let ws: WebSocket
    try {
      ws = createWebSocket(this.options.signalURL, this.options.signalProtocols)
    } catch (error) {
      this.fail(error)
      return
    }
    this.signalWS = ws
    const timeoutMs = this.options.timeoutMs ?? DIRECT_ATTEMPT_TIMEOUT_MS
    this.timeout = window.setTimeout(() => this.fail(new Error('direct attempt timed out')), timeoutMs)
    ws.onopen = () => {
      this.sendSignal({
        kind: 'hello',
        role: 'client',
        client_instance_id: this.options.clientInstanceId,
      })
    }
    ws.onmessage = (event) => {
      try {
        this.handleSignal(parseSignalMessage(event.data))
      } catch (error) {
        this.fail(error)
      }
    }
    ws.onerror = () => {
      // Browser WebSocket exposes useful detail only through the following close.
    }
    ws.onclose = () => {
      this.signalWS = null
      if (!this.authenticated) this.fail(new Error('direct signaling disconnected'))
    }
  }

  sendFrame(frame: Uint8Array): boolean {
    if (this.closed || !this.authenticated || !this.codec || !this.dc || this.dc.readyState !== 'open') return false
    if (this.dc.bufferedAmount > DIRECT_BUFFERED_HIGH_WATER) {
      this.fail(new Error('direct data channel backpressure limit'))
      return false
    }
    try {
      for (const record of this.codec.sealFrame(frame)) this.dc.send(dataChannelBuffer(record))
      return true
    } catch (error) {
      this.fail(error)
      return false
    }
  }

  close(): void {
    this.finish()
  }

  private handleSignal(message: DirectSignalMessage): void {
    if (this.closed) return
    switch (message.kind) {
      case 'hello_ok':
        if (this.helloAccepted) throw new Error('duplicate direct hello acknowledgement')
        this.helloAccepted = true
        this.sendSignal({
          kind: 'direct_request',
          request_id: this.requestId,
          session_id: this.options.sessionId,
          client_instance_id: this.options.clientInstanceId,
          since_seq: this.options.sinceSeq,
        })
        return
      case 'direct_attempt':
        if (message.request_id !== this.requestId) throw new Error('direct request id mismatch')
        void this.beginAttempt(message).catch((error) => this.fail(error))
        return
      case 'signal':
        if (!this.attemptId || message.attempt_id !== this.attemptId) throw new Error('direct signal attempt mismatch')
        this.signalChain = this.signalChain
          .then(() => this.applyRemoteSignal(message))
          .catch((error) => this.fail(error))
        return
      case 'consumed':
        if (message.attempt_id !== this.attemptId) throw new Error('direct consumed attempt mismatch')
        return
      case 'cancel':
        throw new Error(`direct attempt cancelled: ${message.code || 'peer_cancelled'}`)
      case 'error':
        throw new Error(`direct signaling rejected: ${message.code || 'unknown_error'}`)
      case 'host_registered':
        return
      default:
        throw new Error(`unexpected direct signaling message: ${message.kind}`)
    }
  }

  private async beginAttempt(message: DirectSignalMessage): Promise<void> {
    if (this.attemptId) throw new Error('duplicate direct attempt')
    if (!message.attempt_id || !message.ticket || !message.session_id || !message.user_id || !message.host_id ||
        !message.client_instance_id || !message.permission || !Number.isSafeInteger(message.expires_at_unix_ms)) {
      throw new Error('incomplete direct authorization')
    }
    if (message.session_id !== this.options.sessionId || message.client_instance_id !== this.options.clientInstanceId) {
      throw new Error('direct authorization claims mismatch')
    }
    const expiresAt = BigInt(message.expires_at_unix_ms as number)
    const authorization: DirectAuthorization = {
      attemptId: message.attempt_id,
      ticket: decodeDirectTicket(message.ticket),
      sessionId: message.session_id,
      userId: message.user_id,
      hostId: message.host_id,
      clientInstanceId: message.client_instance_id,
      permission: directPermission(message.permission),
      expiresAtUnixMs: expiresAt,
    }
    this.attemptId = authorization.attemptId
    this.handshake = await DirectClientHandshake.create(authorization, this.accountKey)
    if (this.closed) return

    const createPeerConnection = this.options.peerConnectionFactory ?? ((configuration) => new RTCPeerConnection(configuration))
    const pc = createPeerConnection({
      iceServers: this.options.iceServers ?? [{ urls: ['stun:stun.cloudflare.com:3478'] }],
    })
    this.pc = pc
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected' || pc.connectionState === 'closed') {
        this.fail(new Error(`direct peer connection ${pc.connectionState}`))
      }
    }
    const dc = pc.createDataChannel('atterm-terminal-v1', { ordered: true })
    if (!dc.ordered || dc.maxPacketLifeTime !== null || dc.maxRetransmits !== null) {
      throw new Error('direct data channel is not ordered and reliable')
    }
    this.dc = dc
    dc.binaryType = 'arraybuffer'
    dc.onopen = () => {
      try {
        if (!this.handshake) throw new Error('direct handshake unavailable')
        dc.send(dataChannelBuffer(this.handshake.clientHello()))
      } catch (error) {
        this.fail(error)
      }
    }
    dc.onmessage = (event) => {
      if (!(event.data instanceof ArrayBuffer)) {
        this.fail(new Error('direct data channel requires binary messages'))
        return
      }
      const data = new Uint8Array(event.data)
      this.receiveChain = this.receiveChain
        .then(() => this.handleDataMessage(data))
        .catch((error) => this.fail(error))
    }
    dc.onerror = () => this.fail(new Error('direct data channel error'))
    dc.onclose = () => this.fail(new Error('direct data channel closed'))

    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    await waitForICEGathering(pc)
    if (!pc.localDescription) throw new Error('direct local offer unavailable')
    this.sendSignal({
      kind: 'signal',
      attempt_id: this.attemptId,
      signal_type: 'offer',
      payload: JSON.stringify(pc.localDescription),
    })
    this.sendSignal({kind: 'signal', attempt_id: this.attemptId, signal_type: 'ice_end', payload: ''})
  }

  private async applyRemoteSignal(message: DirectSignalMessage): Promise<void> {
    if (!this.pc || typeof message.signal_type !== 'string' || typeof message.payload !== 'string') {
      throw new Error('direct peer connection unavailable')
    }
    switch (message.signal_type) {
      case 'answer': {
        const answer: unknown = JSON.parse(message.payload)
        if (!answer || typeof answer !== 'object' || (answer as RTCSessionDescriptionInit).type !== 'answer') {
          throw new Error('invalid direct answer')
        }
        await this.pc.setRemoteDescription(answer as RTCSessionDescriptionInit)
        return
      }
      case 'ice_candidate': {
        const candidate: unknown = JSON.parse(message.payload)
        if (!candidate || typeof candidate !== 'object') throw new Error('invalid direct ICE candidate')
        await this.pc.addIceCandidate(candidate as RTCIceCandidateInit)
        return
      }
      case 'ice_end':
        return
      default:
        throw new Error('unsupported direct signal')
    }
  }

  private async handleDataMessage(message: Uint8Array): Promise<void> {
    if (!this.handshake || !this.dc) throw new Error('direct handshake unavailable')
    if (!this.hostHelloHandled) {
      const finish = await this.handshake.handleHostHello(message)
      this.hostHelloHandled = true
      this.dc.send(dataChannelBuffer(finish))
      return
    }
    if (!this.authenticated) {
      const keys = this.handshake.handleAuthOK(message)
      this.codec = new DirectClientRecordCodec(keys)
      this.authenticated = true
      if (this.timeout !== null) {
        window.clearTimeout(this.timeout)
        this.timeout = null
      }
      this.accountKey.fill(0)
      this.options.callbacks.onAuthenticated?.()
      return
    }
    if (!this.codec) throw new Error('direct record codec unavailable')
    const opened = this.codec.open(message)
    switch (opened.kind) {
      case DirectRecordKind.Frame:
        this.options.callbacks.onFrame(opened.plaintext)
        return
      case DirectRecordKind.Fragment: {
        const frame = this.reassembler.add(opened.plaintext)
        if (frame) this.options.callbacks.onFrame(frame)
        return
      }
      case DirectRecordKind.DirectReady: {
        if (opened.plaintext.length !== 8) throw new Error('invalid DIRECT_READY payload')
        const replayedSeq = new DataView(
          opened.plaintext.buffer,
          opened.plaintext.byteOffset,
          opened.plaintext.byteLength,
        ).getBigUint64(0, false)
        if (replayedSeq > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('DIRECT_READY sequence exceeds client range')
        this.options.callbacks.onReady(Number(replayedSeq))
        return
      }
      case DirectRecordKind.Ping:
        if (opened.plaintext.length !== 8) throw new Error('invalid direct PING payload')
        this.dc.send(dataChannelBuffer(this.codec.seal(DirectRecordKind.Pong, opened.plaintext)))
        return
      case DirectRecordKind.Pong:
        if (opened.plaintext.length !== 8) throw new Error('invalid direct PONG payload')
        return
      case DirectRecordKind.Close:
        throw new Error('direct peer closed route')
      default:
        throw new Error('unsupported direct record')
    }
  }

  private sendSignal(message: Omit<DirectSignalMessage, 'version'>): void {
    if (!this.signalWS || this.signalWS.readyState !== WebSocket.OPEN) throw new Error('direct signaling is not open')
    this.signalWS.send(signalJSON(message))
  }

  private fail(value: unknown): void {
    if (this.closed) return
    const error = asError(value)
    this.finish()
    try { this.options.callbacks.onFailure(error) } catch { /* fallback callbacks must not escape network handlers */ }
  }

  private finish(): void {
    if (this.closed) return
    this.closed = true
    if (this.timeout !== null) {
      window.clearTimeout(this.timeout)
      this.timeout = null
    }
    this.accountKey.fill(0)
    this.handshake?.dispose()
    this.handshake = null
    this.codec = null
    this.reassembler.reset()
    const ws = this.signalWS
    const dc = this.dc
    const pc = this.pc
    this.signalWS = null
    this.dc = null
    this.pc = null
    try { ws?.close() } catch { /* ignore */ }
    try { dc?.close() } catch { /* ignore */ }
    try { pc?.close() } catch { /* ignore */ }
    this.signalChain = Promise.resolve()
    this.receiveChain = Promise.resolve()
  }
}
