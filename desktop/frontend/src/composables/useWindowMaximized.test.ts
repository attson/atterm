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

  it('defaults to false when platform.system.windowIsMaximized rejects', async () => {
    const platform = createFakePlatform()
    platform.system.windowIsMaximized = vi.fn().mockRejectedValue(new Error('runtime gone'))
    const { __setPlatformForTests } = await import('../platform')
    __setPlatformForTests(platform)
    const mod = await import('./useWindowMaximized')
    const ref = mod.useWindowMaximized()
    // Drain microtasks for the rejected promise + .catch handler.
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    expect(ref.value).toBe(false)
  })

  it('returns the same ref instance on every call (module-level singleton)', async () => {
    const mod = await import('./useWindowMaximized')
    const a = mod.useWindowMaximized()
    const b = mod.useWindowMaximized()
    expect(a).toBe(b)
  })
})
