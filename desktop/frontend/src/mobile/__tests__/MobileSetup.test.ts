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

describe('MobileSetup', () => {
  it('renders url, scheme dropdown, token, connect', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="relay-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-scheme"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-token"]').exists()).toBe(true)
    expect(w.find('[data-testid="connect"]').exists()).toBe(true)
  })

  it('hides the insecure switch entirely when scheme is https', () => {
    const w = mount(MobileSetup)
    // default scheme is https:// → insecure opt-in is irrelevant and hidden
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(false)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(false)
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
    // toggle + hint both gone, and the flag is reset so it won't be saved
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(false)
    expect(w.find('[data-testid="insecure-hint"]').exists()).toBe(false)
  })

  it('splits a full URL pasted into the host field into scheme + host', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('http://121.43.40.128:23301')
    expect((w.find('[data-testid="relay-scheme"]').element as HTMLSelectElement).value).toBe('http://')
    expect((w.find('[data-testid="relay-url"]').element as HTMLInputElement).value).toBe('121.43.40.128:23301')
  })

  it("renders a language selector before relay connection", () => {
    const wrapper = mount(MobileSetup, { props: { reason: null } });
    expect(wrapper.text()).toContain("Language");
    expect(wrapper.find('[data-testid="mobile-language"]').exists()).toBe(true);
  });

  it('shows validation error for malformed url', async () => {
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('not a url')
    await w.find('[data-testid="relay-token"]').setValue('atk_x')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/invalid|malformed/i)
    expect(platform.relay.save).not.toHaveBeenCalled()
  })

  it('on success saves config, calls fetchMe, emits connected', async () => {
    ;(platform.relay.fetchMe as ReturnType<typeof vi.fn>).mockResolvedValue({ user_id: 'u', email: 'e' })
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-token"]').setValue('atk_good')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(platform.relay.save).toHaveBeenCalled()
    expect(platform.relay.fetchMe).toHaveBeenCalled()
    expect(w.emitted('connected')).toBeTruthy()
  })

  it('shows token-invalid error on fetchMe 401-style rejection', async () => {
    ;(platform.relay.fetchMe as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('relay fetchMe failed: HTTP 401'))
    const w = mount(MobileSetup)
    await w.find('[data-testid="relay-url"]').setValue('https://r.example.com')
    await w.find('[data-testid="relay-token"]').setValue('atk_bad')
    await w.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(w.text()).toMatch(/token|invalid|401/i)
    expect(w.emitted('connected')).toBeFalsy()
  })

  it('shows the token-invalid banner when reason prop is set', () => {
    const w = mount(MobileSetup, { props: { reason: 'token_invalid' } })
    expect(w.text()).toMatch(/token|expired|again/i)
  })

  it('pre-fills url, token, and insecure from the saved config', async () => {
    ;(platform.relay.load as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'http://localhost:8080', token: 'atk_saved',
      allow_insecure_relay: true, remote_permission: 'full', connected: false,
    })
    const w = mount(MobileSetup)
    await flushPromises()
    expect((w.find('[data-testid="relay-scheme"]').element as HTMLSelectElement).value).toBe('http://')
    expect((w.find('[data-testid="relay-url"]').element as HTMLInputElement).value).toBe('localhost:8080')
    expect((w.find('[data-testid="relay-token"]').element as HTMLInputElement).value).toBe('atk_saved')
    expect((w.find('[data-testid="allow-insecure"]').element as HTMLInputElement).checked).toBe(true)
  })
})
