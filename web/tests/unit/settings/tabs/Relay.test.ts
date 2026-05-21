import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import {
  saveRelayConfig,
  loadRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'
import Relay from '@/settings/tabs/Relay.vue'

function makeResponse(status: number, body: unknown = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('settings/tabs/Relay.vue', () => {
  let originalLocation: Location
  let replaceMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    __resetMobileDetectionCache()
    clearRelayConfig()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_existing', allowInsecure: false })
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
    await wrapper.find('input[name="relay-token"]').setValue('atk_new')
    await wrapper.find('[data-testid="save"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toMatchObject({ base: 'https://other.example.com', token: 'atk_new' })
  })

  it('disconnect clears config and redirects to setup', async () => {
    const wrapper = mount(Relay)
    await wrapper.find('[data-testid="disconnect"]').trigger('click')
    await flushPromises()
    expect(loadRelayConfig()).toBeNull()
    expect(replaceMock).toHaveBeenCalledWith('/setup.html')
  })
})
