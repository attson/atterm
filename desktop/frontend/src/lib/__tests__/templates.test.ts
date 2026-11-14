import { describe, it, expect } from 'vitest'
import { DEFAULT_TEMPLATES, effectiveTemplates, snippetCollectionJSONBytes } from '../templates'

describe('DEFAULT_TEMPLATES', () => {
  it('exposes the 9 starter entries', () => {
    expect(DEFAULT_TEMPLATES).toHaveLength(9)
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

  it('includes the canonical tokens', () => {
    const texts = DEFAULT_TEMPLATES.map(t => t.text)
    for (const expected of ['yes', 'ok', 'continue', 'commit', 'push', 'release', '1', '2', '3']) {
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

describe('snippetCollectionJSONBytes', () => {
  it('matches Go JSON escaping for shell redirection characters', () => {
    const snippets = Array.from({ length: 3 }, (_, index) => ({
      id: String(index),
      label: 'Redirect',
      text: '<'.repeat(64 * 1024),
    }))

    expect(new TextEncoder().encode(JSON.stringify(snippets)).byteLength).toBeLessThan(1024 * 1024)
    expect(snippetCollectionJSONBytes(snippets)).toBeGreaterThan(1024 * 1024)
  })
})
