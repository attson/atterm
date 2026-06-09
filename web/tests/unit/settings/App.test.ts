import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false }),
  listSessions: vi.fn().mockResolvedValue([]),
  revokeSession: vi.fn(),
  signOutOthers: vi.fn(),
  changePassword: vi.fn(),
  deleteMe: vi.fn(),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('test'),
  formatVersionLabel: (version: string) => `version ${version || 'dev'}`,
}))

import App from '@/settings/App.vue'
import { installI18nTestHooks } from '../i18n-test-helper'

installI18nTestHooks()
describe('Settings App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    document.body.innerHTML = ''
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/settings.html', search: '', hash: '', assign: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders all tab labels', async () => {
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Change Password')
    expect(text).toContain('Signed-in devices')
    expect(text).toContain('Notifications')
    expect(text).toContain('Danger zone')
    expect(text).not.toContain('API Tokens')
  })

  it('opens the tab indicated by the hash on first paint', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#sessions' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Sessions panel renders the "Each row is a browser" hint.
    expect(wrapper.text()).toContain('Each row is a browser')
  })

  it('falls back to the change-password tab when hash is invalid', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#bogus-tab' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Change password panel renders the "Current password" label.
    expect(wrapper.text()).toContain('Current password')
  })
})
