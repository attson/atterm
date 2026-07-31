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

// GetEndpoint 返回的本机 ws 基址;mockSocket 用 host 里的 'local.demo' 区分
// 本机列表(推 localSessions)与远程列表(推 fakeSessions)。
export const LOCAL_ENDPOINT_URL = 'ws://local.demo'

function uuid(): string {
  // 避免 Math.random 限制:用 crypto。demo 运行在浏览器,crypto.randomUUID 可用。
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
  // 极端兜底(不应触发)
  return '00000000-0000-4000-8000-' + Date.now().toString(16).padStart(12, '0')
}

export function createMockGoApp() {
  return {
    // ---- boot chain 致命方法 ----
    GetTerminalTheme: async () => 'vscode-dark',
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
    ListShells: async () => ['/bin/zsh', '/bin/bash'],
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
        notifyLocalSessionsChanged()
      }
    },
    MarkSessionsSeen: async () => {},
  }
}

export function installMockGoApp(): void {
  const g = globalThis as any
  g.go = g.go || {}
  g.go.main = g.go.main || {}
  g.go.main.App = createMockGoApp()
}
