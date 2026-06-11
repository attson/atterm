import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { initI18n, resetI18nForTest } from '../../i18n'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSetup from '../MobileSetup.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(async () => {
  vi.clearAllMocks()
  resetI18nForTest()
  await initI18n({
    getLanguages: () => ['en-US'],
    listenLanguageChange: () => () => undefined,
  })
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false, windowControls: false, autoUpdate: false, pluginHost: false, fileDialog: false }
  __setPlatformForTests(platform)
})
afterEach(() => {
  __setPlatformForTests(null)
  resetI18nForTest()
})

describe('MobileSetup — fields', () => {
  it('renders url, scheme dropdown, email, password, connect', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="relay-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-scheme"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-email"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-password"]').exists()).toBe(true)
    expect(w.find('[data-testid="connect"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-token"]').exists()).toBe(false)
  })

  it('password show/hide toggle flips the input type', async () => {
    const w = mount(MobileSetup)
    const input = w.find('[data-testid="relay-password"]').element as HTMLInputElement
    expect(input.type).toBe('password')
    await w.find('[data-testid="password-toggle"]').trigger('click')
    expect((w.find('[data-testid="relay-password"]').element as HTMLInputElement).type).toBe('text')
  })

  it('hides the insecure switch entirely when scheme is https', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(false)
  })

  it('shows the insecure switch only after selecting http, and warns once enabled', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-scheme"]').setValue('http://')
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(true)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(false)
    await w.find('[data-testid="allow-insecure"]').setValue(true)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(true)
  })

  it('clears allowInsecure when the scheme switches back to https', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-scheme"]').setValue('http://')
    await w.find('[data-testid="allow-insecure"]').setValue(true)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(true)
    await w.find('[data-testid="relay-scheme"]').setValue('https://')
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(false)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(false)
  })

  it('splits a full URL pasted into the host field into scheme + host', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('http://121.43.40.128:23301')
    expect((w.find('[data-testid="relay-scheme"]').element as HTMLSelectElement).value).toBe('http://')
    expect((w.find('[data-testid="relay-url"]').element as HTMLInputElement).value).toBe('121.43.40.128:23301')
  })

  it('renders a language selector before relay connection', () => {
    const wrapper = mount(MobileSetup, { props: { reason: null } })
    expect(wrapper.text()).toContain('Language')
    expect(wrapper.find('[data-testid="mobile-language"]').exists()).toBe(true)
  })

  it('pre-fills url, email, and password from saved config + Keychain', async () => {
    ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'http://localhost:8080', token: 'sess_x',
      session_expires_at: 0, allow_insecure_relay: true, remote_permission: 'full',
      last_email: 'me@example.com', connected: false,
    })
    ;(platform.relay.loadSavedPassword as ReturnType<typeof vi.fn>).mockResolvedValue('hunter2hunter2')
    const w = mount(MobileSetup)
    await flushPromises()
    expect((w.find('[data-testid="relay-scheme"]').element as HTMLSelectElement).value).toBe('http://')
    expect((w.find('[data-testid="relay-url"]').element as HTMLInputElement).value).toBe('localhost:8080')
    expect((w.find('[data-testid="relay-email"]').element as HTMLInputElement).value).toBe('me@example.com')
    expect((w.find('[data-testid="relay-password"]').element as HTMLInputElement).value).toBe('hunter2hunter2')
  })

  it('shows the session-expired banner when reason prop is set', () => {
    const w = mount(MobileSetup, { props: { reason: 'token_invalid' } })
    expect(w.text()).toMatch(/session|expired|sign in|登录|过期/i)
  })
})

describe('MobileSetup — submit', () => {
  it('shows validation error for malformed url and does not call login', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('not a url')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|malformed/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('shows error when email is empty', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/email/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('shows error when password is empty', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/password/i)
    expect(platform.relay.login).not.toHaveBeenCalled()
  })

  it('on success calls platform.relay.login, fetchMe, and emits connected', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(platform.relay.login).toHaveBeenCalledWith(
      'https://r.example.com', 'me@example.com', 'hunter2hunter2', false,
    )
    expect(platform.relay.fetchMe).toHaveBeenCalled()
    expect(w.emitted('connected')).toBeTruthy()
  })

  it('maps invalid_credentials to a friendly error', async () => {
    ;(platform.relay.login as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('invalid_credentials'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|incorrect|错误/i)
    expect(w.emitted('connected')).toBeFalsy()
  })

  it('maps rate_limited to a friendly error', async () => {
    ;(platform.relay.login as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('rate_limited'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-email"]').setValue('me@example.com')
    await w.find('[data-testid="relay-password"]').setValue('hunter2hunter2')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/too many|频繁|later/i)
  })
})
