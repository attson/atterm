// 共享的「本机会话」表(SessionInfo 形状,用 id 字段)。mockGoApp.NewSession
// 往里加、CloseSession 删;mockSocket 的本机列表连接(/client-sessions on
// LOCAL_ENDPOINT_URL)读它并推 LIST_RESP,使新建的会话出现在侧栏 local host 组。

export const LOCAL_HOST = 'this-mac'

export interface LocalSessionInfo {
  id: string
  command: string
  cwd: string
  title: string
  cols: number
  rows: number
  started_at: number
  host_id: string
  host: string
  user: string
  task_state?: string
  remote_permission?: string
}

// 本机会话表初始为空:demo boot 时 App 的 auto-startNewTab(caps.localPty)会
// 通过 NewSession 正确创建首个本机会话(pane.remote=false),从而 tab 标题、
// 文件浏览器、activeSession 都能从 localList 正确解析。若预置一个「未打开」的
// 本机会话在侧栏,点它会走 openRemoteAsTab(硬编码 remote:true)→ activeSession
// 在 localList 找不到 →「(空)」+ 标「远端」+ 文件浏览器无活动面板。
export const localSessions: LocalSessionInfo[] = []

const listeners = new Set<() => void>()

// mockSocket 的本机列表连接订阅它,收到变更就重推 LIST_RESP。
export function onLocalSessionsChanged(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function notifyLocalSessionsChanged(): void {
  for (const fn of Array.from(listeners)) {
    try {
      fn()
    } catch {
      /* ignore */
    }
  }
}
