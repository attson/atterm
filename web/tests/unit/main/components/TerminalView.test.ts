import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

const sessionConnectionInstances: any[] = []

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

function mountWithProvider(props: Record<string, unknown>) {
  const Wrapper = defineComponent({
    setup() {
      return () => h(NMessageProvider, null, { default: () => h(TerminalView as any, props) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

describe('TerminalView.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    sessionConnectionInstances.length = 0
    vi.clearAllMocks()
  })

  it('creates a SessionConnection for the sessionId prop and attaches', async () => {
    const wrapper = mountWithProvider({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()

    expect(sessionConnectionInstances.length).toBe(1)
    expect(sessionConnectionInstances[0]!.sessionId).toBe('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa')
    expect(sessionConnectionInstances[0]!.attach).toHaveBeenCalled()
  })

  it('reflects status changes from SessionConnection onStatus handler', async () => {
    const wrapper = mountWithProvider({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
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
    const wrapper = mountWithProvider({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    conn.fire('onReplayProgress', { phase: 'start', bytes: 0, total_bytes: 100, seq: 0 })
    await flushPromises()
    expect(wrapper.find('[data-testid="replay-progress"]').exists()).toBe(true)

    conn.fire('onReplayProgress', { phase: 'end', bytes: 100, total_bytes: 100, seq: 0 })
    await flushPromises()
    expect(wrapper.find('[data-testid="replay-progress"]').exists()).toBe(false)
  })

  it('detaches on unmount', async () => {
    const wrapper = mountWithProvider({ sessionId: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa' })
    await flushPromises()
    const conn = sessionConnectionInstances[0]!

    wrapper.unmount()
    expect(conn.detach).toHaveBeenCalled()
  })
})
