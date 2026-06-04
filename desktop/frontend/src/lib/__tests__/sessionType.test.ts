import { describe, it, expect } from 'vitest'
import { displayForType } from '../sessionType'

describe('displayForType', () => {
  it('returns null for shell / undefined / empty / unknown', () => {
    expect(displayForType(undefined)).toBeNull()
    expect(displayForType('')).toBeNull()
    expect(displayForType('shell')).toBeNull()
    expect(displayForType('something-weird')).toBeNull()
  })

  it.each(['ai', 'test', 'build', 'deploy'] as const)('returns key+color+iconPath for %s', (t) => {
    const d = displayForType(t)
    expect(d).not.toBeNull()
    expect(d!.key).toBe(t)
    expect(d!.color).toMatch(/^#[0-9a-f]{6}$/i)
    expect(d!.iconPath.length).toBeGreaterThan(0)
  })
})
