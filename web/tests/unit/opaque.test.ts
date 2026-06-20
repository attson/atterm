// Tests for the account-key AEAD wrap helpers (Argon2id + XChaCha20-Poly1305).
// These are NOT part of OPAQUE — the OPAQUE protocol now runs in the bytemare
// WASM client (@shared/lib/opaqueWasm), and cross-client interop is guarded by
// the relay-gated opaque-interop.test.ts and the Go-side
// internal/e2eeclient tests. The KSF golden vector moved to Go
// (TestOpaqueKSFGoldenVector) since the KSF now lives in bytemare.

import { describe, expect, it } from 'vitest'
import { defaultKDFParams, unwrapWithPassword, wrapAccountKey } from '@shared/lib/opaque'

const PASSWORD = 'hunter2'

describe('account-key wrap helpers', () => {
  it('wraps and unwraps a 32-byte account_key with the same password', () => {
    const accountKey = new Uint8Array(32)
    for (let i = 0; i < 32; i++) accountKey[i] = i

    const wrap = wrapAccountKey(PASSWORD, accountKey, defaultKDFParams())

    expect(wrap.method).toBe('password')
    expect(wrap.wrapped.length).toBeGreaterThan(0)
    expect(wrap.nonce.length).toBeGreaterThan(0)
    expect(wrap.salt.length).toBeGreaterThan(0)
    expect(wrap.kdf_params).toContain('argon2id')

    const recovered = unwrapWithPassword(PASSWORD, wrap)
    expect(recovered).toEqual(accountKey)
  })

  it('throws on wrong password', () => {
    const accountKey = new Uint8Array(32).fill(7)
    const wrap = wrapAccountKey(PASSWORD, accountKey, defaultKDFParams())
    expect(() => unwrapWithPassword('not-the-password', wrap)).toThrow(/invalid password/)
  })

  it('refuses non-32-byte input', () => {
    expect(() => wrapAccountKey(PASSWORD, new Uint8Array(31))).toThrow(/must be 32 bytes/)
  })

  it('produces different ciphertext on every call (random nonce + salt)', () => {
    const accountKey = new Uint8Array(32).fill(1)
    const a = wrapAccountKey(PASSWORD, accountKey)
    const b = wrapAccountKey(PASSWORD, accountKey)
    expect(a.wrapped).not.toBe(b.wrapped)
    expect(a.nonce).not.toBe(b.nonce)
    expect(a.salt).not.toBe(b.salt)
    // Both still decrypt back to the same key.
    expect(unwrapWithPassword(PASSWORD, a)).toEqual(accountKey)
    expect(unwrapWithPassword(PASSWORD, b)).toEqual(accountKey)
  })
})
