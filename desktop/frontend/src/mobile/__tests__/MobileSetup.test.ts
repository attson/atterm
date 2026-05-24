import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSetup from '../MobileSetup.vue'

let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  platform = createFakePlatform()
  platform.caps = { ...platform.caps, localPty: false, windowControls: false, autoUpdate: false, pluginHost: false, fileDialog: false }
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSetup', () => {
  it('renders url, token, insecure switch, connect', () => {
    const w = mount(MobileSetup)
    expect(w.find('[data-testid="relay-url"]').exists()).toBe(true)
    expect(w.find('[data-testid="relay-token"]').exists()).toBe(true)
    expect(w.find('[data-testid="allow-insecure"]').exists()).toBe(true)
    expect(w.find('[data-testid="connect"]').exists()).toBe(true)
  })

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
})
