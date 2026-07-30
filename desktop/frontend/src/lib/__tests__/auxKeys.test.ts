import { describe, it, expect } from 'vitest'
import { DEFAULT_AUX_KEYS, effectiveAuxKeys, parseSeq, displaySeq } from '../auxKeys'

describe('effectiveAuxKeys', () => {
  it('returns persisted list when non-empty', async () => {
    const bridge = { load: async () => [{ id: 'x', label: 'x', seq: 'x' }] }
    expect(await effectiveAuxKeys(bridge)).toEqual([{ id: 'x', label: 'x', seq: 'x' }])
  })
  it('returns DEFAULT_AUX_KEYS when persisted list is empty', async () => {
    expect(await effectiveAuxKeys({ load: async () => [] })).toEqual(DEFAULT_AUX_KEYS)
  })
})

describe('DEFAULT_AUX_KEYS', () => {
  it('has stable aux- ids and unique ids', () => {
    for (const k of DEFAULT_AUX_KEYS) expect(k.id).toMatch(/^aux-/)
    expect(new Set(DEFAULT_AUX_KEYS.map((k) => k.id)).size).toBe(DEFAULT_AUX_KEYS.length)
  })
  it('enter sends CR, esc sends ESC, ctrl-c sends 0x03', () => {
    expect(DEFAULT_AUX_KEYS.find((k) => k.id === 'aux-enter')!.seq).toBe('\r')
    expect(DEFAULT_AUX_KEYS.find((k) => k.id === 'aux-esc')!.seq).toBe('\x1b')
    expect(DEFAULT_AUX_KEYS.find((k) => k.id === 'aux-ctrl-c')!.seq).toBe('\x03')
  })
  it('puts direction keys first for the narrow mobile shortcut bar', () => {
    expect(DEFAULT_AUX_KEYS.slice(0, 4).map((k) => k.id)).toEqual([
      'aux-up',
      'aux-down',
      'aux-left',
      'aux-right',
    ])
  })
})

describe('parseSeq', () => {
  it('decodes named escapes', () => {
    expect(parseSeq('\\r')).toBe('\r')
    expect(parseSeq('\\n')).toBe('\n')
    expect(parseSeq('\\t')).toBe('\t')
    expect(parseSeq('\\e')).toBe('\x1b')
    expect(parseSeq('\\\\')).toBe('\\')
  })
  it('decodes \\xNN hex bytes', () => {
    expect(parseSeq('\\x1b')).toBe('\x1b')
    expect(parseSeq('\\x03')).toBe('\x03')
    expect(parseSeq('\\x1b[A')).toBe('\x1b[A')
  })
  it('decodes ^X control notation', () => {
    expect(parseSeq('^C')).toBe('\x03')
    expect(parseSeq('^D')).toBe('\x04')
    expect(parseSeq('^[')).toBe('\x1b')
  })
  it('keeps plain text and unknown escapes literal', () => {
    expect(parseSeq('hello')).toBe('hello')
    expect(parseSeq('\\q')).toBe('\\q')
  })
})

describe('displaySeq', () => {
  it('encodes control bytes back to readable notation', () => {
    expect(displaySeq('\r')).toBe('\\r')
    expect(displaySeq('\x1b')).toBe('\\e')
    expect(displaySeq('\x03')).toBe('\\x03')
    expect(displaySeq('\x1b[A')).toBe('\\e[A')
    expect(displaySeq('\\')).toBe('\\\\')
  })
  it('round-trips with parseSeq for the default keys', () => {
    for (const k of DEFAULT_AUX_KEYS) {
      expect(parseSeq(displaySeq(k.seq))).toBe(k.seq)
    }
  })
})
