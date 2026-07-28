import { isMobileApp, loadRelayConfig } from './api/relay-config'

export type EntryPage = 'home' | 'login' | 'signup' | 'setup' | 'firstrun'

// applyMobileEntryGuard inspects the Capacitor environment and stored
// relay config, then issues a redirect when the current page would be
// unusable. Returns true when a redirect was issued — callers should
// `return` and skip mounting their Vue app.
export function applyMobileEntryGuard(page: EntryPage): boolean {
  if (!isMobileApp()) return false

  const hasConfig = loadRelayConfig() !== null

  if (!hasConfig) {
    if (page === 'setup') return false
    location.replace('/setup.html')
    return true
  }

  // hasConfig is true here.
  if (page === 'setup' || page === 'login' || page === 'signup' || page === 'firstrun') {
    location.replace('/')
    return true
  }

  return false
}
