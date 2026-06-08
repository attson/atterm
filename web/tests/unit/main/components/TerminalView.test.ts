import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const sessionConnectionInstances: any[] = []
const terminalInstances: any[] = []

vi.mock('xterm', () => {
  return {
    Terminal: vi.fn().mockImplementation(() => {
      const instance = {
        options: {},
        loadAddon: vi.fn(),
        open: vi.fn(),
        focus: vi.fn(),
        onData: vi.fn(),
        onResize: vi.fn(),
        write: vi.fn((_data: unknown, cb?: () => void) => cb?.()),
        scrollToBottom: vi.fn(),
        getSelection: vi.fn().mockReturnValue(''),
        reset: vi.fn(),
        dispose: vi.fn(),
      }
      terminalInstances.push(instance)
      return instance
    }),
  }
})

vi.mock('xterm-addon-fit', () => ({
  FitAddon: vi.fn().mockImplementation(() => ({ fit: vi.fn() })),
}))

vi.mock('@shared/ws/client-conn', () => {
  return {
    SessionConnection: vi.fn().mockImplementation((sessionId: string, handlers: any) => {
      const instance = {
        sessionId,
        handlers,
        attach: vi.fn(),
        detach: vi.fn(),
        sendInput: vi.fn(),
        sendResize: vi.fn(),
        sendPasteImage: vi.fn(),
        claimDriver: vi.fn(),
        fire(name: string, ...args: any[]) {
          handlers[name]?.(...args)
        },
      }
      sessionConnectionInstances.push(instance)
      return instance
    }),
  }
})

import TerminalView from '@/main/components/TerminalView.vue'
import { installI18nTestHooks } from '../../i18n-test-helper'

function mountView(props: Record<string, unknown>) {
  return mount(TerminalView as any, { props, attachTo: document.body })
}

installI18nTestHooks()
describe('TerminalView.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    sessionConnectionInstances.length = 0
    terminalInstances.length = 0
    vi.clearAllMocks()
  })

  it('creates a SessionConnection for the sessionId prop and attaches', async () => {
    mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()

    expect(sessionConnectionInstances.length).toBe(1)
    expect(sessionConnectionInstances[0]!.sessionId).toBe('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa')
    expect(sessionConnectionInstances[0]!.attach).toHaveBeenCalled()
  })

  it('reflects status changes from SessionConnection onStatus handler', async () => {
    const wrapper = mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    conn.fire('onStatus', 'attached')
    await flushPromises()
    expect(wrapper.find('[data-testid="status-line"]').text()).toContain('attached')

    conn.fire('onStatus', 'reconnecting')
    await flushPromises()
    expect(wrapper.find('[data-testid="status-line"]').text()).toContain('reconnecting')
  })

  it('shows replay-progress overlay while replay is in flight, hides on end', async () => {
    const wrapper = mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    conn.fire('onReplayProgress', { phase: 'start', bytes: 0, total_bytes: 100, seq: 0 })
    await flushPromises()
    expect(wrapper.find('[data-testid="replay-progress"]').exists()).toBe(true)

    conn.fire('onReplayProgress', { phase: 'end', bytes: 100, total_bytes: 100, seq: 0 })
    await flushPromises()
    expect(wrapper.find('[data-testid="replay-progress"]').exists()).toBe(false)
  })

  it('scrolls to the newest output after initial replay finishes', async () => {
    mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!
    const terminal = terminalInstances[0]!

    conn.fire('onReplayProgress', { phase: 'start', bytes: 0, total_bytes: 100, seq: 0 })
    conn.fire('onOutput', new TextEncoder().encode('old output\r\n'), 1)
    expect(terminal.scrollToBottom).not.toHaveBeenCalled()

    conn.fire('onReplayProgress', { phase: 'end', bytes: 100, total_bytes: 100, seq: 1 })
    await flushPromises()

    expect(terminal.scrollToBottom).toHaveBeenCalledOnce()
  })

  it('shows the viewer overlay when not driver; take-control calls claimDriver', async () => {
    const wrapper = mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    expect(wrapper.find('[data-testid="viewer-overlay"]').exists()).toBe(false)

    conn.fire('onDriverChange', 'owner-A', false, 'mac-mini')
    await flushPromises()
    expect(wrapper.find('[data-testid="viewer-overlay"]').exists()).toBe(true)

    await wrapper.find('[data-testid="take-control"]').trigger('click')
    expect(conn.claimDriver).toHaveBeenCalled()

    conn.fire('onDriverChange', 'me', true, 'web')
    await flushPromises()
    expect(wrapper.find('[data-testid="viewer-overlay"]').exists()).toBe(false)
  })

  it('detaches on unmount', async () => {
    const wrapper = mountView({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    wrapper.unmount()
    expect(conn.detach).toHaveBeenCalled()
  })
})
