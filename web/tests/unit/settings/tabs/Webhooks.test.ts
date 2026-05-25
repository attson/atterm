import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/webhooks', () => ({
  listWebhooks: vi.fn().mockResolvedValue([
    { id: '1', name: 'phone', url: 'https://open.feishu.cn/x', format: 'feishu', created_at: '2026-05-24T00:00:00Z' },
  ]),
  createWebhook: vi.fn().mockResolvedValue({ id: '2', name: 'box', url: 'https://r/y', format: 'generic', created_at: '2026-05-24T00:00:00Z' }),
  deleteWebhook: vi.fn().mockResolvedValue(undefined),
}))

import Webhooks from '@/settings/tabs/Webhooks.vue'
import { listWebhooks, createWebhook, deleteWebhook } from '@shared/api/webhooks'
import { installI18nTestHooks } from '../../i18n-test-helper'

installI18nTestHooks()
describe('Webhooks tab', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists existing webhooks on mount', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    expect(listWebhooks).toHaveBeenCalled()
    expect(w.text()).toContain('phone')
    expect(w.text()).toContain('open.feishu.cn')
  })

  it('creates a webhook from the form', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    await w.find('[data-testid="wh-name"]').setValue('box')
    await w.find('[data-testid="wh-url"]').setValue('https://r/y')
    await w.find('[data-testid="wh-add"]').trigger('click')
    await flushPromises()
    expect(createWebhook).toHaveBeenCalledWith(expect.objectContaining({ name: 'box', url: 'https://r/y' }))
  })

  it('deletes a webhook', async () => {
    const w = mount(Webhooks)
    await flushPromises()
    await w.find('[data-testid="wh-del-1"]').trigger('click')
    await flushPromises()
    expect(deleteWebhook).toHaveBeenCalledWith('1')
  })
})
