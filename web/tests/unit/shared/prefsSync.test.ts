import { describe, it, expect, vi, beforeEach } from 'vitest'
import { PrefsSyncEngine, SYNCED_KEYS, localStorageAdapter, apiRelayClient } from '@shared/sync/prefsSync'

beforeEach(() => { localStorage.clear() })

// Mirrors internal/prefssync/sync.go::syncedKeys. If Go grows a new key
// (or drops one), update BOTH this literal AND the SYNCED_KEYS list in
// desktop/frontend/src/lib/prefsSync.ts + web/src/shared/sync/prefsSync.ts.
const EXPECTED_SYNCED_KEYS = [
  'ai_notifications_only',
  'command_notify_threshold_seconds',
  'locale_preference',
  'notifications_enabled',
  'pinned_session_ids',
  'quick_templates',
  'shell_integration_enabled',
] as const

describe('web prefsSync', () => {
  it('SYNCED_KEYS matches the Go source of truth (internal/prefssync/sync.go)', () => {
    expect(SYNCED_KEYS.slice().sort()).toEqual([...EXPECTED_SYNCED_KEYS])
  })

  it('SYNCED_KEYS includes pinned_session_ids', () => {
    expect(SYNCED_KEYS).toContain('pinned_session_ids' as any)
  })

  it('SYNCED_KEYS includes ai_notifications_only', () => {
    expect(SYNCED_KEYS).toContain('ai_notifications_only' as any)
  })

  it('pull adopts server values', async () => {
    const a = localStorageAdapter()
    const fakeRelay = {
      get: vi.fn().mockResolvedValue([
        { key: 'locale_preference', value: 'zh-CN', updated_at: 1234 },
      ]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, fakeRelay)
    await e.pull()
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.value') ?? '""')).toBe('zh-CN')
  })

  it('push routes through apiFetch with Bearer token', async () => {
    localStorage.setItem('atterm.relay', JSON.stringify({
      baseURL: '', sessionToken: 'tok-1', expiresAt: 0,
    }))
    localStorage.setItem('atterm.locale_preference.value', `"en"`)
    localStorage.setItem('atterm.locale_preference.meta', JSON.stringify({ updatedAtLocal: 999, dirty: true }))
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [
        { key: 'locale_preference', value: 'en', updated_at: 999 },
      ]}), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const e = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
    await e.push()
    const call = fetchMock.mock.calls[0]
    if (!call) throw new Error('fetch was not called')
    expect(call[0]).toBe('/api/me/preferences')
    expect((call[1] as RequestInit).method).toBe('PUT')
    const headers = (call[1] as RequestInit).headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer tok-1')
  })
})
