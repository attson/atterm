import { describe, expect, test } from 'vitest'
import { deriveServiceKeys } from './opaque'

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

describe('Remote Web Preview service key derivation', () => {
  test('matches the Go HKDF vector and separates directions', () => {
    const keys = deriveServiceKeys(
      new Uint8Array(32).fill(0x42),
      '11111111-2222-4333-8444-555555555555',
    )
    expect(hex(keys.clientToHost)).toBe(
      '4d1906ee9238a25afc1142217b979f22b362b8d25b10377e4b280fa289950e8b',
    )
    expect(hex(keys.hostToClient)).toBe(
      'd9647ac75246678dd15c7a614b13d82a70f479c9bde63d21a0dc59586313d95f',
    )
  })
})
