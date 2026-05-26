import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  listInvitations: vi.fn(),
  createInvitation: vi.fn(),
}))

import Invitations from '@/admin/tabs/Invitations.vue'
import source from '@/admin/tabs/Invitations.vue?raw'
import { listInvitations, createInvitation } from '@shared/api/admin'
import { installI18nTestHooks } from '../../i18n-test-helper'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Invitations) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

installI18nTestHooks()
describe('Invitations.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('lists invitations on mount', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>).mockResolvedValue([
      { code_prefix: 'inv_abc', note: 'colleague', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('inv_abc')
    expect(wrapper.text()).toContain('colleague')
  })

  it('uses the shared locale-aware date formatter', () => {
    expect(source).toContain('formatDateTime')
    expect(source).not.toMatch(/toLocale(?:String|DateString)\(/)
  })

  it('shows an empty-state message when no invitations', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const wrapper = mountWithProvider()
    await flushPromises()

    expect(wrapper.text()).toContain('No invitations yet')
  })

  it('creates a single invitation and shows the plaintext once', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { code_prefix: 'inv_xyz', note: 'laptop', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createInvitation as ReturnType<typeof vi.fn>).mockResolvedValue([
      { plaintext: 'inv_full_secret', code_prefix: 'inv_xyz', note: 'laptop', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="invite-note"]').setValue('laptop')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createInvitation).toHaveBeenCalledWith({ count: 1, note: 'laptop' })
    expect(wrapper.text()).toContain('inv_full_secret')
    expect(listInvitations).toHaveBeenCalledTimes(2)
  })

  it('creates a bulk batch and shows every plaintext', async () => {
    ;(listInvitations as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { code_prefix: 'inv_a', note: 'team', created_at: '2026-01-01T00:00:00Z' },
        { code_prefix: 'inv_b', note: 'team', created_at: '2026-01-01T00:00:00Z' },
      ])
    ;(createInvitation as ReturnType<typeof vi.fn>).mockResolvedValue([
      { plaintext: 'inv_p1', code_prefix: 'inv_a', note: 'team', created_at: '2026-01-01T00:00:00Z' },
      { plaintext: 'inv_p2', code_prefix: 'inv_b', note: 'team', created_at: '2026-01-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="invite-note"]').setValue('team')
    await wrapper.find('[data-testid="invite-count"]').setValue('2')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(createInvitation).toHaveBeenCalledWith({ count: 2, note: 'team' })
    expect(wrapper.text()).toContain('inv_p1')
    expect(wrapper.text()).toContain('inv_p2')
  })
})
