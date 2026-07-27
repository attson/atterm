import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn(),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('test'),
  formatVersionLabel: (version: string) => `version ${version || 'dev'}`,
}))

import Topbar from '@shared/components/Topbar.vue'
import { getMe } from '@shared/api/me'
import { logout } from '@shared/api/auth'
import { installI18nTestHooks } from '../../i18n-test-helper'

installI18nTestHooks()
describe('Topbar.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    vi.clearAllMocks()
    originalLocation = window.location
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/admin/', search: '', hash: '', assign },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders the Home nav link unconditionally', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false })
    const wrapper = mount(Topbar, { props: { active: 'home' } })
    await flushPromises()

    const links = wrapper.findAll('nav a')
    const labels = links.map((l) => l.text())
    expect(labels).toContain('Home')
  })

  it('hides Admin link when is_admin is false', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false })
    const wrapper = mount(Topbar, { props: { active: 'home' } })
    await flushPromises()

    expect(wrapper.findAll('nav a').map((l) => l.text())).not.toContain('Admin')
  })

  it('shows Admin link when is_admin is true', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: true })
    const wrapper = mount(Topbar, { props: { active: 'admin' } })
    await flushPromises()

    expect(wrapper.findAll('nav a').map((l) => l.text())).toContain('Admin')
  })

  it('marks the active link with aria-current=page', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: true })
    const wrapper = mount(Topbar, { props: { active: 'admin' } })
    await flushPromises()

    const adminLink = wrapper.findAll('nav a').find((l) => l.text() === 'Admin')
    expect(adminLink?.attributes('aria-current')).toBe('page')
  })

  it('Sign-out triggers logout() and navigates to /login.html', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    const wrapper = mount(Topbar, { props: { active: 'home' } })
    await flushPromises()

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(logout).toHaveBeenCalled()
    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })

  it('still navigates to /login.html when logout throws (offline)', async () => {
    ;(getMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'a@b' })
    ;(logout as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('offline'))
    const wrapper = mount(Topbar, { props: { active: 'home' } })
    await flushPromises()

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(window.location.assign).toHaveBeenCalledWith('/login.html')
  })
})
