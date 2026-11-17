import { encodeFrame, decodeFrame, decodeText, TYPE, NIL_SID } from '@/lib/proto'
import { fakeSessions } from './fakeSessions'
import { replayScripts, PROMPT, IDLE_REPLAY, LOCAL_PROMPT } from './replayScripts'
import { runFakeCommand, type FakeAgentKind } from './fakeCommands'
import { createMockRemoteFS, type FSRequestLike } from './mockFs'
import { localSessions, onLocalSessionsChanged } from './localSessions'
import { LOCAL_ENDPOINT_URL } from './mockGoApp'
import { recordMockInputHistory } from './mockInputHistory'

// 一个最小 WebSocket 替身:根据 url path 区分「会话列表连接」(/client-sessions)
// 与「单会话连接」(/client)。不做真实网络,用真实 proto 帧编解码,保证前端
// 解析路径(SessionListConnection / SessionConnection)与线上一致。

const enc = new TextEncoder()
const dec = new TextDecoder()
const MAX_PASTE_IMAGE_BYTES = 10 * 1024 * 1024

interface PasteImagePayload {
  filename?: unknown
  content_type?: unknown
  data?: unknown
}

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
function remoteSessionInfoJSON(): string {
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

// 本机会话列表(localSessions 已是 SessionInfo 形状)。
function localSessionInfoJSON(): string {
  return JSON.stringify(localSessions)
}

// 已知会话 id 集合:凡在 fakeSessions/localSessions 里、或有专属回放脚本的,
// attach 时走对应脚本;其余(新建的本机会话)走 IDLE_REPLAY。
function isLocalSid(sid: string): boolean {
  return localSessions.some((s) => s.id === sid)
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
  private isLocal: boolean
  private seq = 1
  private idleBuffer = ''
  private cursorIndex = 0
  private lastInputWasBulk = false
  private agentMode: FakeAgentKind | null = null
  private replaying = false
  private commandBusy = false
  private pastedImageCount = 0
  private pendingAgentImages: number[] = []
  private queuedInput: Array<{ sid: Uint8Array; text: string }> = []
  private prompt = PROMPT
  private clientID = 'demo-client'
  private unsubLocal: (() => void) | null = null
  // 单会话连接的文件系统(remote 会话的文件浏览器走 FS_REQUEST 帧)。
  private remoteFS = createMockRemoteFS()

  constructor(url: string, _protocols?: string | string[]) {
    this.url = url
    this.isList = url.includes('/client-sessions')
    // 本机连接 vs 远程连接:GetEndpoint() 返回 LOCAL_ENDPOINT_URL(含 local.demo),
    // relay 连接用别的 host。本机列表推 localSessions,远程列表推 fakeSessions。
    this.isLocal = url.includes('local.demo') || url.startsWith(LOCAL_ENDPOINT_URL)
    setTimeout(() => {
      this.readyState = MockSocket.OPEN
      this.fire('open', {})
      if (this.isList) {
        this.pushList()
        // 本机列表:订阅 localSessions 变更(新建/关闭会话)后重推快照。
        if (this.isLocal) {
          this.unsubLocal = onLocalSessionsChanged(() => {
            if (this.readyState === MockSocket.OPEN) this.pushList()
          })
        }
      }
    }, 0)
  }

  private pushList() {
    const json = this.isLocal ? localSessionInfoJSON() : remoteSessionInfoJSON()
    this.deliver(encodeFrame(TYPE.LIST_RESP, NIL_SID, enc.encode(json)))
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
      this.replaying = true
      const sidStr = uuidStringify(frame.sid)
      // 记录本连接 attach 的 client_id(ATTACH payload 里带),用于后续 META
      // 授予 driver 角色。
      try {
        const p = JSON.parse(dec.decode(frame.payload)) as { client_id?: string }
        if (p.client_id) this.clientID = p.client_id
      } catch {
        /* ignore */
      }
      // 专属回放脚本(codex/claude/npm/go/预置 idle);本机新建会话无专属脚本,
      // 走 IDLE_REPLAY 并用本机 prompt,可直接敲命令。
      let script = replayScripts[sidStr]
      if (!script) {
        script = IDLE_REPLAY
        this.prompt = LOCAL_PROMPT
      } else if (this.isLocal || isLocalSid(sidStr)) {
        this.prompt = LOCAL_PROMPT
      }
      let delay = 60
      for (const chunk of script) {
        const s = frame.sid
        setTimeout(() => this.out(s, chunk), delay)
        delay += 220
      }
      // 所有会话回放完成后都授予 driver,让访客在任意会话都能敲命令(真实
      // atterm 里 remote 会话默认 viewer 需 take over;demo 为体验起见直接授权)。
      setTimeout(() => {
        this.grantDriver(frame.sid)
        this.replaying = false
        this.flushQueuedInput()
      }, delay + 60)
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

    if (frame.type === TYPE.PASTE_IMAGE) {
      this.handlePasteImage(frame.sid, frame.payload)
      return
    }

    if (frame.type === TYPE.IN) {
      // 所有会话都接受输入,走同一套假命令响应表。
      const text = dec.decode(frame.payload)
      if (this.replaying || this.commandBusy) {
        this.queuedInput.push({ sid: frame.sid.slice(), text })
      } else {
        this.handleInput(frame.sid, text)
      }
      return
    }
    // RESIZE / PASTE_FILE 等:mock 忽略
  }

  private handlePasteImage(sid: Uint8Array, raw: Uint8Array) {
    let payload: PasteImagePayload
    try {
      payload = JSON.parse(dec.decode(raw)) as PasteImagePayload
    } catch {
      this.reportPasteImageError(sid, 'invalid image payload')
      return
    }

    const filename = safeImageFilename(payload.filename)
    const contentType = safeImageContentType(payload.content_type)
    const data = typeof payload.data === 'string' ? payload.data : ''
    const bytes = decodedBase64Size(data)
    if (!filename || !contentType || bytes === null || bytes === 0) {
      this.reportPasteImageError(sid, 'invalid image payload')
      return
    }
    if (bytes > MAX_PASTE_IMAGE_BYTES) {
      this.reportPasteImageError(sid, 'image exceeds the 10 MiB demo limit')
      return
    }

    this.pastedImageCount += 1
    if (this.agentMode) {
      this.pendingAgentImages.push(this.pastedImageCount)
      // Keep the attachment token in the active input line so the next text
      // appears after it, matching Codex's composer interaction.
      this.out(sid, `\r\x1b[2K${this.currentInputLine()}`)
      return
    }
    const label = `[Image #${this.pastedImageCount}] ${filename}`
    const image = `${label} (${contentType}, ${formatBytes(bytes)})`
    this.out(sid, `\r\n\x1b[1;35m${image}\x1b[0m\r\nImage received. Start Codex, Claude, or Aider to discuss it.\r\n${this.currentPrompt()}${this.inputBufferDisplay()}`)
  }

  private reportPasteImageError(sid: Uint8Array, message: string) {
    this.out(sid, `\r\n\x1b[31mImage paste failed: ${message}.\x1b[0m\r\n${this.currentInputLine()}`)
  }

  private currentPrompt(): string {
    return this.agentMode ? agentPrompt(this.agentMode) : this.prompt
  }

  private inputBufferDisplay(): string {
    const afterCursor = this.idleBuffer.length - this.cursorIndex
    return this.idleBuffer + cursorLeft(afterCursor)
  }

  private currentInputLine(): string {
    const attachments = this.agentMode
      ? this.pendingAgentImages.map((number) => `\x1b[1;35m[Image #${number}]\x1b[0m`).join(' ')
      : ''
    const separator = attachments ? ' ' : ''
    return `${this.currentPrompt()}${attachments}${separator}${this.inputBufferDisplay()}`
  }

  private grantDriver(sid: Uint8Array) {
    const meta = JSON.stringify({
      driver_client_id: this.clientID,
      driver_client_name: 'you',
    })
    this.deliver(encodeFrame(TYPE.META, sid, enc.encode(meta)))
  }

  private handleInput(sid: Uint8Array, s: string) {
    const bulk = s.length > 1
      && !s.includes('\r')
      && !s.includes('\n')
      && !s.includes('\x1b')
      && !s.includes('\x7f')
      && !s.includes('\b')
    if (bulk && this.lastInputWasBulk && this.idleBuffer) {
      this.submitInputLine(sid)
    } else if (!bulk) {
      this.lastInputWasBulk = false
    }

    for (let index = 0; index < s.length; index++) {
      const control = readInputControl(s, index)
      if (control) {
        this.handleInputControl(sid, control.name)
        index += control.length - 1
        continue
      }

      const ch = s[index]
      if (ch === '\r' || ch === '\n') {
        this.submitInputLine(sid)
        if (this.commandBusy && index + 1 < s.length) {
          this.queuedInput.push({ sid: sid.slice(), text: s.slice(index + 1) })
          return
        }
      } else if (ch === '\x7f' || ch === '\b') {
        this.backspaceInput(sid)
      } else if (ch === '\x01') {
        this.moveInputCursor(sid, -this.cursorIndex)
      } else if (ch === '\x05') {
        this.moveInputCursor(sid, this.idleBuffer.length - this.cursorIndex)
      } else if (ch === '\x03') {
        this.cancelInputLine(sid)
      } else if (ch < ' ') {
        // Ignore shell controls the demo does not model instead of treating
        // their bytes as a command name.
      } else {
        this.insertInput(sid, ch)
      }
    }
    if (bulk) this.lastInputWasBulk = true
  }

  private submitInputLine(sid: Uint8Array) {
    this.out(sid, '\r\n')
    const command = this.idleBuffer
    const historyText = this.agentMode && this.pendingAgentImages.length > 0
      ? [
          ...this.pendingAgentImages.map((number) => `[Image #${number}]`),
          command,
        ].filter(Boolean).join(' ')
      : command
    this.idleBuffer = ''
    this.cursorIndex = 0
    this.lastInputWasBulk = false
    recordMockInputHistory(uuidStringify(sid), historyText)

    if (this.agentMode) {
      this.submitAgentLine(sid, command)
      return
    }

    const res = runFakeCommand(command)
    if (res.interactive) {
      this.agentMode = res.interactive
      this.pendingAgentImages = []
      this.out(sid, agentWelcome(res.interactive))
      this.out(sid, agentPrompt(res.interactive))
      return
    }
    if (!res.longRunning || !res.steps) {
      if (res.output) this.out(sid, res.output)
      this.out(sid, this.prompt)
      return
    }

    this.commandBusy = true
    let delay = 120
    for (const step of res.steps) {
      setTimeout(() => this.out(sid, step), delay)
      delay += 300
    }
    setTimeout(() => {
      this.out(sid, '\r\n' + this.prompt)
      this.commandBusy = false
      MockSocket.onNotify?.('Task completed', 'codex exec finished (3 files changed)')
      this.flushQueuedInput()
    }, delay)
  }

  private insertInput(sid: Uint8Array, text: string) {
    const before = this.idleBuffer.slice(0, this.cursorIndex)
    const after = this.idleBuffer.slice(this.cursorIndex)
    this.idleBuffer = before + text + after
    this.cursorIndex += text.length
    this.out(sid, text + after + cursorLeft(after.length))
  }

  private backspaceInput(sid: Uint8Array) {
    if (this.cursorIndex === 0) return
    const before = this.idleBuffer.slice(0, this.cursorIndex - 1)
    const after = this.idleBuffer.slice(this.cursorIndex)
    this.idleBuffer = before + after
    this.cursorIndex -= 1
    this.out(sid, `\b${after} ${cursorLeft(after.length + 1)}`)
  }

  private deleteInput(sid: Uint8Array) {
    if (this.cursorIndex >= this.idleBuffer.length) return
    const before = this.idleBuffer.slice(0, this.cursorIndex)
    const after = this.idleBuffer.slice(this.cursorIndex + 1)
    this.idleBuffer = before + after
    this.out(sid, `${after} ${cursorLeft(after.length + 1)}`)
  }

  private moveInputCursor(sid: Uint8Array, delta: number) {
    const next = Math.max(0, Math.min(this.idleBuffer.length, this.cursorIndex + delta))
    const distance = next - this.cursorIndex
    if (distance < 0) this.out(sid, cursorLeft(-distance))
    if (distance > 0) this.out(sid, cursorRight(distance))
    this.cursorIndex = next
  }

  private handleInputControl(sid: Uint8Array, control: InputControl) {
    this.lastInputWasBulk = false
    if (control === 'left') this.moveInputCursor(sid, -1)
    else if (control === 'right') this.moveInputCursor(sid, 1)
    else if (control === 'home') this.moveInputCursor(sid, -this.cursorIndex)
    else if (control === 'end') this.moveInputCursor(sid, this.idleBuffer.length - this.cursorIndex)
    else if (control === 'delete') this.deleteInput(sid)
    // Up/down and bracketed-paste markers are intentionally ignored. They
    // must never leak into the submitted shell command.
  }

  private cancelInputLine(sid: Uint8Array) {
    this.idleBuffer = ''
    this.cursorIndex = 0
    this.lastInputWasBulk = false
    this.pendingAgentImages = []
    this.out(sid, `^C\r\n${this.agentMode ? agentPrompt(this.agentMode) : this.prompt}`)
  }

  private submitAgentLine(sid: Uint8Array, raw: string) {
    const line = raw.trim()
    const agent = this.agentMode!
    this.pendingAgentImages = []
    if (line === '/exit' || line === 'exit') {
      this.agentMode = null
      this.out(sid, `\x1b[2mLeaving ${agentLabel(agent)} demo.\x1b[0m\r\n${this.prompt}`)
      return
    }
    if (!line) {
      this.out(sid, agentPrompt(agent))
      return
    }
    this.out(sid, [
      `\x1b[36m${agentLabel(agent)}\x1b[0m is working on: ${line}`,
      '• Read the current project context',
      '• Prepared a focused change plan',
      '\x1b[32m✓ Demo response ready\x1b[0m',
      '',
    ].join('\r\n'))
    this.out(sid, agentPrompt(agent))
  }

  private flushQueuedInput() {
    if (this.replaying || this.commandBusy) return
    while (this.queuedInput.length > 0 && !this.commandBusy) {
      const next = this.queuedInput.shift()!
      this.handleInput(next.sid, next.text)
    }
  }

  close() {
    this.readyState = MockSocket.CLOSED
    this.unsubLocal?.()
    this.unsubLocal = null
    this.queuedInput = []
    this.fire('close', { code: 1000, wasClean: true })
  }
}

type InputControl = 'left' | 'right' | 'up' | 'down' | 'home' | 'end' | 'delete' | 'paste-start' | 'paste-end'

const INPUT_CONTROLS: Array<[string, InputControl]> = [
  ['\x1b[200~', 'paste-start'],
  ['\x1b[201~', 'paste-end'],
  ['\x1b[3~', 'delete'],
  ['\x1b[D', 'left'],
  ['\x1b[C', 'right'],
  ['\x1b[A', 'up'],
  ['\x1b[B', 'down'],
  ['\x1b[H', 'home'],
  ['\x1b[F', 'end'],
  ['\x1bOH', 'home'],
  ['\x1bOF', 'end'],
]

function readInputControl(text: string, index: number): { name: InputControl; length: number } | null {
  if (text[index] !== '\x1b') return null
  for (const [sequence, name] of INPUT_CONTROLS) {
    if (text.startsWith(sequence, index)) return { name, length: sequence.length }
  }
  const unknown = text.slice(index).match(/^\x1b\[[0-9;?]*[A-Za-z~]/)?.[0]
  return unknown ? { name: 'paste-start', length: unknown.length } : { name: 'paste-start', length: 1 }
}

function cursorLeft(count: number): string {
  return count > 0 ? `\x1b[${count}D` : ''
}

function cursorRight(count: number): string {
  return count > 0 ? `\x1b[${count}C` : ''
}

function agentLabel(agent: FakeAgentKind): string {
  if (agent === 'codex') return 'Codex CLI'
  if (agent === 'claude') return 'Claude Code'
  return 'Aider'
}

function agentPrompt(agent: FakeAgentKind): string {
  return `\x1b[36m${agent === 'aider' ? 'aider>' : '›'}\x1b[0m `
}

function agentWelcome(agent: FakeAgentKind): string {
  return [
    `\x1b[1;36m${agentLabel(agent)}\x1b[0m · interactive demo`,
    'Working directory: ~/srv/atterm',
    'Enter an instruction, or type /exit to return to the shell.',
    '',
  ].join('\r\n')
}

function safeImageFilename(value: unknown): string {
  if (typeof value !== 'string') return ''
  const name = value.split(/[\\/]/).pop()?.replace(/[\x00-\x1f\x7f]/g, '').trim() ?? ''
  return name.slice(0, 128)
}

function safeImageContentType(value: unknown): string {
  if (typeof value !== 'string') return ''
  const contentType = value.trim().toLowerCase()
  return /^image\/[a-z0-9.+-]+$/.test(contentType) ? contentType : ''
}

function decodedBase64Size(value: string): number | null {
  const data = value.replace(/\s/g, '')
  if (!data || data.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(data)) return null
  const padding = data.endsWith('==') ? 2 : data.endsWith('=') ? 1 : 0
  return Math.max(0, (data.length / 4) * 3 - padding)
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
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
  const originalNotify = MockSocket.onNotify
  g.WebSocket = MockSocket
  return () => {
    g.WebSocket = original
    MockSocket.onNotify = originalNotify
  }
}
