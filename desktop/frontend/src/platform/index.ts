import type { Platform } from './types'

export type { Platform } from './types'
export * from './types'

let _platform: Platform | null = null

export function initPlatform(): Platform {
  if (_platform) return _platform
  // VITE_TARGET selects implementation. The default 'wails' covers desktop;
  // 'capacitor' will be wired in PR-B.
  const target = (import.meta as { env?: { VITE_TARGET?: string } }).env?.VITE_TARGET ?? 'wails'
  if (target === 'capacitor') {
    throw new Error('platform: VITE_TARGET=capacitor not yet implemented (PR-B)')
  }
  // Lazy import so this module stays runnable in tests that use __setPlatformForTests
  // without triggering the wails impl (which imports from wailsjs/* and lib/api.ts).
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { createWailsPlatform } = require('./wails') as typeof import('./wails')
  _platform = createWailsPlatform()
  return _platform
}

export function usePlatform(): Platform {
  if (!_platform) {
    throw new Error('platform: call initPlatform() in main.ts before usePlatform()')
  }
  return _platform
}

// Test-only escape hatch.
export function __setPlatformForTests(p: Platform | null): void {
  _platform = p
}
