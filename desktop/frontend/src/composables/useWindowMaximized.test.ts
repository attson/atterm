import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createFakePlatform } from '../platform/__tests__/_fakePlatform'

beforeEach(() => {
  vi.resetModules()
})

afterEach(() => {
  vi.resetModules()
})

describe('useWindowMaximized', () => {
  it('initializes to false and asynchronously updates from platform', async () => {
    const platform = createFakePlatform()
    platform.system.windowIsMaximized = vi.fn().mockResolvedValue(false)

    // After resetModules, dynamically import platform to set it in the same
    // module instance that the composable will resolve via usePlatform().
    const { __setPlatformForTests } = await import('../platform')
    __setPlatformForTests(platform)

    const mod = await import('./useWindowMaximized')
    const ref = mod.useWindowMaximized()
    expect(ref.value).toBe(false)
    await Promise.resolve()
    await Promise.resolve()
    expect(platform.system.windowIsMaximized).toHaveBeenCalledOnce()

    __setPlatformForTests(null)
  })

  it('setMaximized flips the ref synchronously', async () => {
    const { __setPlatformForTests } = await import('../platform')
    __setPlatformForTests(createFakePlatform())

    const mod = await import('./useWindowMaximized')
    const ref = mod.useWindowMaximized()
    mod.setMaximized(true)
    expect(ref.value).toBe(true)

    __setPlatformForTests(null)
  })
})
