import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/me', () => ({
  listTokens: vi.fn(),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
}))

import ApiTokens from '@/settings/tabs/ApiTokens.vue'
import { listTokens, createToken, revokeToken } from '@shared/api/me'
import { installI18nTestHooks } from '../../i18n-test-helper'

// useMessage() inside ApiTokens.vue requires an outer <n-message-provider />.
// Wrap each mount in a thin host that supplies one.
function mountWithProvider() {
  const Host = defineComponent({
    name: 'TestHost',
    setup() {
      return () => h(NMessageProvider, null, { default: () => h(ApiTokens) })
    },
  })
  return mount(Host, { attachTo: document.body })
}

installI18nTestHooks()
describe('ApiTokens.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists active tokens (revoked rows are hidden)', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 't1', name: 'laptop',  prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      { id: 't2', name: 'desktop', prefix: 'atk_bbb', created_at: '2026-01-02T00:00:00Z', revoked_at: '2026-01-05T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('laptop')
    expect(text).toContain('atk_aaa')
    expect(text).not.toContain('desktop')
  })

  it('shows an empty-state message when no active tokens', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('No tokens yet')
  })

  it('creates a token, shows the plaintext once, and reloads the list', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { id: 't1', name: 'laptop', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createToken as ReturnType<typeof vi.fn>).mockResolvedValue({
      id: 't1', plaintext: 'atk_full_secret_xyz', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('input[type="text"]').setValue('laptop')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createToken).toHaveBeenCalledWith('laptop')
    expect(wrapper.text()).toContain('atk_full_secret_xyz')
    expect(listTokens).toHaveBeenCalledTimes(2)
  })

  it('revokes a token and reloads the list', async () => {
    ;(listTokens as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 't1', name: 'laptop', prefix: 'atk_aaa', created_at: '2026-01-01T00:00:00Z' },
      ])
      .mockResolvedValueOnce([])
    ;(revokeToken as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="revoke-t1"]').trigger('click')
    await flushPromises()
    // Naive UI's <n-popconfirm> opens a popup; the actual revoke fires on the
    // confirm button inside the popup, so drive that explicitly.
    const confirmBtn = document.querySelector('.n-popconfirm .n-button--primary-type')
    ;(confirmBtn as HTMLButtonElement)?.click()
    await flushPromises()

    expect(revokeToken).toHaveBeenCalledWith('t1')
    expect(listTokens).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('No tokens yet')
  })
})
