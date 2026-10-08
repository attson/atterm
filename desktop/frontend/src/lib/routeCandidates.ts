import type { SessionInfo } from './connection'

export type RoutePrincipal = 'relay' | 'peer'
export type TerminalRoute = 'direct' | 'quick_tunnel' | 'relay'
export type RouteHealth = 'available' | 'unavailable'
export type RoutePolicyMode = 'hybrid' | 'peer-only' | 'relay-only'

export interface RoutePolicy {
  mode: RoutePolicyMode
  direct: boolean
  quickTunnel: boolean
  relay: boolean
}

export interface RouteCandidate<T = unknown> {
  sessionId: string
  principal: RoutePrincipal
  route: TerminalRoute
  enabled: boolean
  health: RouteHealth
  value: T
}

export const HYBRID_ROUTE_POLICY: Readonly<RoutePolicy> = Object.freeze({
  mode: 'hybrid',
  direct: true,
  quickTunnel: true,
  relay: true,
})

export const PEER_ONLY_ROUTE_POLICY: Readonly<RoutePolicy> = Object.freeze({
  mode: 'peer-only',
  direct: true,
  quickTunnel: true,
  relay: false,
})

const ROUTE_PRIORITY: Readonly<Record<TerminalRoute, number>> = Object.freeze({
  direct: 0,
  quick_tunnel: 1,
  relay: 2,
})

function policyAllowsPrincipal(policy: Readonly<RoutePolicy>, principal: RoutePrincipal): boolean {
  if (policy.mode === 'peer-only') return principal === 'peer'
  if (policy.mode === 'relay-only') return principal === 'relay'
  return true
}

function policyAllowsRoute(policy: Readonly<RoutePolicy>, route: TerminalRoute): boolean {
  if (route === 'direct') return policy.direct
  if (route === 'quick_tunnel') return policy.quickTunnel
  return policy.relay
}

function principalAllowsRoute(principal: RoutePrincipal, route: TerminalRoute): boolean {
  // Quick Tunnel authenticates Peer membership; Relay WS authenticates the
  // Relay account. Direct is the only byte path shared by both trust models.
  if (route === 'quick_tunnel') return principal === 'peer'
  if (route === 'relay') return principal === 'relay'
  return true
}

function candidateGroupKey(candidate: Pick<RouteCandidate, 'principal' | 'sessionId'>): string {
  return `${candidate.principal}:${candidate.sessionId}`
}

/** Selects one byte route per (trust principal, session_id). It deliberately
 * never compares a Peer candidate with a Relay candidate, even when both
 * describe the same terminal session. */
export function selectRouteCandidates<T>(
  candidates: readonly RouteCandidate<T>[],
  policy: Readonly<RoutePolicy> = HYBRID_ROUTE_POLICY,
): RouteCandidate<T>[] {
  const selected = new Map<string, RouteCandidate<T>>()

  for (const candidate of candidates) {
    if (
      candidate.sessionId === '' ||
      !candidate.enabled ||
      candidate.health !== 'available' ||
      !policyAllowsPrincipal(policy, candidate.principal) ||
      !policyAllowsRoute(policy, candidate.route) ||
      !principalAllowsRoute(candidate.principal, candidate.route)
    ) {
      continue
    }

    const key = candidateGroupKey(candidate)
    const current = selected.get(key)
    if (!current || ROUTE_PRIORITY[candidate.route] < ROUTE_PRIORITY[current.route]) {
      selected.set(key, candidate)
    }
  }

  return [...selected.values()]
}

/** The current pane model is keyed by authoritative session_id. When the
 * same id is visible through both independent trust models, retain the
 * established Relay-account entry instead of silently attaching it with a
 * Peer credential. Candidate groups remain separate before this UI collapse. */
export function collapseSidebarCandidates<T>(
  candidates: readonly RouteCandidate<T>[],
): RouteCandidate<T>[] {
  const visible = new Map<string, RouteCandidate<T>>()
  for (const candidate of candidates) {
    const current = visible.get(candidate.sessionId)
    if (!current || (current.principal === 'peer' && candidate.principal === 'relay')) {
      visible.set(candidate.sessionId, candidate)
    }
  }
  return [...visible.values()]
}

/** Adapts the two currently available discovery streams to the unified
 * candidate model. Quick Tunnel candidates will enter here after its WSS
 * channel exposes the same frontend transport contract. */
export function mergeVisibleRemoteSessions(
  relaySessions: readonly SessionInfo[],
  peerSessions: readonly SessionInfo[],
): SessionInfo[] {
  const candidates: RouteCandidate<SessionInfo>[] = []
  for (const session of peerSessions) {
    candidates.push({
      sessionId: session.id,
      principal: 'peer',
      route: 'direct',
      enabled: true,
      health: 'available',
      value: session,
    })
  }
  for (const session of relaySessions) {
    candidates.push({
      sessionId: session.id,
      principal: 'relay',
      route: 'relay',
      enabled: true,
      health: 'available',
      value: session,
    })
  }

  return collapseSidebarCandidates(selectRouteCandidates(candidates)).map((candidate) => ({
    ...candidate.value,
    peer_direct: candidate.principal === 'peer' ? true : undefined,
  }))
}
