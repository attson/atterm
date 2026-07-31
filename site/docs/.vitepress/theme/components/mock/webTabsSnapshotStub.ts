// 替换 desktop/frontend/src/lib/webTabsSnapshot(config.mjs alias)。
//
// 根因:demo 以桌面模式(caps.wailsBindings=true)运行,但用户浏览器可能残留
// 早期 web 模式(wailsBindings=false)存下的 web tabs 快照。App.vue 的
// restoreFromWebSnapshot 会无条件把快照里的 pane 恢复成 remote:true —— 若快照
// 里是本机会话的 sid,就会出现「tab 标题(空)+ 右上角远端 + 终端却是本机内容」。
//
// 与其依赖「挂载前清 localStorage」(受清除时机 / 浏览器缓存影响),这里直接让
// loadSnapshot 恒返回 null、saveSnapshot no-op,从源头关闭 restore 路径,无论
// localStorage 里有什么残留都不会触发。parseHashSid / formatHash 是纯函数,
// 原样保留(它们负责 #/t 与 #/session hash,与快照无关)。

export interface PaneSnap { slot: number; session_id: string; host_id?: string; sealed?: string }
export interface TabSnap {
  id: string; layout: string; active_pane_idx: number
  col_ratio?: number; row_ratio?: number
  panes: PaneSnap[]
}
export interface WebTabsSnapshot { tabs: TabSnap[]; active_tab_id: string }

export function getWindowId(): string {
  return 'demo'
}

// demo 恒不恢复快照,也不保存。
export function loadSnapshot(): WebTabsSnapshot | null {
  return null
}
export function saveSnapshot(_snap: WebTabsSnapshot): void {
  /* no-op */
}

// 以下两个与真实实现一致(纯函数,处理 #/session/<sid> deep link 的 hash)。
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
