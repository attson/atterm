import { describe, expect, it } from 'vitest'
import type { SessionInfo } from './connection'
import {
  collapseSidebarCandidates,
  mergeVisibleRemoteSessions,
  PEER_ONLY_ROUTE_POLICY,
  selectRouteCandidates,
  type RouteCandidate,
  type RoutePrincipal,
  type TerminalRoute,
} from './routeCandidates'

function candidate(
  sessionId: string,
  principal: RoutePrincipal,
  route: TerminalRoute,
  overrides: Partial<RouteCandidate<string>> = {},
): RouteCandidate<string> {
  return {
    sessionId,
    principal,
    route,
    enabled: true,
    health: 'available',
    value: `${principal}:${route}:${sessionId}`,
    ...overrides,
  }
}

function session(id: string, title: string): SessionInfo {
  return {
    id,
    command: 'zsh',
    cwd: '/work',
    title,
    cols: 80,
    rows: 24,
    started_at: 1,
    host_id: 'shared-host',
  }
}

describe('route candidate selection', () => {
  it('uses direct, then enabled Quick Tunnel, then Relay priority', () => {
    const routes = [
      candidate('s1', 'relay', 'relay'),
      candidate('s1', 'relay', 'direct'),
      candidate('s2', 'peer', 'quick_tunnel'),
      candidate('s2', 'peer', 'direct', { enabled: false }),
      candidate('s3', 'peer', 'direct', { health: 'unavailable' }),
      candidate('s3', 'peer', 'quick_tunnel'),
    ]

    expect(selectRouteCandidates(routes).map((item) => [item.sessionId, item.route])).toEqual([
      ['s1', 'direct'],
      ['s2', 'quick_tunnel'],
      ['s3', 'quick_tunnel'],
    ])
  })

  it('keeps Relay and Peer trust principals in separate candidate groups', () => {
    const selected = selectRouteCandidates([
      candidate('same-session', 'peer', 'direct'),
      candidate('same-session', 'relay', 'relay'),
    ])

    expect(selected).toHaveLength(2)
    expect(selected.map((item) => item.principal).sort()).toEqual(['peer', 'relay'])
  })

  it('rejects routes that would cross their authenticated principal', () => {
    expect(selectRouteCandidates([
      candidate('s1', 'peer', 'relay'),
      candidate('s2', 'relay', 'quick_tunnel'),
    ])).toEqual([])
  })

  it('keys candidates by session id rather than host id', () => {
    const selected = selectRouteCandidates([
      candidate('session-a', 'peer', 'direct'),
      candidate('session-b', 'peer', 'direct'),
    ])

    expect(selected.map((item) => item.sessionId)).toEqual(['session-a', 'session-b'])
  })

  it('peer-only policy cannot select a Relay-account route', () => {
    const selected = selectRouteCandidates([
      candidate('relay-session', 'relay', 'direct'),
      candidate('relay-session', 'relay', 'relay'),
      candidate('peer-session', 'peer', 'quick_tunnel'),
    ], PEER_ONLY_ROUTE_POLICY)

    expect(selected).toHaveLength(1)
    expect(selected[0]).toMatchObject({
      sessionId: 'peer-session',
      principal: 'peer',
      route: 'quick_tunnel',
    })
  })

  it('collapses candidate health changes to one sidebar item without mixing principals', () => {
    const selected = selectRouteCandidates([
      candidate('same-session', 'peer', 'quick_tunnel'),
      candidate('same-session', 'peer', 'direct', { health: 'unavailable' }),
      candidate('same-session', 'relay', 'relay'),
    ])
    const visible = collapseSidebarCandidates(selected)

    expect(visible).toHaveLength(1)
    expect(visible[0]).toMatchObject({ principal: 'relay', route: 'relay' })
  })
})

describe('visible remote session merge', () => {
  it('shows one Relay-backed entry when both principals report the same session id', () => {
    const merged = mergeVisibleRemoteSessions(
      [session('same-session', 'relay metadata')],
      [session('same-session', 'peer metadata')],
    )

    expect(merged).toHaveLength(1)
    expect(merged[0]).toMatchObject({
      id: 'same-session',
      title: 'relay metadata',
      peer_direct: undefined,
    })
  })

  it('marks only Peer-only entries for the accountless connection path', () => {
    const merged = mergeVisibleRemoteSessions(
      [session('relay-session', 'relay')],
      [session('peer-session', 'peer')],
    )

    expect(merged).toHaveLength(2)
    expect(merged.find((item) => item.id === 'relay-session')?.peer_direct).toBeUndefined()
    expect(merged.find((item) => item.id === 'peer-session')?.peer_direct).toBe(true)
  })
})
