export type RelaySetupErrorCode =
  | 'relay_unreachable'
  | 'invalid_token'
  | 'origin_rejected'
  | 'insecure_ws_blocked'
  | 'incompatible_relay_version'
  | 'permission_denied'
  | 'uplink_not_connected'

export interface RelaySetupIssue {
  code: RelaySetupErrorCode
  recoveryKey: string
}

const LOOPBACK_HOSTS = new Set(['localhost', '127.0.0.1', '::1', '[::1]'])

export function relayHttpToWsURL(rawURL: string): string {
  const u = new URL(rawURL)
  if (u.protocol === 'https:') u.protocol = 'wss:'
  else if (u.protocol === 'http:') u.protocol = 'ws:'
  return u.toString().replace(/\/$/, '')
}

export function validateRelaySetupInputs(url: string, token: string, allowInsecure: boolean): RelaySetupIssue | null {
  const trimmedURL = url.trim()
  if (!trimmedURL) return issue('relay_unreachable')
  let parsed: URL
  try {
    parsed = new URL(trimmedURL)
  } catch {
    return issue('relay_unreachable')
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    return issue('incompatible_relay_version')
  }
  if ((parsed.pathname && parsed.pathname !== '/') || parsed.search || parsed.hash) {
    return issue('incompatible_relay_version')
  }
  if (parsed.protocol === 'http:' && !allowInsecure && !LOOPBACK_HOSTS.has(parsed.hostname)) {
    return issue('insecure_ws_blocked')
  }
  if (!token.trim().startsWith('atk_')) return issue('invalid_token')
  return null
}

export function classifyRelaySetupError(err: unknown): RelaySetupIssue {
  const text = String(err instanceof Error ? err.message : err).toLowerCase()
  if (text.includes('401') || text.includes('invalid token') || text.includes('unauthorized')) return issue('invalid_token')
  if (text.includes('origin')) return issue('origin_rejected')
  if (text.includes('insecure') || text.includes('non-loopback') || text.includes('ws://')) return issue('insecure_ws_blocked')
  if (text.includes('incompatible') || text.includes('version') || text.includes('404')) return issue('incompatible_relay_version')
  if (text.includes('403') || text.includes('permission denied') || text.includes('forbidden')) return issue('permission_denied')
  if (text.includes('uplink') || text.includes('not connected')) return issue('uplink_not_connected')
  return issue('relay_unreachable')
}

function issue(code: RelaySetupErrorCode): RelaySetupIssue {
  return { code, recoveryKey: `settings.relay.wizard.recovery.${code}` }
}
