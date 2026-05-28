// sessionRoute.ts — pure helpers for the terminal-home hash router.
//
// We use #/s/<uuid> so navigating to the terminal doesn't touch the
// browser path (which the server gates by cookie). The session id is
// always a canonical lowercase UUID; uppercase forms are normalised.

const UUID_RE = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/
const HASH_RE = /^#\/s\/([0-9a-fA-F-]+)(?:\?(.+))?$/

export interface SessionRouteAction {
  notification?: string
  focus?: string
  permission?: string
}

export function parseSessionRoute(hash: string): string | null {
  const m = HASH_RE.exec(hash)
  if (!m) return null
  const uuid = m[1]!
  if (!UUID_RE.test(uuid)) return null
  return uuid.toLowerCase()
}

export function parseSessionRouteAction(hash: string): SessionRouteAction {
  const m = HASH_RE.exec(hash)
  if (!m || !m[2]) return {}
  const params = new URLSearchParams(m[2])
  const action: SessionRouteAction = {}
  const notification = params.get('notification')
  const focus = params.get('focus')
  const permission = params.get('permission')
  if (notification) action.notification = notification
  if (focus) action.focus = focus
  if (permission) action.permission = permission
  return action
}

export function formatSessionRoute(uuid: string, action: SessionRouteAction = {}): string {
  const params = new URLSearchParams()
  if (action.notification) params.set('notification', action.notification)
  if (action.focus) params.set('focus', action.focus)
  if (action.permission) params.set('permission', action.permission)
  const suffix = params.toString()
  return `#/s/${uuid}${suffix ? `?${suffix}` : ''}`
}
