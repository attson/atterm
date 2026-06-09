import { t } from '@shared/i18n'

// RelayConfig — single source of truth for "how do we reach the relay
// and prove we're allowed to". Phase 4 of the session-token migration
// added the canonical sessionToken + expiresAt + baseURL fields; the
// legacy base/token/allowInsecure shape is still accepted so the mobile
// setup/settings UI keeps compiling. Phase 5 finishes the mobile sweep.
export interface RelayConfig {
  // Canonical fields (session-token era).
  baseURL?: string
  sessionToken?: string | null
  expiresAt?: number | null

  // Legacy fields kept until Phase 5 mobile UI rewrite.
  base?: string
  token?: string
  allowInsecure?: boolean
}

const STORAGE_KEY = 'atterm.relay'

let cachedMobile: boolean | null = null

// Test-only escape hatch; not exported via index.
export function __resetMobileDetectionCache(): void {
  cachedMobile = null
}

// isMobileApp is retained for legacy callers (mobile-guard, settings UI,
// WS subprotocol). The session-token migration removes the apiFetch
// branch on this but other entry-point gates still depend on it.
export function isMobileApp(): boolean {
  if (cachedMobile !== null) return cachedMobile
  const cap = (globalThis as { Capacitor?: { isNativePlatform?: () => boolean } }).Capacitor
  cachedMobile = !!(cap && typeof cap.isNativePlatform === 'function' && cap.isNativePlatform())
  return cachedMobile
}

export function loadRelayConfig(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as Partial<RelayConfig>
    if (typeof parsed !== 'object' || parsed === null) return null
    return parsed as RelayConfig
  } catch {
    return null
  }
}

export function saveRelayConfig(cfg: RelayConfig): void {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
}

export function clearRelayConfig(): void {
  if (typeof localStorage === 'undefined') return
  localStorage.removeItem(STORAGE_KEY)
}

export function validateRelayBase(base: string, allowInsecure: boolean): string | null {
  const trimmed = base.trim()
  if (!trimmed) return t('setup.errors.relayUrlRequired')
  let u: URL
  try {
    u = new URL(trimmed)
  } catch {
    return t('setup.errors.invalidRelayUrl')
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') {
    return t('setup.errors.relayUrlMustBeHttp')
  }
  if (u.pathname !== '/' || u.search !== '' || u.hash !== '') {
    return t('setup.errors.relayUrlNoPath')
  }
  if (u.protocol === 'http:' && !isLoopbackHost(u.hostname) && !allowInsecure) {
    return t('setup.errors.useInsecureForHttp')
  }
  return null
}

function isLoopbackHost(host: string): boolean {
  const h = host.toLowerCase()
  if (h === 'localhost' || h === '127.0.0.1' || h === '::1') return true
  if (h.startsWith('127.')) return true
  // URL parses [::1] hostname as "[::1]"; strip brackets when present.
  if (h.startsWith('[') && h.endsWith(']')) {
    const inner = h.slice(1, -1)
    if (inner === '::1') return true
  }
  return false
}
