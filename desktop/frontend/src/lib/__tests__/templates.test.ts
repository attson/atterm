import { describe, it, expect } from 'vitest'
import { DEFAULT_TEMPLATES, effectiveTemplates } from '../templates'

describe('DEFAULT_TEMPLATES', () => {
  it('exposes the 10 starter entries', () => {
    expect(DEFAULT_TEMPLATES).toHaveLength(10)
  })

  it('has stable default- IDs', () => {
    for (const t of DEFAULT_TEMPLATES) {
      expect(t.id).toMatch(/^default-/)
    }
  })

  it('has unique IDs', () => {
    const ids = new Set(DEFAULT_TEMPLATES.map(t => t.id))
    expect(ids.size).toBe(DEFAULT_TEMPLATES.length)
  })

  it('includes the canonical AI tokens', () => {
    const texts = DEFAULT_TEMPLATES.map(t => t.text)
    for (const expected of ['y', 'n', 'yes', 'no', 'continue', 'approve', 'deny', 'retry', '/test', '/diff']) {
      expect(texts).toContain(expected)
    }
  })
})

describe('effectiveTemplates', () => {
  it('returns persisted list when non-empty', async () => {
    const bridge = { load: async () => [{ id: 'x', label: 'x', text: 'x' }] }
    const got = await effectiveTemplates(bridge)
    expect(got).toEqual([{ id: 'x', label: 'x', text: 'x' }])
  })

  it('returns DEFAULT_TEMPLATES when persisted list is empty', async () => {
    const bridge = { load: async () => [] }
    expect(await effectiveTemplates(bridge)).toBe(DEFAULT_TEMPLATES)
  })
})
