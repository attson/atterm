import { describe, it, expect, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { PrefsSyncEngine, type Adapter, type RelayClient, SYNCED_KEYS } from './prefsSync'

// Reads internal/prefssync/sync.go::syncedKeys directly instead of comparing
// against a second hardcoded TS literal — a literal-vs-literal comparison in
// this same file can never detect drift from the actual Go source (2026-08-17
// prefs-sync-l1 final review, I3). This file's SYNCED_KEYS is intentionally a
// SUBSET of Go's list (see the comment on SYNCED_KEYS in ./prefsSync.ts for
// why desktop-only keys don't need to be mirrored here), so the check is
// subset-of, not equals. Path is process.cwd()-relative (vitest runs with cwd
// = desktop/frontend), matching the convention used elsewhere in this repo
// (e.g. TaskSidebar.test.ts) rather than import.meta.url, which vitest does
// not always resolve to a file: URL.
function goSyncedKeys(): string[] {
  const goPath = resolve(process.cwd(), '../../internal/prefssync/sync.go')
  const src = readFileSync(goPath, 'utf-8')
  const m = src.match(/var syncedKeys = \[\]string\{([\s\S]*?)\}/)
  if (!m) throw new Error(`could not find syncedKeys in ${goPath}`)
  return [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1])
}

class FakeAdapter implements Adapter {
  values = new Map<string, unknown>()
  meta = new Map<string, { updatedAtLocal: number; dirty: boolean }>()
  readValue(k: string) { return this.values.has(k) ? this.values.get(k) : undefined }
  writeValue(k: string, v: unknown) { this.values.set(k, v) }
  readMeta(k: string) { return this.meta.get(k) ?? { updatedAtLocal: 0, dirty: false } }
  writeMeta(k: string, m: { updatedAtLocal: number; dirty: boolean }) { this.meta.set(k, m) }
  keys() { return SYNCED_KEYS.slice() }
}

describe('PrefsSyncEngine', () => {
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

  it('isolates snippets so an old Relay does not reject established dirty keys', async () => {
    const a = new FakeAdapter()
    a.writeValue('quick_templates', [{ id: 'ok', label: 'ok', text: 'ok' }])
    a.writeMeta('quick_templates', { updatedAtLocal: 100, dirty: true })
    a.writeValue('snippets', [{ id: 'deploy', label: 'Deploy', text: 'make deploy' }])
    a.writeMeta('snippets', { updatedAtLocal: 200, dirty: true })
    const put = vi.fn(async (items: Array<{ key: string; value: unknown; client_updated_at: number }>) => {
      if (items.some((item) => item.key === 'snippets')) throw new Error('unknown_key')
      return items.map((item) => ({ key: item.key, value: item.value, updated_at: item.client_updated_at + 1 }))
    })

    await expect(new PrefsSyncEngine(a, { get: vi.fn(), put }).push()).rejects.toThrow('unknown_key')

    expect(put.mock.calls).toEqual([
      [[{ key: 'quick_templates', value: [{ id: 'ok', label: 'ok', text: 'ok' }], client_updated_at: 100 }]],
      [[{ key: 'snippets', value: [{ id: 'deploy', label: 'Deploy', text: 'make deploy' }], client_updated_at: 200 }]],
    ])
    expect(a.meta.get('quick_templates')).toEqual({ updatedAtLocal: 101, dirty: false })
    expect(a.meta.get('snippets')).toEqual({ updatedAtLocal: 200, dirty: true })
  })

  it('does not overwrite an established preference edited during the snippets PUT', async () => {
    const a = new FakeAdapter()
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

    expect(a.values.get('quick_templates')).toEqual([{ id: 'new', label: 'New', text: 'new' }])
    expect(a.meta.get('quick_templates')).toEqual({ updatedAtLocal: 300, dirty: true })
  })

  it('ignores an unsent preference returned by an established PUT', async () => {
    const a = new FakeAdapter()
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

    expect(a.values.get('notifications_enabled')).toBe(true)
    expect(a.meta.get('notifications_enabled')).toEqual({ updatedAtLocal: 300, dirty: true })
  })

  it('pull adopts server value when newer and not dirty', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    a.writeMeta('locale_preference', { updatedAtLocal: 100, dirty: false })
    const r: RelayClient = {
      get: vi.fn().mockResolvedValue([{ key: 'locale_preference', value: 'zh-CN', updated_at: 500 }]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, r)
    await e.pull()
    expect(a.values.get('locale_preference')).toBe('zh-CN')
    expect(a.meta.get('locale_preference')).toEqual({ updatedAtLocal: 500, dirty: false })
  })

  it('pull preserves dirty local even when server is newer', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    a.writeMeta('locale_preference', { updatedAtLocal: 800, dirty: true })
    const r: RelayClient = {
      get: vi.fn().mockResolvedValue([{ key: 'locale_preference', value: 'zh-CN', updated_at: 500 }]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, r)
    await e.pull()
    expect(a.values.get('locale_preference')).toBe('en')
    expect(a.meta.get('locale_preference')?.dirty).toBe(true)
  })

  it.each(['pull', 'push'] as const)('%s ignores retired keys and only pull adopts pull-only preferences', async (operation) => {
    const a = new FakeAdapter()
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
    const r: RelayClient = { get: vi.fn().mockResolvedValue(items), put: vi.fn().mockResolvedValue(items) }

    await new PrefsSyncEngine(a, r)[operation]()

    expect(a.values.has('profiles_encrypted')).toBe(false)
    expect(a.meta.has('profiles_encrypted')).toBe(false)
    for (const method of access) {
      expect(method.mock.calls.some(([key]) => key === 'profiles_encrypted')).toBe(false)
    }
    expect(a.values.get('locale_preference')).toBe('zh-CN')
    expect(a.meta.get('locale_preference')).toEqual({ updatedAtLocal: 500, dirty: false })
    if (operation === 'push') {
      expect(a.values.get('ssh_hosts_encrypted')).toBe('old-ssh-hosts')
      expect(a.meta.get('ssh_hosts_encrypted')).toEqual({ updatedAtLocal: 100, dirty: true })
      expect(r.put).toHaveBeenCalledWith([
        { key: 'locale_preference', value: 'en', client_updated_at: 100 },
      ])
    } else {
      expect(a.values.get('ssh_hosts_encrypted')).toBe('new-ssh-hosts')
      expect(a.meta.get('ssh_hosts_encrypted')).toEqual({ updatedAtLocal: 500, dirty: false })
    }
  })

  it('markDirty + push sends dirty keys and clears flag', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'zh-CN')
    const putMock = vi.fn().mockResolvedValue([
      { key: 'locale_preference', value: 'zh-CN', updated_at: 1234 },
    ])
    const r: RelayClient = { get: vi.fn(), put: putMock }
    const e = new PrefsSyncEngine(a, r)
    e.markDirty('locale_preference', 1000)
    await e.push()
    expect(putMock).toHaveBeenCalledWith([
      { key: 'locale_preference', value: 'zh-CN', client_updated_at: 1000 },
    ])
    expect(a.meta.get('locale_preference')).toEqual({ updatedAtLocal: 1234, dirty: false })
  })

  it('push with no dirty keys does not call relay', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    const putMock = vi.fn()
    const r: RelayClient = { get: vi.fn(), put: putMock }
    const e = new PrefsSyncEngine(a, r)
    await e.push()
    expect(putMock).not.toHaveBeenCalled()
  })
})
