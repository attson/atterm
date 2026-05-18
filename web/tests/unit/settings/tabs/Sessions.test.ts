import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/me', () => ({
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
  signOutOthers: vi.fn(),
}))

import Sessions from '@/settings/tabs/Sessions.vue'
import { listSessions, revokeSession, signOutOthers } from '@shared/api/me'

const baseTime = Date.UTC(2026, 0, 1)

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Sessions) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

async function clickConfirm() {
  // Naive UI's <n-popconfirm> teleports the popup to body; clicking
  // the trigger only opens the popup, so we then click the primary
  // confirm button inside it.
  const confirmBtn = document.querySelector(
    '.n-popconfirm .n-button--primary-type',
  ) as HTMLButtonElement | null
  confirmBtn?.click()
  await flushPromises()
}

describe('Sessions.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists sessions and marks the current one', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id_hash: 'h-current', user_agent: 'Chrome/120',  ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      { id_hash: 'h-other',   user_agent: 'Firefox/125', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('Chrome')
    expect(wrapper.text()).toContain('Firefox')
    expect(wrapper.text()).toContain('this device')
    // Only the non-current row should have a Revoke button.
    expect(wrapper.findAll('[data-testid^="revoke-session-"]').length).toBe(1)
  })

  it('revokes a single session and reloads the list', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
        { id_hash: 'h-other',   user_agent: 'Firefox', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
      ])
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      ])
    ;(revokeSession as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="revoke-session-h-other"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(revokeSession).toHaveBeenCalledWith('h-other')
    expect(listSessions).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('Firefox')
  })

  it('sign-out-others POSTs and reloads', async () => {
    ;(listSessions as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
        { id_hash: 'h-other',   user_agent: 'Firefox', ip_prefix: '10.0.1', created_at: baseTime, expires_at: baseTime + 1, is_current: false },
      ])
      .mockResolvedValueOnce([
        { id_hash: 'h-current', user_agent: 'Chrome', ip_prefix: '10.0.0', created_at: baseTime, expires_at: baseTime + 1, is_current: true },
      ])
    ;(signOutOthers as ReturnType<typeof vi.fn>).mockResolvedValue({ deleted: 1 })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="sign-out-others"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(signOutOthers).toHaveBeenCalled()
    expect(listSessions).toHaveBeenCalledTimes(2)
  })
})
