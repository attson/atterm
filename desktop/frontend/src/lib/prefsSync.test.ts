import { describe, it, expect, vi } from 'vitest'
import { PrefsSyncEngine, type Adapter, type RelayClient, SYNCED_KEYS } from './prefsSync'

class FakeAdapter implements Adapter {
  values = new Map<string, unknown>()
  meta = new Map<string, { updatedAtLocal: number; dirty: boolean }>()
  readValue(k: string) { return this.values.has(k) ? this.values.get(k) : undefined }
  writeValue(k: string, v: unknown) { this.values.set(k, v) }
  readMeta(k: string) { return this.meta.get(k) ?? { updatedAtLocal: 0, dirty: false } }
  writeMeta(k: string, m: { updatedAtLocal: number; dirty: boolean }) { this.meta.set(k, m) }
  keys() { return SYNCED_KEYS.slice() }
}

// Mirrors internal/prefssync/sync.go::syncedKeys. If Go grows a new key
// (or drops one), update BOTH this literal AND the SYNCED_KEYS list in
// desktop/frontend/src/lib/prefsSync.ts + web/src/shared/sync/prefsSync.ts.
// The drift-check test below cross-checks TS against this expectation —
// a hardcoded literal is intentional (importing Go at test time is
// impractical) and reviewed together with the Go change.
const EXPECTED_SYNCED_KEYS = [
  'ai_notifications_only',
  'command_notify_threshold_seconds',
  'locale_preference',
  'notifications_enabled',
  'pinned_session_ids',
  'quick_templates',
  'shell_integration_enabled',
] as const

describe('PrefsSyncEngine', () => {
  it('SYNCED_KEYS matches the Go source of truth (internal/prefssync/sync.go)', () => {
    expect(SYNCED_KEYS.slice().sort()).toEqual([...EXPECTED_SYNCED_KEYS])
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
