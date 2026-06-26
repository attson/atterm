// Shared helpers for rendering a session row: short command label, full
// command for tooltips, and host display name. Used by both the desktop
// TaskGroupedList and the mobile session list so the two surfaces stay
// in lockstep.

import type { TaskState } from './taskState'

// taskStateLabel returns the localized chip text for a TaskState. The
// switch keeps the i18n key set explicit so vue-tsc's MessageKey union
// check stays exhaustive — a dynamic `t(\`mobile.taskStates.${s}\`)`
// would lose that.
export function taskStateLabel(
  state: TaskState | string | undefined,
  t: (key: 'mobile.taskStates.idle' | 'mobile.taskStates.running' | 'mobile.taskStates.waiting_input' | 'mobile.taskStates.completed' | 'mobile.taskStates.failed' | 'mobile.taskStates.disconnected' | 'mobile.taskStates.closed') => string,
): string {
  switch (state) {
    case 'running': return t('mobile.taskStates.running')
    case 'waiting_input': return t('mobile.taskStates.waiting_input')
    case 'completed': return t('mobile.taskStates.completed')
    case 'failed': return t('mobile.taskStates.failed')
    case 'disconnected': return t('mobile.taskStates.disconnected')
    case 'closed': return t('mobile.taskStates.closed')
    default: return t('mobile.taskStates.idle')
  }
}

export interface SessionLike {
  current_command?: string
  title?: string
  session_id: string
  cwd?: string
  // Optional workload classification — "ai" | "shell" | "test" | "build"
  // | "deploy". When absent, treat as shell. Drives aiTitleOrCommand().
  type?: string
}

export function fullCommand(s: Pick<SessionLike, 'current_command' | 'title' | 'session_id'>): string {
  return s.current_command || s.title || s.session_id.slice(0, 8)
}

// commandLabel is the SHORT row display: only the executable name
// (first whitespace-separated token, with any leading path stripped).
// `/usr/local/bin/claude --permission-mode bypassPermissions` → `claude`.
export function commandLabel(s: Pick<SessionLike, 'current_command' | 'title' | 'session_id'>): string {
  const raw = fullCommand(s)
  const firstToken = raw.split(/\s+/)[0] || raw
  return firstToken.split('/').pop() || firstToken
}

export function rowTitle(s: SessionLike): string {
  const cmd = fullCommand(s)
  return s.cwd ? `${cmd}\n${s.cwd}` : cmd
}

// aiTitleOrCommand returns the AI-set window title (from OSC 0/1/2, surfaced
// via SessionInfo.Title) when the session is classified as an AI workload
// and a title is available. Otherwise returns the existing short command
// label so shell sessions keep their current display.
export function aiTitleOrCommand(s: SessionLike): string {
  if (s.type === 'ai' && s.title) return s.title
  return commandLabel(s)
}

// hostName resolves a host display name for a group key. The list is the
// sessions inside that group — the caller picks them out of byHost. Falls
// back to hostId, then to the provided unknown-host label.
export function hostName(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHost: string,
): string {
  const first = list?.[0]
  return first?.host || hostId || unknownHost
}

// coResidentIndex assigns 1-based numbers to host_ids that physically live on
// the same machine as the caller (`localHost` = this machine's OS hostname).
// Returns an empty map when localHost is empty or when fewer than 2 local
// host_ids exist — callers render the bare host name in those cases.
// Order is lexicographic by host_id so every window sees the same numbering.
export function coResidentIndex(
  byHost: Record<string, { host?: string }[]>,
  localHost: string,
): Map<string, number> {
  if (!localHost) return new Map()
  const localHostIds: string[] = []
  for (const hid of Object.keys(byHost)) {
    const list = byHost[hid]
    if (list && list.length > 0 && (list[0]?.host ?? '') === localHost) {
      localHostIds.push(hid)
    }
  }
  if (localHostIds.length < 2) return new Map()
  localHostIds.sort()
  const out = new Map<string, number>()
  localHostIds.forEach((hid, i) => out.set(hid, i + 1))
  return out
}

// hostNameWithIndex wraps hostName() and appends a "#N" co-residence suffix
// when an index is provided. Index 0 / undefined → no suffix (the helper
// stays no-op for the single-instance default).
export function hostNameWithIndex(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHost: string,
  index: number | undefined,
): string {
  const base = hostName(hostId, list, unknownHost)
  return index && index >= 1 ? `${base} #${index}` : base
}
