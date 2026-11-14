import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { PrefsSyncEngine, SYNCED_KEYS, localStorageAdapter, apiRelayClient } from '@shared/sync/prefsSync'

beforeEach(() => { localStorage.clear() })

// Reads internal/prefssync/sync.go::syncedKeys directly instead of comparing
// against a second hardcoded TS literal in this same file — that comparison
// can never detect drift from the actual Go source (2026-08-17 prefs-sync-l1
// final review, I3). SYNCED_KEYS here is intentionally a SUBSET of Go's list
// (see the comment on SYNCED_KEYS in web/src/shared/sync/prefsSync.ts for
// why desktop-only keys don't need to be mirrored here), so the check is
// subset-of, not equals. Path is process.cwd()-relative (vitest runs with
// cwd = web), matching the convention used elsewhere in this repo (e.g.
// tests/unit/setup/App.test.ts) rather than import.meta.url, which vitest
// does not always resolve to a file: URL.
function goSyncedKeys(): string[] {
  const goPath = resolve(process.cwd(), '../internal/prefssync/sync.go')
  const src = readFileSync(goPath, 'utf-8')
  const m = src.match(/var syncedKeys = \[\]string\{([\s\S]*?)\}/)
  if (!m) throw new Error(`could not find syncedKeys in ${goPath}`)
  return [...(m[1] ?? '').matchAll(/"([^"]+)"/g)].map((x) => x[1] ?? '')
}

describe('web prefsSync', () => {
  it('SYNCED_KEYS is a subset of the Go source of truth (internal/prefssync/sync.go)', () => {
    const goKeys = new Set(goSyncedKeys())
    expect(goKeys.size).toBeGreaterThan(0)
    for (const k of SYNCED_KEYS) {
      expect(goKeys.has(k), `SYNCED_KEYS has ${k} but internal/prefssync/sync.go::syncedKeys does not`).toBe(true)
    }
  })

  it('SYNCED_KEYS includes pinned_session_ids', () => {
    expect(SYNCED_KEYS).toContain('pinned_session_ids' as any)
  })

  it('SYNCED_KEYS includes ai_notifications_only', () => {
    expect(SYNCED_KEYS).toContain('ai_notifications_only' as any)
  })

  it('SYNCED_KEYS includes traffic_time_zone', () => {
    expect(SYNCED_KEYS).toContain('traffic_time_zone' as any)
  })

  it('SYNCED_KEYS includes snippets', () => {
    expect(SYNCED_KEYS).toContain('snippets' as any)
  })

  it('isolates snippets from established dirty preferences', async () => {
    const a = localStorageAdapter()
    a.writeValue('quick_templates', [{ id: 'ok', label: 'ok', text: 'ok' }])
    a.writeMeta('quick_templates', { updatedAtLocal: 100, dirty: true })
    a.writeValue('snippets', [{ id: 'deploy', label: 'Deploy', text: 'make deploy' }])
    a.writeMeta('snippets', { updatedAtLocal: 200, dirty: true })
    const put = vi.fn(async (items: Array<{ key: string; value: unknown; client_updated_at: number }>) => {
      if (items.some((item) => item.key === 'snippets')) throw new Error('unknown_key')
      return items.map((item) => ({ key: item.key, value: item.value, updated_at: item.client_updated_at + 1 }))
    })

    await expect(new PrefsSyncEngine(a, { get: vi.fn(), put }).push()).rejects.toThrow('unknown_key')
    expect(put).toHaveBeenCalledTimes(2)
    expect(a.readMeta('quick_templates')).toEqual({ updatedAtLocal: 101, dirty: false })
    expect(a.readMeta('snippets')).toEqual({ updatedAtLocal: 200, dirty: true })
  })

  it('does not overwrite an established preference edited during the snippets PUT', async () => {
    const a = localStorageAdapter()
    a.writeValue('quick_templates', [{ id: 'old', label: 'Old', text: 'old' }])
    a.writeMeta('quick_templates', { updatedAtLocal: 100, dirty: true })
    a.writeValue('snippets', [{ id: 'deploy', label: 'Deploy', text: 'make deploy' }])
    a.writeMeta('snippets', { updatedAtLocal: 200, dirty: true })
    const put = vi.fn(async (items: Array<{ key: string; value: unknown; client_updated_at: number }>) => {
      if (items[0]?.key === 'snippets') {
        a.writeValue('quick_templates', [{ id: 'new', label: 'New', text: 'new' }])
        a.writeMeta('quick_templates', { updatedAtLocal: 300, dirty: true })
        return [
          { key: 'quick_templates', value: [{ id: 'old', label: 'Old', text: 'old' }], updated_at: 101 },
          { key: 'snippets', value: items[0].value, updated_at: 201 },
        ]
      }
      return items.map((item) => ({ key: item.key, value: item.value, updated_at: item.client_updated_at + 1 }))
    })

    await new PrefsSyncEngine(a, { get: vi.fn(), put }).push()

    expect(a.readValue('quick_templates')).toEqual([{ id: 'new', label: 'New', text: 'new' }])
    expect(a.readMeta('quick_templates')).toEqual({ updatedAtLocal: 300, dirty: true })
  })

  it('ignores an unsent preference returned by an established PUT', async () => {
    const a = localStorageAdapter()
    a.writeValue('quick_templates', [{ id: 'quick', label: 'Quick', text: 'quick' }])
    a.writeMeta('quick_templates', { updatedAtLocal: 100, dirty: true })
    a.writeValue('notifications_enabled', false)
    a.writeMeta('notifications_enabled', { updatedAtLocal: 50, dirty: false })
    const put = vi.fn(async (items: Array<{ key: string; value: unknown; client_updated_at: number }>) => {
      a.writeValue('notifications_enabled', true)
      a.writeMeta('notifications_enabled', { updatedAtLocal: 300, dirty: true })
      return [
        { key: 'quick_templates', value: items[0]!.value, updated_at: 101 },
        { key: 'notifications_enabled', value: false, updated_at: 51 },
      ]
    })

    await new PrefsSyncEngine(a, { get: vi.fn(), put }).push()

    expect(a.readValue('notifications_enabled')).toBe(true)
    expect(a.readMeta('notifications_enabled')).toEqual({ updatedAtLocal: 300, dirty: true })
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

  it.each(['pull', 'push'] as const)('%s ignores retired keys and only pull adopts pull-only preferences', async (operation) => {
    const a = localStorageAdapter()
    a.writeValue('locale_preference', 'en')
    a.writeMeta('locale_preference', { updatedAtLocal: 100, dirty: operation === 'push' })
    a.writeValue('ssh_hosts_encrypted', 'old-ssh-hosts')
    a.writeMeta('ssh_hosts_encrypted', { updatedAtLocal: 100, dirty: operation === 'push' })
    const access = [
      vi.spyOn(a, 'readValue'), vi.spyOn(a, 'readMeta'),
      vi.spyOn(a, 'writeValue'), vi.spyOn(a, 'writeMeta'),
    ]
    const items = [
      { key: 'profiles_encrypted', value: 'retired-profile-data', updated_at: 500 },
      { key: 'locale_preference', value: 'zh-CN', updated_at: 500 },
      { key: 'ssh_hosts_encrypted', value: 'new-ssh-hosts', updated_at: 500 },
    ]
    const r = { get: vi.fn().mockResolvedValue(items), put: vi.fn().mockResolvedValue(items) }

    await new PrefsSyncEngine(a, r)[operation]()

    expect(localStorage.getItem('atterm.profiles_encrypted.value')).toBeNull()
    expect(localStorage.getItem('atterm.profiles_encrypted.meta')).toBeNull()
    for (const method of access) {
      expect(method.mock.calls.some(([key]) => key === 'profiles_encrypted')).toBe(false)
    }
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.value') ?? 'null')).toBe('zh-CN')
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.meta') ?? 'null')).toEqual({ updatedAtLocal: 500, dirty: false })
    if (operation === 'push') {
      expect(JSON.parse(localStorage.getItem('atterm.ssh_hosts_encrypted.value') ?? 'null')).toBe('old-ssh-hosts')
      expect(JSON.parse(localStorage.getItem('atterm.ssh_hosts_encrypted.meta') ?? 'null')).toEqual({ updatedAtLocal: 100, dirty: true })
      expect(r.put).toHaveBeenCalledWith([
        { key: 'locale_preference', value: 'en', client_updated_at: 100 },
      ])
    } else {
      expect(JSON.parse(localStorage.getItem('atterm.ssh_hosts_encrypted.value') ?? 'null')).toBe('new-ssh-hosts')
      expect(JSON.parse(localStorage.getItem('atterm.ssh_hosts_encrypted.meta') ?? 'null')).toEqual({ updatedAtLocal: 500, dirty: false })
    }
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
