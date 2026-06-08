import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import { __resetForTests as resetGroupBy } from '../../composables/useTaskGroupBy'
import type { RemoteSession } from '../../platform/types'

const sessions: RemoteSession[] = [
  // attention-needing
  { session_id: 'a', host_id: 'h1', host: 'box1', user: 'me', title: 'codex', cwd: '/Users/me/proj', cols: 80, rows: 24, task_state: 'waiting_input', current_command: 'codex --plan', unread: true },
  // running
  { session_id: 'b', host_id: 'h1', host: 'box1', user: 'me', title: 'zsh',   cwd: '/Users/me',      cols: 100, rows: 30, task_state: 'running',       current_command: 'npm test' },
  // failed + unread
  { session_id: 'c', host_id: 'h2', host: 'box2', user: 'me', title: 'go',    cwd: '/srv/api',       cols: 120, rows: 40, task_state: 'failed',        current_command: 'go test ./...', unread: true },
  // completed + seen → fold
  { session_id: 'd', host_id: 'h2', host: 'box2', user: 'me', title: 'ls',    cwd: '/srv/api',       cols: 120, rows: 40, task_state: 'completed',     current_command: 'ls',           unread: false },
]
let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  resetGroupBy()
  platform = createFakePlatform()
  ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue(sessions)
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSessionList', () => {
  it('groups sessions by state by default (excluding completed+seen) and orders by STATE_ORDER', async () => {
    // Default groupBy is 'host'; flip via button to land on 'state' for this test.
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="group-toggle"]').trigger('click')
    await flushPromises()

    // Headers present, in waiting_input → failed → running order; completed+seen
    // belongs to the fold and must NOT appear as a state group.
    const headers = w.findAll('[data-testid^="state-group-"]').map((el) => el.attributes('data-testid'))
    expect(headers).toEqual([
      'state-group-waiting_input',
      'state-group-failed',
      'state-group-running',
    ])
  })

  it('groups by host when groupBy = host', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const headers = w.findAll('[data-testid^="host-group-"]').map((el) => el.attributes('data-testid'))
    expect(headers).toEqual(['host-group-h1', 'host-group-h2'])
  })

  it('renders a card per non-folded session with short command + cwd', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const cards = w.findAll('[data-testid="task-card"]')
    expect(cards).toHaveLength(3)              // d is folded
    expect(cards[0]!.text()).toContain('codex')
  })

  it('emits open(session) when a card body is tapped', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="card-body"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
  })

  it('row ✓ posts markSessionsSeen with just that session id', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="row-mark-read"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ ids: ['a'] })
  })

  it('group ✓ posts markSessionsSeen with unread ids of that group', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    // Group h1 has session a (unread). Click its group-level mark-all.
    await w.find('[data-testid="mark-all-host-h1"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ ids: ['a'] })
  })

  it('footer ✓ posts markSessionsSeen with { all: true } when totalUnread > 0', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="footer-mark-all"]').exists()).toBe(true)
    await w.find('[data-testid="footer-mark-all"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ all: true })
  })

  it('completed+seen sessions are hidden in the default fold and revealed when toggled', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="completed-fold-row-d"]').exists()).toBe(false)
    await w.find('[data-testid="completed-fold-toggle"]').trigger('click')
    expect(w.find('[data-testid="completed-fold-row-d"]').exists()).toBe(true)
  })

  it('emits tokenInvalid when listRemoteSessions throws relay_unauthorized', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })

  it('emits tokenInvalid when markSessionsSeen throws relay_unauthorized', async () => {
    ;(platform.sessions.markSessionsSeen as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="row-mark-read"]').trigger('click')
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })
})
