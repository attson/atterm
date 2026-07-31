import { encodeFrame, decodeFrame, decodeText, TYPE, NIL_SID } from '@/lib/proto'
import { fakeSessions } from './fakeSessions'
import { replayScripts, PROMPT } from './replayScripts'
import { runFakeCommand } from './fakeCommands'
import { createMockRemoteFS, type FSRequestLike } from './mockFs'

// 一个最小 WebSocket 替身:根据 url path 区分「会话列表连接」(/client-sessions)
// 与「单会话连接」(/client)。不做真实网络,用真实 proto 帧编解码,保证前端
// 解析路径(SessionListConnection / SessionConnection)与线上一致。

const enc = new TextEncoder()
const dec = new TextDecoder()

// OUT payload = 8 字节 big-endian seq (hi u32 + lo u32) + data,见 lib/proto.ts
// 的 decodeOutPayload。这里做反向编码。
function encodeOutPayload(seq: number, text: string): Uint8Array {
  const data = enc.encode(text)
  const out = new Uint8Array(8 + data.length)
  const dv = new DataView(out.buffer)
  dv.setUint32(0, Math.floor(seq / 0x100000000), false)
  dv.setUint32(4, seq >>> 0, false)
  out.set(data, 8)
  return out
}

// LIST_RESP 期望 SessionInfo[](用 id 字段),而 fakeSessions 是 RemoteSession
// (用 session_id)。映射到 SessionInfo 形状。
function toSessionInfoJSON(): string {
  const infos = fakeSessions.map((s) => ({
    id: s.session_id,
    command: s.current_command ?? '',
    cwd: s.cwd ?? '',
    title: s.title,
    cols: s.cols,
    rows: s.rows,
    started_at: s.started_at ?? 0,
    host_id: s.host_id,
    host: s.host,
    user: s.user,
    remote_permission: s.remote_permission,
    task_state: s.task_state,
    current_command: s.current_command,
    command_started_at: s.command_started_at,
    command_ended_at: s.command_ended_at,
    command_duration_ms: s.command_duration_ms,
    command_exit_code: s.command_exit_code,
    last_output_at: s.last_output_at,
    type: s.type,
    unread: s.unread,
  }))
  return JSON.stringify(infos)
}

type Handler = (ev: any) => void

export class MockSocket {
  static onNotify: ((title: string, body: string) => void) | null = null

  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSING = 2
  static readonly CLOSED = 3

  url: string
  readyState = MockSocket.CONNECTING
  binaryType = 'arraybuffer'

  private handlers: Record<string, Handler[]> = {}
  private isList: boolean
  private seq = 1
  private idleBuffer = ''
  private clientID = 'demo-client'
  // 单会话连接的文件系统(remote 会话的文件浏览器走 FS_REQUEST 帧)。
  private remoteFS = createMockRemoteFS()

  constructor(url: string, _protocols?: string | string[]) {
    this.url = url
    this.isList = url.includes('/client-sessions')
    setTimeout(() => {
      this.readyState = MockSocket.OPEN
      this.fire('open', {})
      // 会话列表连接:open 后 server 主动推快照(前端不发 LIST 请求)。
      if (this.isList) {
        this.deliver(encodeFrame(TYPE.LIST_RESP, NIL_SID, enc.encode(toSessionInfoJSON())))
      }
    }, 0)
  }

  // 兼容属性式(SessionConnection/SessionListConnection 用 ws.onmessage = …)与
  // addEventListener 两种订阅方式。
  set onopen(fn: Handler) { this.add('open', fn) }
  set onmessage(fn: Handler) { this.add('message', fn) }
  set onclose(fn: Handler) { this.add('close', fn) }
  set onerror(fn: Handler) { this.add('error', fn) }
  addEventListener(type: string, fn: Handler) { this.add(type, fn) }
  removeEventListener(type: string, fn: Handler) {
    this.handlers[type] = (this.handlers[type] || []).filter((f) => f !== fn)
  }

  private add(type: string, fn: Handler) { (this.handlers[type] ||= []).push(fn) }
  private fire(type: string, ev: any) { for (const fn of this.handlers[type] || []) fn(ev) }
  private deliver(bytes: Uint8Array) {
    const buf = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
    this.fire('message', { data: buf })
  }
  private out(sid: Uint8Array, text: string) {
    this.deliver(encodeFrame(TYPE.OUT, sid, encodeOutPayload(this.seq++, text)))
  }

  send(data: ArrayBuffer | Uint8Array | string) {
    if (typeof data === 'string') return
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data)
    let frame
    try { frame = decodeFrame(bytes) } catch { return }
    if (!frame) return

    if (frame.type === TYPE.ATTACH) {
      const sidStr = uuidStringify(frame.sid)
      // 记录本连接 attach 的 client_id(ATTACH payload 里带),用于后续 META
      // 授予 driver 角色。
      try {
        const p = JSON.parse(dec.decode(frame.payload)) as { client_id?: string }
        if (p.client_id) this.clientID = p.client_id
      } catch {
        /* ignore */
      }
      const script = replayScripts[sidStr] || []
      let delay = 60
      for (const chunk of script) {
        const s = frame.sid
        setTimeout(() => this.out(s, chunk), delay)
        delay += 220
      }
      // 所有会话回放完成后都授予 driver,让访客在任意会话都能敲命令(真实
      // atterm 里 remote 会话默认 viewer 需 take over;demo 为体验起见直接授权)。
      // 非 idle 会话回放结尾已带 prompt,续敲命令即可。
      setTimeout(() => this.grantDriver(frame.sid), delay + 60)
      return
    }

    if (frame.type === TYPE.CLAIM_DRIVER) {
      // 用户按空格 take over:对任意会话授予 driver。
      this.grantDriver(frame.sid)
      return
    }

    if (frame.type === TYPE.FS_REQUEST) {
      // remote 会话的文件浏览器:解析请求,用内存树生成响应,回 FS_RESPONSE。
      let req: FSRequestLike
      try {
        req = JSON.parse(dec.decode(frame.payload)) as FSRequestLike
      } catch {
        return
      }
      const resp = this.remoteFS.handleFSRequest(req)
      this.deliver(encodeFrame(TYPE.FS_RESPONSE, frame.sid, enc.encode(JSON.stringify(resp))))
      return
    }

    if (frame.type === TYPE.IN) {
      // 所有会话都接受输入,走同一套假命令响应表。
      this.handleInput(frame.sid, dec.decode(frame.payload))
      return
    }
    // RESIZE / PASTE_* 等:mock 忽略
  }

  private grantDriver(sid: Uint8Array) {
    const meta = JSON.stringify({
      driver_client_id: this.clientID,
      driver_client_name: 'you',
    })
    this.deliver(encodeFrame(TYPE.META, sid, enc.encode(meta)))
  }

  private handleInput(sid: Uint8Array, s: string) {
    for (const ch of s) {
      if (ch === '\r' || ch === '\n') {
        this.out(sid, '\r\n')
        const res = runFakeCommand(this.idleBuffer)
        this.idleBuffer = ''
        if (res.longRunning && res.steps) {
          let d = 120
          for (const step of res.steps) {
            const st = step
            setTimeout(() => this.out(sid, st), d)
            d += 300
          }
          setTimeout(() => {
            this.out(sid, '\r\n' + PROMPT)
            MockSocket.onNotify?.('Task completed', 'codex exec finished (3 files changed)')
          }, d)
        } else {
          if (res.output) this.out(sid, res.output)
          this.out(sid, PROMPT)
        }
      } else if (ch === '\x7f' || ch === '\b') {
        if (this.idleBuffer.length) {
          this.idleBuffer = this.idleBuffer.slice(0, -1)
          this.out(sid, '\b \b')
        }
      } else {
        this.idleBuffer += ch
        this.out(sid, ch) // echo
      }
    }
  }

  close() {
    this.readyState = MockSocket.CLOSED
    this.fire('close', { code: 1000, wasClean: true })
  }
}

function uuidStringify(sid: Uint8Array): string {
  const h = Array.from(sid)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

// 在 demo 容器生命周期内替换全局 WebSocket,返回 restore 函数。
export function installMockWebSocket(): () => void {
  const g = globalThis as any
  const original = g.WebSocket
  g.WebSocket = MockSocket
  return () => {
    g.WebSocket = original
  }
}
