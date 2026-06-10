import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import {
  saveRelayConfig,
  loadRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import Relay from '@/settings/tabs/Relay.vue'
import { installI18nTestHooks } from '../../i18n-test-helper'

function makeResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

installI18nTestHooks()
describe('settings/tabs/Relay.vue', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    saveRelayConfig({
      baseURL: 'https://r.example.com',
      sessionToken: 'ses_existing',
      expiresAt: null,
      allowInsecure: false,
    })
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock },
      writable: true,
    })
    vi.restoreAllMocks()
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('pre-fills inputs from stored config', () => {
    const wrapper = mount(Relay)
    const base = wrapper.find('input[name="relay-base"]').element as HTMLInputElement
    expect(base.value).toBe('https://r.example.com')
  })

  it('save validates + probes + persists', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u', email: 'e' })))
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('ses_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toMatchObject({
      baseURL: 'https://other.example.com',
      sessionToken: 'ses_new',
    })
  })

  it('shows 401 error and does not save', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(401, { error: 'unauthenticated' })))
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('ses_bad')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/token|invalid/i)
    // The stored config from beforeEach is unchanged
    expect(loadRelayConfig()).toMatchObject({ sessionToken: 'ses_existing' })
  })

  it('shows 403 origin-rejected error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(403, { error: 'origin_rejected' })))
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/origin|ATTERM_ORIGINS|capacitor/i)
  })

  it('shows network error on fetch reject', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/connect|network|relay/i)
  })

  it('shows inline error and skips probe when token is empty', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('https://other.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/token.*required|token is required/i)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows inline error and skips probe when base is invalid', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(Relay)
    await wrapper.find('input[name="relay-base"]').setValue('not a url')
    await wrapper.find('input[name="relay-token"]').setValue('atk_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/invalid|malformed/i)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('disconnect clears config and redirects to setup', async () => {
    const wrapper = mount(Relay)
    await wrapper.find('[data-testid="disconnect"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toBeNull()
    expect(replaceMock).toHaveBeenCalledWith('/setup.html')
  })
})
