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

// 预置一个本机会话,让 local host 组默认非空(体现本地+远程双源)。
export const localSessions: LocalSessionInfo[] = [
  {
    id: '66666666-6666-4666-8666-666666666666',
    command: '/bin/zsh',
    cwd: '~',
    title: 'zsh',
    cols: 100,
    rows: 30,
    started_at: 1_753_900_000,
    host_id: LOCAL_HOST,
    host: LOCAL_HOST,
    user: 'you',
    task_state: 'idle',
    remote_permission: 'full',
  },
]

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
