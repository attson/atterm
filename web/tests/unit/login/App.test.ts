import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/auth', () => ({
  login: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('test'),
  formatVersionLabel: (version: string) => `version ${version || 'dev'}`,
}))

import App from '@/login/App.vue'
import AppSource from '@/login/App.vue?raw'
import { login } from '@shared/api/auth'
import { ApiError } from '@shared/api/client'
import { installI18nTestHooks } from '../i18n-test-helper'

installI18nTestHooks()
describe('Login App.vue', () => {
  it('overrides Chrome autofill background on login inputs', () => {
    expect(AppSource).toContain(':-webkit-autofill')
    expect(AppSource).toContain(':has(input:-webkit-autofill)')
    expect(AppSource).toContain('-webkit-text-fill-color')
    expect(AppSource).toContain('-webkit-box-shadow')
    expect(AppSource).toContain('-webkit-background-clip')
  })

  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/login.html', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('calls login() with the entered credentials and redirects to / on success', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(login).toHaveBeenCalledWith('a@b', 'password-1234')
    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('uses safeNext on ?next= when provided', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, search: '?next=%2Fsettings.html%3Ftab%3Dtokens' },
      writable: true,
    })
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/settings.html?tab=tokens')
  })

  it('rejects open-redirect ?next= values (//evil → /)', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, search: '?next=' + encodeURIComponent('//evil.example') },
      writable: true,
    })
    ;(login as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('password-1234')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/')
  })

  it('shows "Invalid email or password." on 401 invalid_credentials', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(401, 'invalid_credentials', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('wrong')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Invalid email or password.')
    expect(window.location.assign).not.toHaveBeenCalled()
  })

  it('shows the rate-limit message on 429', async () => {
    ;(login as ReturnType<typeof vi.fn>).mockRejectedValue(
      new ApiError(429, 'rate_limited', null),
    )
    const wrapper = mount(App)

    await wrapper.find('input[autocomplete="username"]').setValue('a@b')
    await wrapper.find('input[autocomplete="current-password"]').setValue('p')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Too many attempts')
  })
})
