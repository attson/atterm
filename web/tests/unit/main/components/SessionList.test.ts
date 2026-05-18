import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/sessions', () => ({
  listSessions: vi.fn(),
}))

import SessionList from '@/main/components/SessionList.vue'
import { listSessions } from '@shared/api/sessions'

describe('SessionList.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows empty-state when no sessions', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mount(SessionList, { attachTo: document.body })
    await flushPromises()

    expect(wrapper.text()).toMatch(/no live sessions/i)
  })

  it('groups sessions by host and renders cards', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: '11111111-2222-3333-4444-555555555555', command: 'bash', cwd: '/h/a', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h1', host: 'laptop', user: 'me' },
      { id: '22222222-3333-4444-5555-666666666666', command: 'zsh',  cwd: '/h/b', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h1', host: 'laptop', user: 'me' },
      { id: '33333333-4444-5555-6666-777777777777', command: 'fish', cwd: '/h/c', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h2', host: 'desktop', user: 'me' },
    ])
    const wrapper = mount(SessionList, { attachTo: document.body })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('laptop')
    expect(text).toContain('desktop')
    expect(text).toContain('bash')
    expect(text).toContain('zsh')
    expect(text).toContain('fish')
  })

  it('emits navigate event with session id when a card is clicked', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: '11111111-2222-3333-4444-555555555555', command: 'bash', cwd: '/h', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h1', host: 'laptop', user: 'me' },
    ])
    const wrapper = mount(SessionList, { attachTo: document.body })
    await flushPromises()

    await wrapper.find('[data-testid="session-card-11111111-2222-3333-4444-555555555555"]').trigger('click')

    const events = wrapper.emitted('navigate')
    expect(events).toBeTruthy()
    expect(events![0]).toEqual(['11111111-2222-3333-4444-555555555555'])
  })

  it('refreshes every 2 seconds while mounted', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mount(SessionList, { attachTo: document.body })
    await flushPromises()
    expect(listSessions).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(2000)
    await flushPromises()
    expect(listSessions).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    vi.advanceTimersByTime(10_000)
    expect(listSessions).toHaveBeenCalledTimes(2)  // poll stopped after unmount
  })
})
