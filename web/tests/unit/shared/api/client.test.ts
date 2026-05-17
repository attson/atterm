import { describe, it, expect } from 'vitest'
import { safeNext } from '@shared/api/client'

describe('safeNext', () => {
  it('returns / when input is null', () => {
    expect(safeNext(null)).toBe('/')
  })

  it('returns / when input is empty', () => {
    expect(safeNext('')).toBe('/')
  })

  it('accepts a same-origin path', () => {
    expect(safeNext('/settings.html')).toBe('/settings.html')
  })

  it('preserves query and hash on same-origin paths', () => {
    expect(safeNext('/admin/?tab=users#row-7')).toBe('/admin/?tab=users#row-7')
  })

  it('rejects protocol-relative URLs', () => {
    expect(safeNext('//evil.example')).toBe('/')
  })

  it('rejects backslash quirk', () => {
    expect(safeNext('/\\evil.example')).toBe('/')
  })

  it('rejects absolute URLs to other origins', () => {
    expect(safeNext('https://evil.example/login')).toBe('/')
  })

  it('rejects javascript: URLs', () => {
    expect(safeNext('javascript:alert(1)')).toBe('/')
  })

  it('rejects non-leading-slash paths (relative)', () => {
    expect(safeNext('settings.html')).toBe('/')
  })
})
