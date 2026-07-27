const WINDOW_ID_KEY = 'atterm.web.window_id'
const SNAPSHOT_KEY_PREFIX = 'atterm.web.tabs.v1.'

function uuid(): string {
  if (crypto?.randomUUID) return crypto.randomUUID()
  const b = new Uint8Array(16); crypto.getRandomValues(b)
  b[6] = (b[6] & 0x0f) | 0x40; b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`
}

export function getWindowId(): string {
  let id = sessionStorage.getItem(WINDOW_ID_KEY)
  if (!id) { id = uuid(); sessionStorage.setItem(WINDOW_ID_KEY, id) }
  return id
}

export interface PaneSnap { slot: number; session_id: string; host_id?: string; sealed?: string }
export interface TabSnap {
  id: string; layout: string; active_pane_idx: number
  col_ratio?: number; row_ratio?: number
  panes: PaneSnap[]
}
export interface WebTabsSnapshot { tabs: TabSnap[]; active_tab_id: string }

export function loadSnapshot(): WebTabsSnapshot | null {
  const key = SNAPSHOT_KEY_PREFIX + getWindowId()
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    return JSON.parse(raw) as WebTabsSnapshot
  } catch { return null }
}

export function saveSnapshot(snap: WebTabsSnapshot): void {
  const key = SNAPSHOT_KEY_PREFIX + getWindowId()
  localStorage.setItem(key, JSON.stringify(snap))
}

export function parseHashSid(hash: string): {
  sid: string | null; focus: 'input' | undefined; permission: 'view' | undefined
} {
  const m = /^#\/session\/([^?]+)(?:\?(.*))?$/.exec(hash)
  if (!m) return { sid: null, focus: undefined, permission: undefined }
  const sid = decodeURIComponent(m[1])
  const params = new URLSearchParams(m[2] ?? '')
  const focus = params.get('focus') === 'input' ? 'input' : undefined
  const permission = params.get('permission') === 'view' ? 'view' : undefined
  return { sid, focus, permission }
}

export function formatHash(sid: string, opts?: { focus?: 'input'; permission?: 'view' }): string {
  const qs = new URLSearchParams()
  if (opts?.focus) qs.set('focus', opts.focus)
  if (opts?.permission) qs.set('permission', opts.permission)
  const q = qs.toString()
  return `#/session/${encodeURIComponent(sid)}${q ? '?' + q : ''}`
}
