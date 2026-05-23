import { describe, it, expect } from 'vitest'
import { groupByHost } from '../sessionGroups'
import type { RemoteSession } from '../../platform/types'

const mk = (over: Partial<RemoteSession>): RemoteSession => ({
  session_id: 's', host_id: 'h', host: 'box', user: 'me', title: 't', cols: 80, rows: 24, ...over,
})

describe('groupByHost', () => {
  it('groups sessions by host preserving first-seen host order', () => {
    const out = groupByHost([
      mk({ session_id: 'a', host: 'box1', user: 'me' }),
      mk({ session_id: 'b', host: 'box2', user: 'me' }),
      mk({ session_id: 'c', host: 'box1', user: 'me' }),
    ])
    expect(out.map((g) => g.host)).toEqual(['box1', 'box2'])
    expect(out[0]!.sessions.map((s) => s.session_id)).toEqual(['a', 'c'])
    expect(out[1]!.sessions.map((s) => s.session_id)).toEqual(['b'])
  })
  it('carries the user of the first session in each host group', () => {
    const out = groupByHost([mk({ host: 'box', user: 'alice' })])
    expect(out[0]!.user).toBe('alice')
  })
  it('returns [] for empty input', () => {
    expect(groupByHost([])).toEqual([])
  })
})
