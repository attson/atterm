import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { saveRelayConfig, clearRelayConfig, __resetMobileDetectionCache } from '@shared/api/relay-config'
import { wsUrl } from '@shared/ws/client-conn'

describe('wsUrl browser mode', () => {
  let originalLocation: Location

  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    delete (globalThis as any).Capacitor
    originalLocation = window.location
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { value: originalLocation, writable: true })
  })

  it('uses ws:// when location.protocol is http:', () => {
    Object.defineProperty(window, 'location', {
      value: { protocol: 'http:', host: 'example.com:5173' },
      writable: true,
    })
    expect(wsUrl('/client')).toBe('ws://example.com:5173/client')
  })

  it('uses wss:// when location.protocol is https:', () => {
    Object.defineProperty(window, 'location', {
      value: { protocol: 'https:', host: 'example.com' },
      writable: true,
    })
    expect(wsUrl('/client')).toBe('wss://example.com/client')
  })
})

describe('wsUrl mobile mode', () => {
  beforeEach(() => {
    clearRelayConfig()
    __resetMobileDetectionCache()
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
  })

  afterEach(() => {
    delete (globalThis as any).Capacitor
    __resetMobileDetectionCache()
  })

  it('derives ws:// from http:// base', () => {
    saveRelayConfig({ base: 'http://1.2.3.4:8080', token: 'atk_t', allowInsecure: true })
    expect(wsUrl('/client')).toBe('ws://1.2.3.4:8080/client')
  })

  it('derives wss:// from https:// base', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_t', allowInsecure: false })
    expect(wsUrl('/client')).toBe('wss://r.example.com/client')
  })

  it('preserves the port from the base URL', () => {
    saveRelayConfig({ base: 'https://r.example.com:8443', token: 'atk_t', allowInsecure: false })
    expect(wsUrl('/client')).toBe('wss://r.example.com:8443/client')
  })

  it('throws ApiError with code relay_not_configured when no config is stored', () => {
    let err: unknown
    try { wsUrl('/client') } catch (e) { err = e }
    expect(err).toMatchObject({ status: 0, code: 'relay_not_configured' })
  })
})
