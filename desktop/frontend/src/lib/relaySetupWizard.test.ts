import { describe, expect, it } from 'vitest'
import {
  classifyRelaySetupError,
  relayHttpToWsURL,
  validateRelaySetupInputs,
} from './relaySetupWizard'

describe('relay setup wizard helpers', () => {
  it('validates relay URL and API token inputs before connecting', () => {
    expect(validateRelaySetupInputs('', 'atk_x', false)?.code).toBe('relay_unreachable')
    expect(validateRelaySetupInputs('ftp://relay.example.com', 'atk_x', false)?.code).toBe('incompatible_relay_version')
    expect(validateRelaySetupInputs('http://relay.example.com', 'atk_x', false)?.code).toBe('insecure_ws_blocked')
    expect(validateRelaySetupInputs('https://relay.example.com', 'bad', false)?.code).toBe('invalid_token')
    expect(validateRelaySetupInputs('https://relay.example.com', 'atk_x', false)).toBeNull()
  })

  it('derives compatible websocket URLs from http and https relay URLs', () => {
    expect(relayHttpToWsURL('https://relay.example.com')).toBe('wss://relay.example.com')
    expect(relayHttpToWsURL('http://127.0.0.1:8080')).toBe('ws://127.0.0.1:8080')
  })

  it('classifies known connection failures with recovery actions', () => {
    expect(classifyRelaySetupError(new Error('401 invalid token')).code).toBe('invalid_token')
    expect(classifyRelaySetupError(new Error('origin rejected by relay')).code).toBe('origin_rejected')
    expect(classifyRelaySetupError(new Error('insecure ws blocked')).code).toBe('insecure_ws_blocked')
    expect(classifyRelaySetupError(new Error('incompatible relay version')).code).toBe('incompatible_relay_version')
    expect(classifyRelaySetupError(new Error('403 permission denied')).code).toBe('permission_denied')
    expect(classifyRelaySetupError(new Error('fetch failed')).code).toBe('relay_unreachable')
    expect(classifyRelaySetupError(new Error('uplink not connected')).code).toBe('uplink_not_connected')
  })
})
