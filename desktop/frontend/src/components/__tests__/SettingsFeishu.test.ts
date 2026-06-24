import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

import SettingsFeishu from '../SettingsFeishu.vue'
import * as api from '../../lib/api'

const HOOK_OK: api.HookInstallState = {
  enabled: true,
  binary_path: '/x',
  binary_ok: true,
  binary_version: 'v',
  settings_path: '/y',
  settings_ok: true,
  last_error: '',
  last_check: '',
}

beforeEach(() => {
  vi.spyOn(api, 'getHookInstallState').mockResolvedValue(HOOK_OK as never)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SettingsFeishu configured view', () => {
  it('local mode: shows configured state, long-conn hint, no callback URL, pair enabled', async () => {
    vi.spyOn(api, 'getFeishuStatus').mockResolvedValue({
      enabled: true,
      mode: 'local',
      bound: false,
      open_id: '',
      disabled: false,
      configured: true,
      app_id: 'cli_app',
    } as never)

    const w = mount(SettingsFeishu)
    await flushPromises()

    expect(w.find('[data-test="feishu-configured"]').exists()).toBe(true)
    expect(w.find('[data-test="feishu-longconn-hint"]').exists()).toBe(true)
    expect(w.find('[data-test="feishu-callback-url"]').exists()).toBe(false)
    // Start-pair button is present and not disabled (the old bug greyed it out
    // after reopen because `saved` reset to false).
    const pair = w.find('[data-test="feishu-begin-pair"]')
    expect(pair.exists()).toBe(true)
    expect((pair.element as HTMLButtonElement).disabled).toBe(false)
    // The empty credentials form must NOT render when already configured.
    expect(w.find('.feishu-fieldset').exists()).toBe(false)
  })

  it('relay mode: shows callback URL and copies it to the clipboard', async () => {
    const url = 'https://relay.example.com/v1/feishu/events/abc123'
    vi.spyOn(api, 'getFeishuStatus').mockResolvedValue({
      enabled: true,
      mode: 'relay',
      bound: false,
      open_id: '',
      disabled: false,
      configured: true,
      callback_url: url,
      app_id_hash: 'abc123',
    } as never)
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })

    const w = mount(SettingsFeishu)
    await flushPromises()

    const input = w.find('[data-test="feishu-callback-url"]')
    expect(input.exists()).toBe(true)
    expect((input.element as HTMLInputElement).value).toBe(url)
    expect(w.find('[data-test="feishu-longconn-hint"]').exists()).toBe(false)

    await w.find('[data-test="feishu-callback-copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(url)

    vi.unstubAllGlobals()
  })

  it('after pairing, polls status and flips to the bound view once the relay binds', async () => {
    vi.useFakeTimers()
    const configured = {
      enabled: true,
      mode: 'relay',
      bound: false,
      open_id: '',
      disabled: false,
      configured: true,
      callback_url: 'https://relay.example.com/v1/feishu/events/abc',
      app_id_hash: 'abc',
    }
    const statusSpy = vi.spyOn(api, 'getFeishuStatus').mockResolvedValue(configured as never)
    vi.spyOn(api, 'beginFeishuPair').mockResolvedValue('SNVXZU' as never)

    const w = mount(SettingsFeishu)
    await flushPromises()

    await w.find('[data-test="feishu-begin-pair"]').trigger('click')
    await flushPromises()
    // Pair code is shown; not yet bound.
    expect(w.text()).toContain('settings.feishu.pair_hint')
    expect(w.find('[data-test="feishu-configured"]').exists()).toBe(true)

    // The user sends /bind to the bot; the relay flips bound server-side.
    statusSpy.mockResolvedValue({ ...configured, bound: true, open_id: 'ou_x' } as never)

    await vi.advanceTimersByTimeAsync(2500)
    await flushPromises()

    // Bound view takes over without reopening the dialog.
    expect(w.find('[data-test="feishu-configured"]').exists()).toBe(false)
    expect(w.text()).toContain('settings.feishu.bound')

    vi.useRealTimers()
  })

  it('unconfigured: shows the empty credentials form', async () => {
    vi.spyOn(api, 'getFeishuStatus').mockResolvedValue({
      enabled: true,
      mode: 'local',
      bound: false,
      open_id: '',
      disabled: false,
      configured: false,
    } as never)

    const w = mount(SettingsFeishu)
    await flushPromises()

    expect(w.find('.feishu-fieldset').exists()).toBe(true)
    expect(w.find('[data-test="feishu-configured"]').exists()).toBe(false)
  })
})
