import type { RemoteSession } from '../platform/types'

export interface HostGroup {
  host: string
  user: string
  sessions: RemoteSession[]
}

export function groupByHost(sessions: RemoteSession[]): HostGroup[] {
  const order: string[] = []
  const map = new Map<string, HostGroup>()
  for (const s of sessions) {
    let g = map.get(s.host)
    if (!g) {
      g = { host: s.host, user: s.user, sessions: [] }
      map.set(s.host, g)
      order.push(s.host)
    }
    g.sessions.push(s)
  }
  return order.map((h) => map.get(h)!)
}
