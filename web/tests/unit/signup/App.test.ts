import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/auth', () => ({
  signup: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersionLabel: vi.fn().mockResolvedValue('version test'),
}))

import App from '@/signup/App.vue'
import { signup } from '@shared/api/auth'
import { ApiError } from '@shared/api/client'
import { installI18nTestHooks } from '../i18n-test-helper'

installI18nTestHooks()
describe('Signup App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/signup.html', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('calls signup() with email + password + invite_code and redirects to /', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('invite-xyz')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(signup).toHaveBeenCalledWith('a@b', 'password-1234', 'invite-xyz')
    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('shows "An account with that email already exists." on 409 email_taken', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(409, 'email_taken', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('invite-xyz')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('An account with that email already exists.')
  })

  it('shows the invite-invalid message on 400 invite_invalid', async () => {
    ;(signup as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(400, 'invite_invalid', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="new-password"]').setValue('password-1234')
    await wrapper.find('input[autocomplete="off"]').setValue('bad-invite')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Invite code is invalid or already used.')
  })
})
