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
  localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
}

export function clearRelayConfig(): void {
  localStorage.removeItem(STORAGE_KEY)
}
