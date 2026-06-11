// Cross-device user-preference sync engine for the web frontend.
// Mirrors desktop/frontend/src/lib/prefsSync.ts — same key list, same
// per-key LWW semantics. The relay client uses apiFetch so the
// Authorization header and 401-redirect logic are handled centrally.

import { apiFetch } from '@shared/api/client'

export const SYNCED_KEYS = [
  'locale_preference',
  'quick_templates',
  'notifications_enabled',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
] as const

export type SyncedKey = (typeof SYNCED_KEYS)[number]

export interface Meta { updatedAtLocal: number; dirty: boolean }

export interface Adapter {
  readValue(k: string): unknown | undefined
  writeValue(k: string, v: unknown): void
  readMeta(k: string): Meta
  writeMeta(k: string, m: Meta): void
  keys(): string[]
}

export interface ServerItem { key: string; value: unknown; updated_at: number }
export interface ClientItem { key: string; value: unknown; client_updated_at: number }

export interface RelayClient {
  get(): Promise<ServerItem[]>
  put(items: ClientItem[]): Promise<ServerItem[]>
}

export class PrefsSyncEngine {
  constructor(private readonly adapter: Adapter, private readonly relay: RelayClient) {}

  async pull(): Promise<void> {
    const items = await this.relay.get()
    for (const it of items) {
      const local = this.adapter.readMeta(it.key)
      if (it.updated_at > local.updatedAtLocal) {
        if (local.dirty) continue
        this.adapter.writeValue(it.key, it.value)
        this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
      }
    }
  }

  markDirty(key: string, updatedAtLocal: number): void {
    this.adapter.writeMeta(key, { updatedAtLocal, dirty: true })
  }

  async push(): Promise<void> {
    const items: ClientItem[] = []
    for (const k of this.adapter.keys()) {
      const m = this.adapter.readMeta(k)
      if (!m.dirty) continue
      const v = this.adapter.readValue(k)
      if (v === undefined) continue
      items.push({ key: k, value: v, client_updated_at: m.updatedAtLocal })
    }
    if (items.length === 0) return
    const resp = await this.relay.put(items)
    for (const it of resp) {
      this.adapter.writeValue(it.key, it.value)
      this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
    }
  }
}

export function localStorageAdapter(): Adapter {
  const v = (k: string) => `atterm.${k}.value`
  const m = (k: string) => `atterm.${k}.meta`
  return {
    readValue(k) {
      const r = localStorage.getItem(v(k))
      if (r === null) return undefined
      try { return JSON.parse(r) } catch { return undefined }
    },
    writeValue(k, val) { localStorage.setItem(v(k), JSON.stringify(val)) },
    readMeta(k) {
      const r = localStorage.getItem(m(k))
      if (r === null) return { updatedAtLocal: 0, dirty: false }
      try {
        const x = JSON.parse(r) as { updatedAtLocal?: unknown; dirty?: unknown }
        return { updatedAtLocal: Number(x?.updatedAtLocal ?? 0), dirty: !!x?.dirty }
      } catch { return { updatedAtLocal: 0, dirty: false } }
    },
    writeMeta(k, meta) { localStorage.setItem(m(k), JSON.stringify(meta)) },
    keys() { return [...SYNCED_KEYS] },
  }
}

export function apiRelayClient(): RelayClient {
  return {
    async get(): Promise<ServerItem[]> {
      const { data } = await apiFetch<{ items: ServerItem[] }>('/api/me/preferences')
      return data.items ?? []
    },
    async put(items: ClientItem[]): Promise<ServerItem[]> {
      const { data } = await apiFetch<{ items: ServerItem[] }>('/api/me/preferences', {
        method: 'PUT',
        body: JSON.stringify({ items }),
      })
      return data.items ?? []
    },
  }
}

let SHARED: PrefsSyncEngine | null = null
export function setSharedPrefsSync(e: PrefsSyncEngine): void { SHARED = e }
export function getSharedPrefsSync(): PrefsSyncEngine | null { return SHARED }
export function notifyLocalChange(key: string): void {
  if (!SHARED) return
  SHARED.markDirty(key, Date.now())
  void SHARED.push().catch(() => {})
}
