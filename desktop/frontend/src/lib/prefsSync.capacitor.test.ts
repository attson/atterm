import { describe, it, expect, beforeEach, vi } from 'vitest'
import { localStorageAdapter, capacitorRelayClient, setSharedPrefsSync, notifyLocalChange } from './prefsSync.capacitor'
import { PrefsSyncEngine } from './prefsSync'

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

describe('notifyLocalChange', () => {
  it('marks dirty and schedules a push', () => {
    const fakeRelay = { get: vi.fn(), put: vi.fn().mockResolvedValue([]) }
    const e = new PrefsSyncEngine(localStorageAdapter(), fakeRelay)
    localStorage.setItem('atterm.locale_preference.value', `"zh-CN"`)
    setSharedPrefsSync(e)
    notifyLocalChange('locale_preference')
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.meta') ?? '{}').dirty)
      .toBe(true)
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

  it('reads the current Capacitor relay config shape', async () => {
    localStorage.setItem('atterm.relay.session', JSON.stringify({
      url: 'https://r.example.com',
      token: 'tok-current',
      session_expires_at: 0,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: '',
      connected: false,
    }))
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [{ key: 'pinned_session_ids', value: ['sid-web'], updated_at: 10 }] }), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = capacitorRelayClient()
    const out = await client.get()
    expect(out).toEqual([{ key: 'pinned_session_ids', value: ['sid-web'], updated_at: 10 }])
    const call = fetchMock.mock.calls[0]
    expect(call[0]).toBe('https://r.example.com/api/me/preferences')
    expect((call[1] as RequestInit).headers).toMatchObject({ Authorization: 'Bearer tok-current' })
  })
})
