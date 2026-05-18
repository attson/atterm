import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  changePassword: vi.fn(),
}))

import ChangePassword from '@/settings/tabs/ChangePassword.vue'
import { changePassword } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

describe('ChangePassword.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '#change-password', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('submits current + new password and redirects to /login.html on success', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockResolvedValue(undefined)
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('old-password-1')
    await wrapper.find('input[autocomplete="new-password"]').setValue('new-password-12')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(changePassword).toHaveBeenCalledWith('old-password-1', 'new-password-12')
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('shows the current-password-wrong message on 401', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'current_password_wrong', null),
    )
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('wrong')
    await wrapper.find('input[autocomplete="new-password"]').setValue('new-password-12')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Current password is incorrect.')
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the password-weak message on 400', async () => {
    ;(changePassword as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'password_weak', null),
    )
    const wrapper = mount(ChangePassword)

    await wrapper.find('input[autocomplete="current-password"]').setValue('old-password-1')
    await wrapper.find('input[autocomplete="new-password"]').setValue('short')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('at least 12 characters')
  })
})
