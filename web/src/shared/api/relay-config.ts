import { t } from '@shared/i18n'

export interface RelayConfig {
  base: string
  token: string
  allowInsecure: boolean
}

const STORAGE_KEY = 'atterm.relay'

let cachedMobile: boolean | null = null

// Test-only escape hatch; not exported via index.
export function __resetMobileDetectionCache(): void {
  cachedMobile = null
}

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
    if (
      typeof parsed.base !== 'string' ||
      typeof parsed.token !== 'string' ||
      typeof parsed.allowInsecure !== 'boolean'
    ) {
      return null
    }
    return { base: parsed.base, token: parsed.token, allowInsecure: parsed.allowInsecure }
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
