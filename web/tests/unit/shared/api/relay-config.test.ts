import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  isMobileApp,
  loadRelayConfig,
  saveRelayConfig,
  clearRelayConfig,
  validateRelayBase,
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

  it('saveRelayConfig persists the canonical shape and loadRelayConfig reads it back', () => {
    saveRelayConfig({
      baseURL: 'https://r.example.com',
      sessionToken: 'ses_test',
      expiresAt: 123,
      allowInsecure: false,
    })
    expect(loadRelayConfig()).toEqual({
      baseURL: 'https://r.example.com',
      sessionToken: 'ses_test',
      expiresAt: 123,
      allowInsecure: false,
    })
  })

  it('round-trips allowInsecure=true', () => {
    saveRelayConfig({
      baseURL: 'http://1.2.3.4:8080',
      sessionToken: 'ses_lan',
      expiresAt: null,
      allowInsecure: true,
    })
    expect(loadRelayConfig()).toEqual({
      baseURL: 'http://1.2.3.4:8080',
      sessionToken: 'ses_lan',
      expiresAt: null,
      allowInsecure: true,
    })
  })

  it('migrates legacy {base, token, allow_insecure} on read', () => {
    localStorage.setItem('atterm.relay', JSON.stringify({
      base: 'ws://localhost:8080',
      token: 'ses_old',
      allow_insecure: true,
    }))
    expect(loadRelayConfig()).toEqual({
      baseURL: 'ws://localhost:8080',
      sessionToken: 'ses_old',
      expiresAt: null,
      allowInsecure: true,
    })
  })

  it('migrates legacy {base, token, allowInsecure} camelCase on read', () => {
    // Some legacy writers used camelCase allowInsecure, not snake_case.
    localStorage.setItem('atterm.relay', JSON.stringify({
      base: 'https://r.example.com',
      token: 'atk_legacy',
      allowInsecure: false,
    }))
    expect(loadRelayConfig()).toEqual({
      baseURL: 'https://r.example.com',
      sessionToken: 'atk_legacy',
      expiresAt: null,
      allowInsecure: false,
    })
  })

  it('canonical fields win over legacy fields when both are present', () => {
    localStorage.setItem('atterm.relay', JSON.stringify({
      baseURL: 'https://canonical.example.com',
      base: 'https://legacy.example.com',
      sessionToken: 'ses_canonical',
      token: 'atk_legacy',
      expiresAt: 999,
      allowInsecure: true,
      allow_insecure: false,
    }))
    expect(loadRelayConfig()).toEqual({
      baseURL: 'https://canonical.example.com',
      sessionToken: 'ses_canonical',
      expiresAt: 999,
      allowInsecure: true,
    })
  })

  it('fills defaults when localStorage entry is an empty object', () => {
    localStorage.setItem('atterm.relay', JSON.stringify({}))
    expect(loadRelayConfig()).toEqual({
      baseURL: '',
      sessionToken: null,
      expiresAt: null,
      allowInsecure: false,
    })
  })

  it('loadRelayConfig returns null when stored JSON is malformed', () => {
    localStorage.setItem('atterm.relay', '{not json')
    expect(loadRelayConfig()).toBeNull()
  })

  it('clearRelayConfig removes the stored entry', () => {
    saveRelayConfig({
      baseURL: 'https://r.example.com',
      sessionToken: 'ses_x',
      expiresAt: null,
      allowInsecure: false,
    })
    clearRelayConfig()
    expect(loadRelayConfig()).toBeNull()
  })
})

describe('validateRelayBase', () => {
  it('accepts https with hostname', () => {
    expect(validateRelayBase('https://r.example.com', false)).toBeNull()
  })

  it('accepts https with port', () => {
    expect(validateRelayBase('https://r.example.com:8443', false)).toBeNull()
  })

  it('rejects wss scheme (use http(s) for base, ws scheme derives)', () => {
    expect(validateRelayBase('wss://r.example.com', false)).toMatch(/must start with http/i)
  })

  it('rejects ws scheme', () => {
    expect(validateRelayBase('ws://localhost:8080', true)).toMatch(/must start with http/i)
  })

  it('accepts http://localhost without insecure flag', () => {
    expect(validateRelayBase('http://localhost:8080', false)).toBeNull()
  })

  it('accepts http://127.0.0.1 without insecure flag', () => {
    expect(validateRelayBase('http://127.0.0.1:8080', false)).toBeNull()
  })

  it('accepts http://[::1] (IPv6 loopback) without insecure flag', () => {
    expect(validateRelayBase('http://[::1]:8080', false)).toBeNull()
  })

  it('rejects http to non-loopback host when allowInsecure is false', () => {
    const err = validateRelayBase('http://relay.example.com', false)
    expect(err).toMatch(/insecure/i)
  })

  it('accepts http to non-loopback host when allowInsecure is true', () => {
    expect(validateRelayBase('http://relay.example.com', true)).toBeNull()
  })

  it('rejects empty string', () => {
    expect(validateRelayBase('', false)).toMatch(/empty|required|missing/i)
  })

  it('rejects malformed URL', () => {
    expect(validateRelayBase('not a url', false)).toMatch(/invalid|malformed/i)
  })

  it('rejects URL with trailing path segment', () => {
    expect(validateRelayBase('https://r.example.com/api', false)).toMatch(/path/i)
  })

  it('accepts URL with trailing slash (treated as root path)', () => {
    expect(validateRelayBase('https://r.example.com/', false)).toBeNull()
  })

  it('rejects URL with query string', () => {
    expect(validateRelayBase('https://r.example.com?foo=1', false)).toMatch(/path|query|fragment/i)
  })

  it('rejects URL with fragment', () => {
    expect(validateRelayBase('https://r.example.com#x', false)).toMatch(/path|query|fragment/i)
  })

  it('accepts 127.0.0.2 (full 127.x.x.x loopback block)', () => {
    expect(validateRelayBase('http://127.0.0.2:8080', false)).toBeNull()
  })
})
