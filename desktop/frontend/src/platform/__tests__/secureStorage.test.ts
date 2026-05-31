import { describe, it, expect, beforeEach } from 'vitest'
import { createInMemorySecureStorage } from '../secureStorage'

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
