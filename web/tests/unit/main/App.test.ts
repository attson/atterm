import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

vi.mock('@shared/api/me', () => ({
  getMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'a@b', is_admin: false }),
}))
vi.mock('@shared/api/auth', () => ({
  logout: vi.fn(),
}))
vi.mock('@shared/api/version', () => ({
  fetchVersion: vi.fn().mockResolvedValue('test'),
  formatVersionLabel: (version: string) => `version ${version || 'dev'}`,
}))
vi.mock('@shared/api/sessions', () => ({
  listSessions: vi.fn().mockResolvedValue([]),
}))
vi.mock('@shared/ws/client-conn', () => ({
  SessionConnection: vi.fn().mockImplementation(() => ({
    attach: vi.fn(),
    detach: vi.fn(),
    sendInput: vi.fn(),
    sendResize: vi.fn(),
    sendPasteImage: vi.fn(),
  })),
}))

import App from '@/main/App.vue'
import PasteFallback from '@/main/components/PasteFallback.vue'
import { pasteImageBus } from '@/main/lib/pasteImageBus'
import { installI18nTestHooks } from '../i18n-test-helper'

installI18nTestHooks()
describe('Main (home) App.vue', () => {
  let originalLocation: Location

  beforeEach(() => {
    document.body.innerHTML = ''
    originalLocation = window.location
    Object.defineProperty(window, 'location', {
      value: { origin: 'http://localhost', pathname: '/', search: '', hash: '', assign: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('renders the session list when hash is empty', async () => {
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.text()).toMatch(/active sessions/i)
  })

  it('renders the terminal view when hash is #/s/<uuid>', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#/s/11111111-2222-3333-4444-555555555555' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.find('[data-testid="status-line"]').exists()).toBe(true)
  })

  it('shows a view-only warning and passes focus input for notification deep links', async () => {
    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#/s/11111111-2222-3333-4444-555555555555?notification=waiting_input&focus=input&permission=view' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.find('[data-testid="view-only-notice"]').exists()).toBe(true)
    const term = wrapper.findComponent({ name: 'TerminalView' })
    expect(term.props('focusInput')).toBe(true)
  })

  it('reacts to hashchange events to switch views', async () => {
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()
    expect(wrapper.text()).toMatch(/active sessions/i)

    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#/s/11111111-2222-3333-4444-555555555555' },
      writable: true,
    })
    window.dispatchEvent(new HashChangeEvent('hashchange'))
    await flushPromises()

    expect(wrapper.find('[data-testid="status-line"]').exists()).toBe(true)
  })

  it('emits pasteImageBus when a pasted image is dispatched via PasteFallback', async () => {
    const emitSpy = vi.spyOn(pasteImageBus, 'emit')

    Object.defineProperty(window, 'location', {
      value: { ...window.location, hash: '#/s/11111111-2222-3333-4444-555555555555' },
      writable: true,
    })
    const wrapper = mount(App, { attachTo: document.body })
    await flushPromises()

    const fallback = wrapper.findComponent(PasteFallback)
    const file = new File([new Uint8Array([1, 2])], 'pasted.png', { type: 'image/png' })
    fallback.vm.$emit('paste-image', file)
    await flushPromises()

    expect(emitSpy).toHaveBeenCalledTimes(1)
    const [blob, name] = emitSpy.mock.calls[0]!
    expect(blob).toBe(file)
    expect(name).toBe('pasted.png')
    emitSpy.mockRestore()
  })
})
