import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  isMobileApp,
  loadRelayConfig,
  saveRelayConfig,
  clearRelayConfig,
  __resetMobileDetectionCache,
} from '@shared/api/relay-config'

describe('isMobileApp', () => {
  beforeEach(() => {
    __resetMobileDetectionCache()
    delete (globalThis as any).Capacitor
  })

  it('returns false when Capacitor global is absent', () => {
    expect(isMobileApp()).toBe(false)
  })

  it('returns false when Capacitor.isNativePlatform is missing', () => {
    ;(globalThis as any).Capacitor = {}
    expect(isMobileApp()).toBe(false)
  })

  it('returns false when isNativePlatform() returns false', () => {
    ;(globalThis as any).Capacitor = { isNativePlatform: () => false }
    expect(isMobileApp()).toBe(false)
  })

  it('returns true when isNativePlatform() returns true', () => {
    ;(globalThis as any).Capacitor = { isNativePlatform: () => true }
    expect(isMobileApp()).toBe(true)
  })

  it('caches the result after the first call', () => {
    const stub = vi.fn().mockReturnValue(true)
    ;(globalThis as any).Capacitor = { isNativePlatform: stub }
    isMobileApp()
    isMobileApp()
    isMobileApp()
    expect(stub).toHaveBeenCalledTimes(1)
  })
})

describe('relay config storage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('loadRelayConfig returns null when nothing is stored', () => {
    expect(loadRelayConfig()).toBeNull()
  })

  it('saveRelayConfig persists fields and loadRelayConfig reads them back', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_test', allowInsecure: false })
    expect(loadRelayConfig()).toEqual({
      base: 'https://r.example.com',
      token: 'atk_test',
      allowInsecure: false,
    })
  })

  it('loadRelayConfig returns null when stored JSON is malformed', () => {
    localStorage.setItem('atterm.relay', '{not json')
    expect(loadRelayConfig()).toBeNull()
  })

  it('loadRelayConfig returns null when stored config is missing required fields', () => {
    localStorage.setItem('atterm.relay', JSON.stringify({ base: 'https://r.example.com' }))
    expect(loadRelayConfig()).toBeNull()
  })

  it('clearRelayConfig removes the stored entry', () => {
    saveRelayConfig({ base: 'https://r.example.com', token: 'atk_x', allowInsecure: false })
    clearRelayConfig()
    expect(loadRelayConfig()).toBeNull()
  })
})
