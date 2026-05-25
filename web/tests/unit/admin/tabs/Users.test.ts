import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { NMessageProvider } from 'naive-ui'

vi.mock('@shared/api/admin', () => ({
  listUsers: vi.fn(),
  promoteUser: vi.fn(),
  demoteUser: vi.fn(),
  resetUserPassword: vi.fn(),
  disableUser: vi.fn(),
}))

import Users from '@/admin/tabs/Users.vue'
import {
  listUsers,
  promoteUser,
  demoteUser,
  resetUserPassword,
  disableUser,
} from '@shared/api/admin'
import { ApiError } from '@shared/api/client'
import { installI18nTestHooks } from '../../i18n-test-helper'

function mountWithProvider() {
  const Wrapper = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Users) })
    },
  })
  return mount(Wrapper, { attachTo: document.body })
}

async function clickConfirm() {
  const confirmBtn = document.querySelector(
    '.n-popconfirm .n-button--primary-type',
  ) as HTMLButtonElement | null
  confirmBtn?.click()
  await flushPromises()
}

installI18nTestHooks()
describe('Users.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('lists users with role + status labels', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
      { id: 'u2', email: 'plain@example', created_at: '2026-01-02T00:00:00Z', is_admin: false },
      { id: 'u3', email: 'gone@example',  created_at: '2026-01-03T00:00:00Z', is_admin: false, disabled_at: '2026-02-01T00:00:00Z' },
    ])
    const wrapper = mountWithProvider()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('admin@example')
    expect(text).toContain('plain@example')
    expect(text).toContain('gone@example')
    expect(text).toContain('admin')
    expect(text).toContain('disabled')
  })

  it('promotes a user and reloads', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
      ])
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
      ])
    ;(promoteUser as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="promote-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(promoteUser).toHaveBeenCalledWith('u2')
    expect(listUsers).toHaveBeenCalledTimes(2)
  })

  it('reset-password shows the temporary plaintext', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
    ])
    ;(resetUserPassword as ReturnType<typeof vi.fn>).mockResolvedValue({ plaintext: 'tmp_super_secret' })
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="reset-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(resetUserPassword).toHaveBeenCalledWith('u2')
    expect(wrapper.text()).toContain('tmp_super_secret')
  })

  it('disable user calls the endpoint and reloads', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false },
      ])
      .mockResolvedValueOnce([
        { id: 'u2', email: 'plain@example', created_at: '2026-01-01T00:00:00Z', is_admin: false, disabled_at: '2026-03-01T00:00:00Z' },
      ])
    ;(disableUser as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="disable-u2"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(disableUser).toHaveBeenCalledWith('u2')
    expect(listUsers).toHaveBeenCalledTimes(2)
  })

  it('demote on the last admin surfaces the last_admin message', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
    ])
    ;(demoteUser as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'last_admin', null),
    )
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="demote-u1"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(wrapper.text()).toContain('last admin')
  })

  it('cannot_demote_self surfaces a specific message', async () => {
    ;(listUsers as ReturnType<typeof vi.fn>).mockResolvedValue([
      { id: 'u1', email: 'admin@example', created_at: '2026-01-01T00:00:00Z', is_admin: true },
    ])
    ;(demoteUser as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'cannot_demote_self', null),
    )
    const wrapper = mountWithProvider()
    await flushPromises()

    await wrapper.find('[data-testid="demote-u1"]').trigger('click')
    await flushPromises()
    await clickConfirm()

    expect(wrapper.text()).toContain('demote yourself')
  })
})
