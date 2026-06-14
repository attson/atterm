import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import { __resetForTests as resetGroupBy } from '../../composables/useTaskGroupBy'
import { __resetForTests as resetCollapsedGroups } from '../../composables/useCollapsedGroups'
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
  resetCollapsedGroups()
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

  it('renders groups expanded by default with aria-expanded=true', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const headers = w.findAll('[data-testid="group-header"]')
    expect(headers.length).toBeGreaterThan(0)
    for (const h of headers) {
      expect(h.attributes('aria-expanded')).toBe('true')
    }
    // h1 has session a (unread), card should render.
    expect(w.find('[data-testid="card-body"]').exists()).toBe(true)
  })

  it('collapses a host group when its header is tapped, hiding only its cards', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()

    // host mode: h1 has [a, b], h2 has [c] (d folded).
    expect(w.findAll('[data-testid="task-card"]')).toHaveLength(3)

    const h1Group = w.find('[data-testid="host-group-h1"]')
    const h1Header = h1Group.find('[data-testid="group-header"]')
    await h1Header.trigger('click')
    await flushPromises()

    // h1 cards hidden; h2 still showing.
    expect(h1Group.findAll('[data-testid="task-card"]')).toHaveLength(0)
    expect(h1Header.attributes('aria-expanded')).toBe('false')
    expect(h1Header.text()).toContain('▶')

    const h2Group = w.find('[data-testid="host-group-h2"]')
    expect(h2Group.findAll('[data-testid="task-card"]')).toHaveLength(1)
    expect(h2Group.find('[data-testid="group-header"]').attributes('aria-expanded')).toBe('true')
  })

  it('re-expands a collapsed group on second click', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const h1Header = w.find('[data-testid="host-group-h1"] [data-testid="group-header"]')
    await h1Header.trigger('click')
    await flushPromises()
    await h1Header.trigger('click')
    await flushPromises()
    expect(h1Header.attributes('aria-expanded')).toBe('true')
    expect(w.find('[data-testid="host-group-h1"]').findAll('[data-testid="task-card"]')).toHaveLength(2)
  })

  it('collapses also work in state grouping mode', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="group-toggle"]').trigger('click')
    await flushPromises()

    const wgGroup = w.find('[data-testid="state-group-waiting_input"]')
    const wgHeader = wgGroup.find('[data-testid="group-header"]')
    await wgHeader.trigger('click')
    await flushPromises()

    expect(wgGroup.findAll('[data-testid="task-card"]')).toHaveLength(0)
    expect(wgHeader.attributes('aria-expanded')).toBe('false')
    // Other state groups stay open.
    const failed = w.find('[data-testid="state-group-failed"]')
    expect(failed.find('[data-testid="group-header"]').attributes('aria-expanded')).toBe('true')
  })

  it('group mark-all button does NOT toggle group collapse', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const h1Header = w.find('[data-testid="host-group-h1"] [data-testid="group-header"]')
    expect(h1Header.attributes('aria-expanded')).toBe('true')

    await w.find('[data-testid="mark-all-host-h1"]').trigger('click')
    await flushPromises()
    expect(h1Header.attributes('aria-expanded')).toBe('true')
  })

  it('collapse state survives unmount + remount (MobileApp v-if cycle)', async () => {
    // First mount: collapse h1.
    const first = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const firstHeader = first.find('[data-testid="host-group-h1"] [data-testid="group-header"]')
    await firstHeader.trigger('click')
    await flushPromises()
    expect(firstHeader.attributes('aria-expanded')).toBe('false')

    // Tear down (mimics MobileApp toggling view away from 'list').
    first.unmount()

    // Remount: h1 must still be collapsed.
    const second = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const secondHeader = second.find('[data-testid="host-group-h1"] [data-testid="group-header"]')
    expect(secondHeader.attributes('aria-expanded')).toBe('false')
    expect(second.find('[data-testid="host-group-h1"]').findAll('[data-testid="task-card"]')).toHaveLength(0)

    // Sanity: h2 stayed expanded.
    expect(second.find('[data-testid="host-group-h2"] [data-testid="group-header"]')
      .attributes('aria-expanded')).toBe('true')
  })

  it('keyboard Enter/Space on a header toggles collapse', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const h1Header = w.find('[data-testid="host-group-h1"] [data-testid="group-header"]')
    await h1Header.trigger('keydown.enter')
    await flushPromises()
    expect(h1Header.attributes('aria-expanded')).toBe('false')
    await h1Header.trigger('keydown.space')
    await flushPromises()
    expect(h1Header.attributes('aria-expanded')).toBe('true')
  })
})
