import { describe, it, expect, beforeEach, vi } from 'vitest'
import { localStorageAdapter, capacitorRelayClient } from './prefsSync.capacitor'

beforeEach(() => {
  localStorage.clear()
})

describe('localStorageAdapter', () => {
  it('persists value and meta under namespaced keys', () => {
    const a = localStorageAdapter()
    a.writeValue('locale_preference', 'zh-CN')
    a.writeMeta('locale_preference', { updatedAtLocal: 123, dirty: true })

    expect(localStorage.getItem('atterm.locale_preference.value')).toBe(`"zh-CN"`)
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.meta') ?? '{}'))
      .toEqual({ updatedAtLocal: 123, dirty: true })

    expect(a.readValue('locale_preference')).toBe('zh-CN')
    expect(a.readMeta('locale_preference')).toEqual({ updatedAtLocal: 123, dirty: true })
  })
})

describe('capacitorRelayClient', () => {
  it('sends Authorization: Bearer with the stored session token', async () => {
    localStorage.setItem('atterm.relay.session', JSON.stringify({
      baseURL: 'https://r.example.com',
      sessionToken: 'tok-xyz',
      expiresAt: 0,
      allowInsecure: false,
    }))
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = capacitorRelayClient()
    const out = await client.get()
    expect(out).toEqual([])
    const call = fetchMock.mock.calls[0]
    expect(call[0]).toBe('https://r.example.com/api/me/preferences')
    expect((call[1] as RequestInit).headers).toMatchObject({ Authorization: 'Bearer tok-xyz' })
  })
})
