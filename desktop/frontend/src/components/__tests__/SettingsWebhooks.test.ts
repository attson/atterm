import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'

const { fake } = vi.hoisted(() => ({
  fake: {
    relay: {
      load: vi.fn().mockResolvedValue(null),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      fetchMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
      webhooks: {
        list: vi.fn().mockResolvedValue([]),
        create: vi.fn().mockResolvedValue({}),
        delete: vi.fn().mockResolvedValue(undefined),
      },
    },
  },
}))

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const parts = Object.entries(params).map(([key, val]) => `${key}=${val}`).join(',')
      return `${k}(${parts})`
    },
  }),
}))

vi.mock('../../platform', () => ({
  usePlatform: () => fake,
  __fake: fake,
}))

import SettingsWebhooks from '../SettingsWebhooks.vue'

const sampleRow = (over: Record<string, unknown> = {}) => ({
  id: 'wh_1',
  name: 'alerts',
  url: 'https://hook.example/x',
  format: 'generic' as const,
  created_at: '2026-06-13T00:00:00Z',
  ...over,
})

describe('SettingsWebhooks', () => {
  it('renders empty state when list returns no rows', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([])
    const w = mount(SettingsWebhooks)
    await flushPromises()
    expect(w.text()).toContain('settings.webhooks.empty')
    expect(w.findAll('[data-testid^="wh-row-"]')).toHaveLength(0)
  })

  it('renders one row per webhook returned by bridge.list()', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([
      sampleRow({ id: 'wh_1', name: 'a' }),
      sampleRow({ id: 'wh_2', name: 'b', format: 'feishu' }),
    ])
    const w = mount(SettingsWebhooks)
    await flushPromises()
    const rows = w.findAll('[data-testid^="wh-row-"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('a')
    expect(rows[1].text()).toContain('b')
  })

  it('disables submit until name and a valid URL are entered', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([])
    const w = mount(SettingsWebhooks)
    await flushPromises()
    const submit = w.find('[data-testid="wh-submit"]')
    expect((submit.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-testid="wh-name"]').setValue('alerts')
    await w.find('[data-testid="wh-url"]').setValue('not-a-url')
    expect((submit.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-testid="wh-url"]').setValue('https://hook.example/x')
    expect((submit.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('prepends the created row and clears the form on success', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([
      sampleRow({ id: 'wh_old', name: 'old' }),
    ])
    fake.relay.webhooks.create = vi
      .fn()
      .mockResolvedValue(sampleRow({ id: 'wh_new', name: 'fresh' }))
    const w = mount(SettingsWebhooks)
    await flushPromises()

    await w.find('[data-testid="wh-name"]').setValue('fresh')
    await w.find('[data-testid="wh-url"]').setValue('https://hook.example/y')
    await w.find('[data-testid="wh-submit"]').trigger('click')
    await flushPromises()

    expect(fake.relay.webhooks.create).toHaveBeenCalledWith({
      name: 'fresh',
      url: 'https://hook.example/y',
      format: 'generic',
      allow_insecure: false,
    })
    const rows = w.findAll('[data-testid^="wh-row-"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('fresh')
    expect((w.find('[data-testid="wh-name"]').element as HTMLInputElement).value).toBe('')
  })

  it('surfaces a localized message when create fails with a known code', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([])
    fake.relay.webhooks.create = vi.fn().mockRejectedValue(new Error('invalid_url'))
    const w = mount(SettingsWebhooks)
    await flushPromises()

    await w.find('[data-testid="wh-name"]').setValue('x')
    await w.find('[data-testid="wh-url"]').setValue('https://h/x')
    await w.find('[data-testid="wh-submit"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('settings.webhooks.errors.invalid_url')
  })

  it('falls back to generic copy when the error code is unknown', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([])
    fake.relay.webhooks.create = vi.fn().mockRejectedValue(new Error('mystery_error'))
    const w = mount(SettingsWebhooks)
    await flushPromises()

    await w.find('[data-testid="wh-name"]').setValue('x')
    await w.find('[data-testid="wh-url"]').setValue('https://h/x')
    await w.find('[data-testid="wh-submit"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain('settings.webhooks.errors.generic')
  })

  it('shows confirmation dialog and removes the row on confirm', async () => {
    fake.relay.webhooks.list = vi.fn().mockResolvedValue([
      sampleRow({ id: 'wh_kill', name: 'kill-me' }),
    ])
    fake.relay.webhooks.delete = vi.fn().mockResolvedValue(undefined)
    const w = mount(SettingsWebhooks)
    await flushPromises()

    await w.find('[data-testid="wh-delete-wh_kill"]').trigger('click')
    expect(w.find('[data-testid="wh-confirm-delete"]').exists()).toBe(true)

    await w.find('[data-testid="wh-confirm-delete-button"]').trigger('click')
    await flushPromises()

    expect(fake.relay.webhooks.delete).toHaveBeenCalledWith('wh_kill')
    expect(w.findAll('[data-testid^="wh-row-"]')).toHaveLength(0)
  })

  it('surfaces a list-load error when bridge.list() throws', async () => {
    fake.relay.webhooks.list = vi.fn().mockRejectedValue(new Error('unauthorized'))
    const w = mount(SettingsWebhooks)
    await flushPromises()
    expect(w.text()).toContain('settings.webhooks.errors.unauthorized')
  })
})
