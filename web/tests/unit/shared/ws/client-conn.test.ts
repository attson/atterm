import { describe, it, expect, beforeEach } from 'vitest'
import { wsUrl } from '@shared/ws/client-conn'
import { saveRelayConfig, clearRelayConfig } from '@shared/api/relay-config'

describe('wsUrl home routing', () => {
  beforeEach(() => clearRelayConfig())

  it('routes to homeInstanceURL when set', () => {
    saveRelayConfig({ baseURL: 'https://relay.example', sessionToken: 't', expiresAt: null, allowInsecure: false, homeInstanceURL: 'https://node-1.example' })
    expect(wsUrl('/client')).toBe('wss://node-1.example/client')
  })

  it('falls back to baseURL/location when home unset', () => {
    saveRelayConfig({ baseURL: 'https://relay.example', sessionToken: 't', expiresAt: null, allowInsecure: false })
    // In happy-dom, location.host drives the non-mobile branch; assert it does NOT use a home node.
    expect(wsUrl('/client').endsWith('/client')).toBe(true)
    expect(wsUrl('/client')).not.toContain('node-1.example')
  })
})
