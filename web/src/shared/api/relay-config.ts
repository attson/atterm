import { t } from '@shared/i18n'

// RelayConfig — single source of truth for "how do we reach the relay
// and prove we're allowed to". After Phase 4 of the session-token
// migration the legacy {base, token, allow_insecure} shape is gone;
// loadRelayConfig still silently migrates any stale localStorage entry
// on first read so users with an old shape on disk don't lose state.
export interface RelayConfig {
  baseURL: string
  sessionToken: string | null
  expiresAt: number | null
  allowInsecure: boolean
  // realmId is populated from the login finalize response realm_id field.
  // Used by subproject C for relay node selection. Not written by register
  // (register finalize does not return realm_id yet).
  realmId?: string
  // homeInstanceURL is the user's home relay node for this realm (from the
  // login response home_instance_url). The stateful /client WS routes here
  // when set; empty falls back to baseURL/location. Not written by register.
  homeInstanceURL?: string
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

// LegacyRelayConfig — the pre-session-token shape, kept solely so
// loadRelayConfig can migrate it on read. Never written, never exported.
interface LegacyRelayConfig {
  base?: string
  token?: string
  allow_insecure?: boolean
  allowInsecure?: boolean
}

export function loadRelayConfig(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as Partial<RelayConfig> & LegacyRelayConfig
    if (typeof parsed !== 'object' || parsed === null) return null
    return {
      baseURL: parsed.baseURL ?? parsed.base ?? '',
      sessionToken: parsed.sessionToken ?? parsed.token ?? null,
      expiresAt: parsed.expiresAt ?? null,
      allowInsecure: parsed.allowInsecure ?? parsed.allow_insecure ?? false,
      ...(parsed.realmId !== undefined ? { realmId: parsed.realmId } : {}),
      ...(parsed.homeInstanceURL !== undefined ? { homeInstanceURL: parsed.homeInstanceURL } : {}),
    }
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
