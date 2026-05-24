import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import type { RemoteSession } from '../../platform/types'

const sessions: RemoteSession[] = [
  { session_id: 'a', host_id: 'h1', host: 'box1', user: 'me', title: 'claude', cwd: '/Users/me/proj', cols: 80, rows: 24 },
  { session_id: 'b', host_id: 'h1', host: 'box1', user: 'me', title: 'zsh', cols: 100, rows: 30 },
  { session_id: 'c', host_id: 'h2', host: 'box2', user: 'me', title: 'codex', cols: 120, rows: 40 },
]
let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue(sessions)
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSessionList', () => {
  it('lists sessions grouped by host on mount', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const headers = w.findAll('[data-testid="host-group"]').map((h) => h.text())
    expect(headers.some((t) => t.includes('box1'))).toBe(true)
    expect(headers.some((t) => t.includes('box2'))).toBe(true)
    expect(w.findAll('[data-testid="session-row"]').length).toBe(3)
  })

  it('shows the working directory for sessions that report one', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="session-cwd-a"]').text()).toBe('/Users/me/proj')
    // session 'b' has no cwd → no cwd line rendered
    expect(w.find('[data-testid="session-cwd-b"]').exists()).toBe(false)
  })

  it('marks open sessions with an open badge', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: ['a'] } })
    await flushPromises()
    expect(w.find('[data-testid="open-badge-a"]').exists()).toBe(true)
    expect(w.find('[data-testid="open-badge-b"]').exists()).toBe(false)
  })

  it('emits open(info) when a row is tapped', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="session-row"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
    expect((w.emitted('open')![0]![0] as RemoteSession).session_id).toBe('a')
  })

  it('refresh button re-fetches', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="refresh"]').trigger('click')
    await flushPromises()
    expect(platform.sessions.listRemoteSessions).toHaveBeenCalledTimes(2)
  })

  it('shows empty state when no sessions', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.text()).toMatch(/no remote sessions/i)
  })

  it('emits token-invalid when listRemoteSessions throws relay_unauthorized', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })
})
