import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  getAdminConfig: vi.fn(),
  setAdminConfig: vi.fn(),
}))

import Config from '@/admin/tabs/Config.vue'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'
import { installI18nTestHooks } from '../../i18n-test-helper'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Config) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

installI18nTestHooks()
describe('Config.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('loads and shows the effective fallback when stored values are 0', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      allowed_origins: [],
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('effective: 120')
    expect(text).toContain('effective: 16')
    expect(text).toContain('v0.1.79')
  })

  it('PUTs the new values on save', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      allowed_origins: [],
      version: 'v0.1.79',
    })
    ;(setAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 240,
      max_connections_per_key: 32,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      allowed_origins: [],
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="cfg-rate"]').setValue('240')
    await wrapper.find('[data-testid="cfg-conn"]').setValue('32')
    await wrapper.find('[data-testid="cfg-save"]').trigger('click')
    await flushPromises()

    expect(setAdminConfig).toHaveBeenCalledWith(expect.objectContaining({
      rate_limit_per_minute: 240,
      max_connections_per_key: 32,
      allowed_origins: [],
    }))
  })

  it('negative values disable the limit entirely (effective: disabled)', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: -1,
      max_connections_per_key: -1,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      allowed_origins: [],
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('effective: disabled')
  })

  it('shows current allowed_origins and sends parsed list on save (trimmed, blanks dropped)', async () => {
    ;(getAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      debug: false,
      debug_payload: false,
      allowed_origins: ['https://relay.example.com', 'capacitor://localhost'],
      version: 'v0.1.79',
    })
    ;(setAdminConfig as ReturnType<typeof vi.fn>).mockResolvedValue({
      rate_limit_per_minute: 0,
      max_connections_per_key: 0,
      default_rate_limit_per_minute: 120,
      default_max_connections_per_key: 16,
      debug: false,
      debug_payload: false,
      allowed_origins: ['https://relay.example.com'],
      version: 'v0.1.79',
    })
    const wrapper = mountWithProvider()
    await flushPromises()

    const originsEl = wrapper.find('[data-testid="cfg-origins"]')
    expect((originsEl.element as HTMLTextAreaElement).value).toBe(
      'https://relay.example.com\ncapacitor://localhost',
    )
    // Replace with a value that includes extra whitespace + a blank line to
    // exercise the trim/filter path.
    await originsEl.setValue('  https://relay.example.com  \n\n')
    await wrapper.find('[data-testid="cfg-save"]').trigger('click')
    await flushPromises()

    expect(setAdminConfig).toHaveBeenCalledWith(expect.objectContaining({
      allowed_origins: ['https://relay.example.com'],
    }))
  })
})
