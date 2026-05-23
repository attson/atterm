import type { Platform } from './types'

export type { Platform } from './types'
export * from './types'

let _platform: Platform | null = null

// Factory injection: main.ts passes createWailsPlatform (desktop) or
// createCapacitorPlatform (PR-B). index.ts stays unaware of either, so
// Vite can tree-shake the unused branch per build target.
export function initPlatform(factory: () => Platform): Platform {
  if (_platform) return _platform
  const p = factory()
  _platform = p
  return p
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
