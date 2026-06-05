import { describe, it, expect, beforeEach } from 'vitest'
import { createInMemorySecureStorage, wrapLazyBackend, type SecureStorage } from '../secureStorage'

describe('createInMemorySecureStorage', () => {
  let s: ReturnType<typeof createInMemorySecureStorage>

  beforeEach(() => {
    s = createInMemorySecureStorage()
  })

  it('get returns null for unknown key', async () => {
    expect(await s.get('missing')).toBeNull()
  })

  it('set + get round-trip returns same value', async () => {
    await s.set('k', 'v')
    expect(await s.get('k')).toBe('v')
  })

  it('set twice updates the value (upsert)', async () => {
    await s.set('k', 'v1')
    await s.set('k', 'v2')
    expect(await s.get('k')).toBe('v2')
  })

  it('remove deletes the value', async () => {
    await s.set('k', 'v')
    await s.remove('k')
    expect(await s.get('k')).toBeNull()
  })

  it('remove of unknown key does not throw', async () => {
    await expect(s.remove('missing')).resolves.toBeUndefined()
  })

  it('keys are independent', async () => {
    await s.set('a', '1')
    await s.set('b', '2')
    expect(await s.get('a')).toBe('1')
    expect(await s.get('b')).toBe('2')
  })
})

describe('wrapLazyBackend', () => {
  it('memoizes backend selection (select runs once across calls)', async () => {
    let selects = 0
    const backend = createInMemorySecureStorage()
    const s = wrapLazyBackend(async () => { selects++; return backend })
    await s.set('k', 'v')
    await s.get('k')
    await s.remove('k')
    expect(selects).toBe(1)
  })

  it('set propagates a backend write failure instead of resolving as success', async () => {
    const failing: SecureStorage = {
      set: async () => { throw new Error('KEYCHAIN_ERROR') },
      get: async () => null,
      remove: async () => {},
    }
    const s = wrapLazyBackend(async () => failing)
    // Regression: a dropped inner promise would let this resolve as success.
    await expect(s.set('k', 'v')).rejects.toThrow('KEYCHAIN_ERROR')
  })

  it('set resolves only after the backend write actually completes', async () => {
    let written = false
    const slow: SecureStorage = {
      set: async () => { await Promise.resolve(); written = true },
      get: async () => null,
      remove: async () => {},
    }
    const s = wrapLazyBackend(async () => slow)
    await s.set('k', 'v')
    // Regression: fire-and-forget would let the await return before written flips.
    expect(written).toBe(true)
  })

  it('get returns the backend value through the wrapper', async () => {
    const backend = createInMemorySecureStorage()
    await backend.set('k', 'persisted')
    const s = wrapLazyBackend(async () => backend)
    expect(await s.get('k')).toBe('persisted')
  })
})
