import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: true }),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('test'),
  formatVersionLabel: (version: string) => `version ${version || 'dev'}`,
}))
vi.mock('@shared/api/admin', () => ({
  listInvitations: vi.fn().mockResolvedValue([]),
  createInvitation: vi.fn(),
  listUsers: vi.fn().mockResolvedValue([]),
  promoteUser: vi.fn(),
  demoteUser: vi.fn(),
  resetUserPassword: vi.fn(),
  disableUser: vi.fn(),
  getAdminConfig: vi.fn().mockResolvedValue({
    rate_limit_per_minute: 0,
    max_connections_per_key: 0,
    default_rate_limit_per_minute: 120,
    default_max_connections_per_key: 16,
    version: 'v0.1.79',
  }),
  setAdminConfig: vi.fn(),
}))

import App from '@/admin/App.vue'
import { installI18nTestHooks } from '../i18n-test-helper'

installI18nTestHooks()
describe('Admin App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    document.body.innerHTML = ''
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/admin/', search: '', hash: '', assign: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders all three tab labels', async () => {
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Invitations')
    expect(text).toContain('Users')
    expect(text).toContain('Config')
  })

  it('opens the tab indicated by the hash on first paint', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#users' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Users panel triggers listUsers; empty list renders its data-table container.
    expect(wrapper.html()).toContain('n-data-table')
  })

  it('falls back to the invitations tab when hash is invalid', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#bogus' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    // Invitations panel's empty state message proves we landed on the fallback tab.
    expect(wrapper.text()).toContain('No invitations yet.')
  })
})
