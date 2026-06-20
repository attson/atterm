import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  getFeishuAdminConfig: vi.fn(),
  setFeishuAdminConfig: vi.fn(),
  generateFeishuKey: vi.fn(),
}))

import FeishuConfig from '@/admin/tabs/FeishuConfig.vue'
import { getFeishuAdminConfig, setFeishuAdminConfig } from '@shared/api/admin'
import { installI18nTestHooks } from '../../i18n-test-helper'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(FeishuConfig) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

installI18nTestHooks()
describe('FeishuConfig.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  // Regression: the component must fetch the config on mount, otherwise the
  // form never renders (the tab showed only its title — broken since v0.2.119).
  it('loads the config on mount and renders the form', async () => {
    ;(getFeishuAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      enabled: false,
      running: false,
      base_url: '',
      key_set: false,
      requires_restart_for_vapid: true,
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(getFeishuAdminConfig).toHaveBeenCalledTimes(1)
    // The enable switch only exists once cfg is loaded (v-if="cfg").
    expect(wrapper.find('[data-testid="feishu-enabled"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="feishu-save"]').exists()).toBe(true)
  })

  it('saves the enabled flag + key on save', async () => {
    ;(getFeishuAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      enabled: false, running: false, base_url: '', key_set: false, requires_restart_for_vapid: true,
    })
    ;(setFeishuAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      enabled: true, running: true, base_url: '', key_set: true, key_last4: 'abcd', requires_restart_for_vapid: true,
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="feishu-save"]').trigger('click')
    await flushPromises()

    expect(setFeishuAdminConfig).toHaveBeenCalledTimes(1)
  })
})
