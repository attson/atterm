import { describe, it, expect } from 'vitest'
import { parseSessionRoute, formatSessionRoute } from '@/main/lib/sessionRoute'

describe('parseSessionRoute', () => {
  it('returns null when the hash is empty', () => {
    expect(parseSessionRoute('')).toBeNull()
    expect(parseSessionRoute('#')).toBeNull()
  })

  it('returns null for hashes that do not match #/s/<uuid>', () => {
    expect(parseSessionRoute('#/foo')).toBeNull()
    expect(parseSessionRoute('#s/abc')).toBeNull()
    expect(parseSessionRoute('#/s/')).toBeNull()
    expect(parseSessionRoute('#/s/not-a-uuid')).toBeNull()
  })

  it('returns the uuid for #/s/<uuid>', () => {
    const uuid = '11111111-2222-3333-4444-555555555555'
    expect(parseSessionRoute('#/s/' + uuid)).toBe(uuid)
  })

  it('is case-insensitive on the uuid hex', () => {
    const uuid = 'AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE'
    expect(parseSessionRoute('#/s/' + uuid)).toBe(uuid.toLowerCase())
  })
})

describe('formatSessionRoute', () => {
  it('produces a hash that round-trips through parseSessionRoute', () => {
    const uuid = '11111111-2222-3333-4444-555555555555'
    const hash = formatSessionRoute(uuid)
    expect(hash).toBe('#/s/' + uuid)
    expect(parseSessionRoute(hash)).toBe(uuid)
  })
})
