import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import type { RemoteSession } from '../../platform/types'

const sessions: RemoteSession[] = [
  { session_id: 'a', host_id: 'h1', host: 'box1', user: 'me', title: 'claude', cwd: '/Users/me/proj', cols: 80, rows: 24, task_state: 'waiting_input', current_command: 'codex', last_output_at: 1715234579, remote_permission: 'control' },
  { session_id: 'b', host_id: 'h1', host: 'box1', user: 'me', title: 'zsh', cols: 100, rows: 30, task_state: 'running', current_command: 'npm test', command_started_at: 1715234500 },
  { session_id: 'c', host_id: 'h2', host: 'box2', user: 'me', title: 'codex', cols: 120, rows: 40, task_state: 'failed', current_command: 'go test ./...', command_exit_code: 1, command_duration_ms: 12500, remote_permission: 'view' },
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
  it('lists sessions as task cards grouped by task state on mount', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="task-section-needs_attention"]').exists()).toBe(true)
    expect(w.find('[data-testid="task-section-running"]').exists()).toBe(true)
    expect(w.find('[data-testid="task-section-failed"]').exists()).toBe(true)
    expect(w.findAll('[data-testid="task-card"]').length).toBe(3)
  })

  it('shows task command, host, cwd, dimensions and permission on each card', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const card = w.find('[data-testid="task-card-a"]')
    expect(card.text()).toContain('codex')
    expect(card.text()).toContain('box1')
    expect(card.text()).toContain('/Users/me/proj')
    expect(card.text()).toContain('80×24')
    expect(card.text()).toContain('control')
    expect(card.text()).toContain('waiting input')
    expect(card.text()).toContain('last output')
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
    await w.find('[data-testid="task-card"]').trigger('click')
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

  it('shows relay disconnected empty state when the session fetch fails', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('network down'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="relay-disconnected"]').exists()).toBe(true)
    expect(w.text()).toContain('network down')
  })

  it('renders task cards in a narrow mobile viewport', async () => {
    Object.defineProperty(window, 'innerWidth', { value: 390, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: 844, configurable: true })
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('.list').exists()).toBe(true)
    expect(w.findAll('[data-testid="task-card"]').length).toBe(3)
  })

  it('emits token-invalid when listRemoteSessions throws relay_unauthorized', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })

  it('shows the localised type chip for non-shell sessions', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { session_id: 'a', host_id: 'h', host: 'box', user: 'me', title: 'claude', cwd: '/', cols: 80, rows: 24, type: 'ai' },
      { session_id: 'b', host_id: 'h', host: 'box', user: 'me', title: 'bash', cwd: '/', cols: 80, rows: 24, type: 'shell' },
    ])

    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()

    const aiCard = w.get('[data-testid="task-card-a"]')
    expect(aiCard.find('.type-chip').exists()).toBe(true)
    expect(aiCard.find('.type-chip').text()).toBe('AI')

    const shellCard = w.get('[data-testid="task-card-b"]')
    expect(shellCard.find('.type-chip').exists()).toBe(false)
  })
})
