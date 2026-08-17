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
