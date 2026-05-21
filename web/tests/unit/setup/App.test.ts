import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import {
  loadRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import App from '@/setup/App.vue'

function makeResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('setup/App.vue', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    originalLocation = window.location
    replaceMock = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock, search: '', pathname: '/setup.html' },
      writable: true,
    })
    vi.restoreAllMocks()
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('renders the three required inputs', () => {
    const wrapper = mount(App)
    expect(wrapper.find('input[name="relay-base"]').exists()).toBe(true)
    expect(wrapper.find('input[name="relay-token"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="connect"]').exists()).toBe(true)
  })

  it('shows an inline error when base is invalid', async () => {
    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('not a url')
    await wrapper.find('input[name="relay-token"]').setValue('atk_test')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/invalid|malformed/i)
  })

  it('shows an inline error when http to non-loopback without insecure switch', async () => {
    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('http://relay.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_test')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toMatch(/insecure/i)
  })

  it('on successful probe saves config and replaces location to /', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(200, { user_id: 'u1', email: 'e@x' }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_good')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalled()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/me')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_good')
    expect((init as RequestInit).credentials).toBe('omit')

    expect(loadRelayConfig()).toEqual({
      base: 'https://r.example.com',
      token: 'atk_good',
      allowInsecure: false,
    })
    expect(replaceMock).toHaveBeenCalledWith('/')
  })

  it('shows token-invalid error on 401', async () => {
    const fetchMock = vi.fn().mockResolvedValue(makeResponse(401, { error: 'unauthenticated' }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_bad')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/token|invalid/i)
    expect(loadRelayConfig()).toBeNull()
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('shows network error on fetch reject', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_t')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/connect|network|relay/i)
    expect(loadRelayConfig()).toBeNull()
  })

  it('shows origin-rejected error on 403', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(makeResponse(403, { error: 'origin_rejected' })))

    const wrapper = mount(App)
    await wrapper.find('input[name="relay-base"]').setValue('https://r.example.com')
    await wrapper.find('input[name="relay-token"]').setValue('atk_t')
    await wrapper.find('[data-testid="connect"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toMatch(/origin|ATTERM_ORIGINS|capacitor/i)
  })

  it('shows token-invalid banner when ?reason=token_invalid in URL', () => {
    Object.defineProperty(window, 'location', {
      value: { ...originalLocation, replace: replaceMock, search: '?reason=token_invalid', pathname: '/setup.html' },
      writable: true,
    })
    const wrapper = mount(App)
    expect(wrapper.text()).toMatch(/token.*invalid|sign.*in.*again/i)
  })
})
