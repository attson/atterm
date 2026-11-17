// 注入到 window.go.main.App 的假 Wails 绑定。让 App.vue 以「桌面模式」
// (caps.wailsBindings=true, caps.localPty=true)启动,从而支持网页新建本机
// 会话 —— 新建按钮由 caps.localPty 门控,startNewTab → listShells/newSession
// 走这里。
//
// 方法清单来自对 App.vue boot 链的调研(见 mockPlatform 注释)。本机会话列表
// 和终端字节流都走 WebSocket(GetEndpoint 返回的 url 的 /client-sessions 和
// /client),已被 mockSocket 的全局 WebSocket 替身拦截。
//
// localSessions 是共享的本机会话表:NewSession 往里加,mockSocket 的 local
// 列表连接读它并推 LIST_RESP。

import { localSessions, notifyLocalSessionsChanged, LOCAL_HOST } from './localSessions'
import { clearMockInputHistory } from './mockInputHistory'
import { mockEvents } from './mockEvents'

// GetEndpoint 返回的本机 ws 基址;mockSocket 用 host 里的 'local.demo' 区分
// 本机列表(推 localSessions)与远程列表(推 fakeSessions)。
export const LOCAL_ENDPOINT_URL = 'ws://local.demo'

const DEMO_SSH_HOSTS = [
  {
    id: 'demo-staging',
    alias: 'staging',
    host: 'staging.example.com',
    port: '22',
    user: 'deploy',
    auth_kind: 'key' as const,
    tags: ['staging', 'web'],
    group_path: ['Demo infrastructure'],
  },
  {
    id: 'demo-database',
    alias: 'analytics-db',
    host: '10.0.4.21',
    port: '22',
    user: 'ops',
    auth_kind: 'password' as const,
    tags: ['database'],
    group_path: ['Demo infrastructure'],
  },
]

type ClipboardReader = {
  read?: () => Promise<Array<{ types: readonly string[]; getType: (type: string) => Promise<Blob> }>>
  readText?: () => Promise<string>
}

// Browsers cannot read bytes from a file-manager path copied as text. Keep a
// real-sized PNG in memory so the shared terminal hover preview remains useful
// without exposing or pretending to load the user's local file.
const DEMO_CLIPBOARD_IMAGE_BASE64 = 'iVBORw0KGgoAAAANSUhEUgAAAPAAAACgCAMAAAAFBRFXAAADAFBMVEUSGCcYJDoaNGIdKD0jOUcjRogkX7YuT1MxSVQxW1g2Oz43QVE4U1A8ZWFBXGlBa2ZCbGdIe8dlm+RsWkaMZ1ONqv6jgF6u2P28n4XMqFnT4erXy7Xa5P/iumX1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFH1xFEA/wAxcq+DAAABAHRSTlP///////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////8AU/cHJQAABetJREFUeJzt2YmWojoQBmAwaosLDdiIuL3/Y3YCBNkClY0l5J9zpqenjZfvVpltnNPK4pzcVcWCTY8Fmx4LNj0WbHos2PRYsOmxYNNjwabHgk2PBZseCzY9FrycoPMtTd/vNL2dEXzUcsHX96fM+woetlTwNf3UkkLJCwVfP60AxcsEd3ih4kWCj13ez+cIGbtIcNoNTiFjlwjubGhoUy8QjN4s8BuwHi8QzCwwqMQLBN/Y4Nvw6AWCGVMWcNpaIJj5ETYVzPZ+3sOjLXj+WV1Lr27SWt2ydGaDz8OjFYK3W1XvNBBmT0NOD+rAJ88bSTyPw8PB80YTz+F4mHk9j+P6UCIzuAAovB7PhalEJr/i+fG8ycUjXuKhr9fzfqTfDpRj85oW1M+uEnDN63kH2fcDZrKL+IZ3PPFE/9TS8o62OIlFFozOLe+8xZLgTu+sxXJg1MkdbQMiEikw0zvWciwQGfCW6Z2xWALc51W8AVE4J4iD+70qxegcXJSRhcFDXnUbkO0lCIJfVe8mCh72ep6ag+fpN8iiaOYXBEO8SpZj3M40atpaDAzzKhCjS1CJiqlfCHwCeqXF29+gFgVFFgEfhqFlZGqCfoJWfmSLLADm8cpsQOrtrKrI/GAur4S42c40kgsUN7h9/B2I4Aako51ppD4nnOCO474WMfLYXlxkiRWeDyziFdlysdpZQZG5wGJe/sWpp51phOcuHrCol1Pc385lBBcoDjDjOke1eNu5GikrMhws4+UQA9qZ5ldkOgSD2dc5sMAaENjOEkWGgmW9sA0IuJ3LInOvAEAw9HjUE8Akw9HOZXgXKBhYgXd4A8LZzjScbQ0CK/EObUC427kM13YdAlbk7b/zEWlnkSIDwMq8PYuTYDuXge9ChsEKvUyxeDtzF3kQrNTLEMu0Mw10gRoCw6+vgGn3nmw708AWqAEw5/UGIK0pVb6daUDH5H6wem9LfBg6+vIEUOResA5vfQOiqp1phueuPrDw8RcsVtfOZYYWKDZY/Lg/GDqhKm1nmoEiM8EavcWWS3U70/Qfk1lgrd5sOdbQzjR9RWaANXuxWEs70/TsQrrBctc5kOgrbx7mAtUJ1u/VWd48rLbuAktf5wzlot8bsI7JHWDFx4UO7xjcgFHkNli7d5Ty5unYhbTAur3jtDNNu8hNsHbvmNygY4FqgHV7Ry1vnsYCVQdr9o7bzjT1Y3INrNs7BZekWuQqWLN3kvLmqcxdFbDy66tapmnnMuUC9QXrud4ovZNyg2+RS7Be77TlzVIckylY63Fw4namyYpcgPV6p5bSkF1IBtZ73J9HefN4iIC1emdT3jwXDF6TNwgwWOf1xpzaOQ8G6+POZHauRSd4du1MohE8w/IGGsFzbGcSXeBZtjOJJvBMyxtoAs+1nUkw2FlVLNj0WLDpsWDTY8Gmx4JNz9rBKIoridBUj6UvDXBU++4UjfswY6QBjus/bXxrQoDg/ePPiV5P5OySxxV/H76eR/zFDZPX447/tH+RpPlHwI1edxe/hOThJ/mP3OaYXYJftEtu5VjyNXuz+YCxNSS/OSh+UfdfmGDH/nH3fb94q11CaBt/nzx9Hx386HHzj40xKfqCi7Hka5T/T5kCjNrgR3Jzo6zC+GGTu+NsiNXZnfOfO98Xx9m3mwTXlRAzaH3M1a2A/8r/Bn5t5Y10pBO8dRw/bIOfcbpLYvL0+LHiZ9GV34ct4sZPP/tBHdwcs8t7vQHe47/Qmi7wKfbdON46LXBIfuGnR/jJSSnIw2+S7KGLh88ldxSRMtbA1THFy3DDh5Wxk4ERWYHjOGyDd0ly3+Onz4tDsM+jewjLz2H+8ctmK/LwNXB1DH6/W/MzfJyupeneY9sCn+LXjYBDPAv55MHJBBTED/rQZ/JS3NF/fkisNXBtTPh6N8HnYtK6a97stMEh3WeFLTDuySsGb2K8PmW1IEsMfn66LGUzbAZxSU9XwY0x7WXpeZzVsmRO1r61XN3hwfxYsOmxYNNjwabHgk2PBZueNYJXln+H4NW6Bp5DVQAAAABJRU5ErkJggg=='
const IMAGE_FILE_EXTENSION = /\.(?:png|jpe?g|gif|webp|bmp|heic|heif|tiff?)$/i

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let value = ''
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    value += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
  }
  return btoa(value)
}

function clipboardImageFilename(text: string): string {
  const candidate = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line && !line.startsWith('#'))
  if (!candidate) return ''

  let path = candidate
  if (candidate.startsWith('file:')) {
    try {
      const url = new URL(candidate)
      if (url.protocol !== 'file:') return ''
      path = decodeURIComponent(url.pathname)
    } catch {
      return ''
    }
  } else if (!candidate.startsWith('/') && !/^[a-z]:[\\/]/i.test(candidate)) {
    return ''
  }

  const filename = path.split(/[\\/]/).pop()?.replace(/[\x00-\x1f\x7f]/g, '').trim() ?? ''
  return IMAGE_FILE_EXTENSION.test(filename) ? filename.slice(0, 128) : ''
}

export async function readMockClipboardPaste(clipboard: ClipboardReader | undefined = globalThis.navigator?.clipboard) {
  try {
    if (clipboard?.read) {
      const items = await clipboard.read()
      for (const item of items) {
        const contentType = item.types.find((type) => type.startsWith('image/'))
        if (!contentType) continue
        const image = await item.getType(contentType)
        const subtype = contentType.slice('image/'.length).replace(/[^a-z0-9.+-]/gi, '') || 'png'
        return {
          kind: 'image' as const,
          filename: `clipboard-image.${subtype}`,
          content_type: contentType,
          data_base64: arrayBufferToBase64(await image.arrayBuffer()),
        }
      }
    }

    const text = await clipboard?.readText?.()
    if (text) {
      const filename = clipboardImageFilename(text)
      if (filename) {
        return {
          kind: 'image' as const,
          filename,
          content_type: 'image/png',
          data_base64: DEMO_CLIPBOARD_IMAGE_BASE64,
        }
      }
      return { kind: 'text' as const, text }
    }
    return { kind: 'none' as const, reason: 'clipboard has no text or image' }
  } catch {
    return { kind: 'none' as const, reason: 'clipboard access failed' }
  }
}

function uuid(): string {
  // 避免 Math.random 限制:用 crypto。demo 运行在浏览器,crypto.randomUUID 可用。
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
  // 极端兜底(不应触发)
  return '00000000-0000-4000-8000-' + Date.now().toString(16).padStart(12, '0')
}

export function createMockGoApp() {
  let terminalTheme = 'vscode-dark'
  let terminalFontHead = ''
  let terminalFontSize = 14
  let terminalLineHeight = 1.2
  let terminalCursorStyle = 'block'
  let terminalCursorBlink = true
  let terminalScrollback = 10_000

  return {
    // ---- boot chain 致命方法 ----
    GetTerminalTheme: async () => terminalTheme,
    SetTerminalTheme: async (value: string) => { terminalTheme = value },
    GetTerminalFontHead: async () => terminalFontHead,
    SetTerminalFontHead: async (value: string) => { terminalFontHead = value },
    GetTerminalFontSize: async () => terminalFontSize,
    SetTerminalFontSize: async (value: number) => { terminalFontSize = value },
    GetTerminalLineHeight: async () => terminalLineHeight,
    SetTerminalLineHeight: async (value: number) => { terminalLineHeight = value },
    GetTerminalCursorStyle: async () => terminalCursorStyle,
    SetTerminalCursorStyle: async (value: string) => { terminalCursorStyle = value },
    GetTerminalCursorBlink: async () => terminalCursorBlink,
    SetTerminalCursorBlink: async (value: boolean) => { terminalCursorBlink = value },
    GetTerminalScrollback: async () => terminalScrollback,
    SetTerminalScrollback: async (value: number) => { terminalScrollback = value },
    GetShortcutBindings: async () => ({}),
    GetClipboardPastePayload: () => readMockClipboardPaste(),
    GetEndpoint: async () => ({ url: LOCAL_ENDPOINT_URL, session_token: 'demo-token' }),
    GetHostInfo: async () => ({ host_id: LOCAL_HOST, host: LOCAL_HOST, user: 'you' }),
    LoadRecoverySnapshot: async () => ({
      version: 1,
      host_id: '',
      clean_shutdown: false,
      saved_at_unix: 0,
      tabs: [],
    }),
    GetRecoveryDialogEnabled: async () => false,
    GetRelayConfig: async () => ({
      // connected + remote_proxy_url 让桌面 boot 建立远程会话列表连接:
      // remote_proxy_url 的 host 不含 local.demo → mockSocket 推 fakeSessions
      // (dev-server / macbook-pro 两个远程 host 组)。
      url: 'https://relay.demo',
      token: 'demo-token',
      session_expires_at: 4_102_444_800,
      allow_insecure_relay: false,
      disable_e2ee: false,
      remote_permission: 'full',
      last_email: 'you@example.com',
      connected: true,
      remote_proxy_url: 'ws://remote.demo',
    }),
    GetUplinkHealth: async () => ({
      state: 'connected' as const,
      rtt: { last_ms: 18, p50_ms: 18, p95_ms: 24 },
      rtt_samples: [],
      reconnect: { count_last_hour: 0, last_at_ms: null, last_reason: '', history: [] },
      bytes: { in_per_sec: 320, out_per_sec: 1280 },
      seq_gaps: 0,
    }),
    ListShells: async () => ['/bin/zsh', '/bin/bash'],
    ListSSHHosts: async () => DEMO_SSH_HOSTS.map((host) => ({
      ...host,
      tags: [...host.tags],
      group_path: [...host.group_path],
    })),
    RunSnippetOnHosts: async (_label: string, text: string, hostIDs: string[]) => {
      const runID = uuid()
      for (const [index, hostID] of hostIDs.entries()) {
        const host = DEMO_SSH_HOSTS.find((item) => item.id === hostID)
        const hostLabel = host?.alias || host?.host || hostID
        window.setTimeout(() => {
          mockEvents.emit('snippet:run:progress', {
            run_id: runID,
            result: {
              host_id: hostID,
              host_label: hostLabel,
              state: 'ok',
              exit_code: 0,
              output: `$ ${text}\nDemo command completed successfully.`,
              truncated: false,
            },
          })
        }, 80 + index * 40)
      }
      return runID
    },
    CancelSnippetRun: async () => {},
    NewSession: async (req: { command?: string; cwd?: string; cols?: number; rows?: number }) => {
      const session_id = uuid()
      const shell = (req.command || '/bin/zsh').split('/').pop() || 'zsh'
      localSessions.push({
        id: session_id,
        command: req.command || '/bin/zsh',
        cwd: req.cwd || '~',
        title: shell,
        cols: req.cols || 80,
        rows: req.rows || 24,
        started_at: 1_753_900_000,
        host_id: LOCAL_HOST,
        host: LOCAL_HOST,
        user: 'you',
        task_state: 'idle',
        remote_permission: 'full',
      })
      notifyLocalSessionsChanged()
      return { session_id }
    },

    // ---- 非致命 / 交互方法 ----
    GetStartupError: async () => ({ fatal: false, message: '', log_path: '' }),
    GetAccountKey: async () => '',
    GetTaskSidebarCollapsed: async () => false,
    SetTaskSidebarCollapsed: async () => {},
    GetCommandNotifyThresholdSeconds: async () => 10,
    GetPinnedSessionIds: async () => [],
    SetPinnedSessionIds: async () => {},
    GetUpdateState: async () => ({
      current: 'demo',
      latest: 'demo',
      available: false,
      ready: false,
      checking: false,
      downloading: false,
      error: '',
    }),
    FetchRelayMe: async () => {
      throw new Error('no relay in demo')
    },
    SaveRecoverySnapshot: async () => {},
    CloseSession: async (sessionId: string) => {
      const i = localSessions.findIndex((s) => s.id === sessionId)
      if (i >= 0) {
        localSessions.splice(i, 1)
        clearMockInputHistory(sessionId)
        notifyLocalSessionsChanged()
      }
    },
    MarkSessionsSeen: async () => {},
  }
}

export function installMockGoApp(): () => void {
  const g = globalThis as any
  const previousGo = g.go
  g.go = {
    ...(previousGo || {}),
    main: {
      ...(previousGo?.main || {}),
      App: createMockGoApp(),
    },
  }
  return () => {
    if (previousGo === undefined) delete g.go
    else g.go = previousGo
  }
}
