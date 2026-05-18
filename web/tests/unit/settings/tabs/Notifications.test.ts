import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

const enablePushFlow = vi.fn()
const disablePushFlow = vi.fn()
const testPush = vi.fn()

vi.mock('@shared/api/push-flow', () => ({
  enablePushFlow: (...args: unknown[]) => enablePushFlow(...args),
  disablePushFlow: (...args: unknown[]) => disablePushFlow(...args),
}))
vi.mock('@shared/api/push', () => ({
  testPush: (...args: unknown[]) => testPush(...args),
}))

import Notifications from '@/settings/tabs/Notifications.vue'

function mountWithProvider() {
  const Host = defineComponent({
    setup() {
      return () => h(NMessageProvider, null, { default: () => h(Notifications) })
    },
  })
  return mount(Host, { attachTo: document.body })
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  Object.defineProperty(window, 'Notification', {
    value: { permission: 'default', requestPermission: () => Promise.resolve('granted') },
    configurable: true,
  })
  Object.defineProperty(navigator, 'serviceWorker', {
    value: {
      ready: Promise.resolve({
        pushManager: {
          subscribe: () => Promise.resolve({
            endpoint: 'https://example/abc',
            toJSON: () => ({ endpoint: 'https://example/abc', keys: { p256dh: 'aaa', auth: 'bbb' } }),
          }),
          getSubscription: () => Promise.resolve(null),
        },
      }),
    },
    configurable: true,
  })
})

describe('Notifications tab', () => {
  it('renders a permission status line', async () => {
    const wrapper = mountWithProvider()
    await flushPromises()
    expect(wrapper.find('[data-testid="push-status"]').text()).toMatch(/default/i)
  })

  it('enables push when the Enable button is clicked', async () => {
    enablePushFlow.mockResolvedValue({ ok: true })
    const wrapper = mountWithProvider()
    await flushPromises()
    await wrapper.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(enablePushFlow).toHaveBeenCalledTimes(1)
  })

  it('shows an error message when enablePushFlow returns ok=false', async () => {
    enablePushFlow.mockResolvedValue({ ok: false, reason: 'denied' })
    const wrapper = mountWithProvider()
    await flushPromises()
    await wrapper.find('[data-testid="push-enable"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/denied|permission/i)
  })

  it('sends a test notification via testPush', async () => {
    testPush.mockResolvedValue(2)
    // Simulate subscribed state so the button is enabled.
    Object.defineProperty(navigator, 'serviceWorker', {
      value: {
        ready: Promise.resolve({
          pushManager: {
            subscribe: () => Promise.resolve(null),
            getSubscription: () => Promise.resolve({ endpoint: 'https://example/abc' }),
          },
        }),
      },
      configurable: true,
    })
    const wrapper = mountWithProvider()
    await flushPromises()
    await wrapper.find('[data-testid="push-test"]').trigger('click')
    await flushPromises()
    expect(testPush).toHaveBeenCalledTimes(1)
  })
})
