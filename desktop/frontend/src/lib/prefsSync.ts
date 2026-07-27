// Cross-device user-preference sync engine. Mirror of internal/prefssync
// (Go) — same key list, same per-key LWW semantics. Shared between the
// Wails desktop entry and the Capacitor mobile entry; the adapter is the
// per-platform persistence layer (Wails RPC vs Capacitor localStorage).

export const SYNCED_KEYS = [
  'locale_preference',
  'quick_templates',
  'notifications_enabled',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
  'pinned_session_ids',
] as const

export type SyncedKey = (typeof SYNCED_KEYS)[number]

export interface Meta {
  updatedAtLocal: number
  dirty: boolean
}

export interface Adapter {
  readValue(key: string): unknown | undefined
  writeValue(key: string, value: unknown): void | Promise<void>
  readMeta(key: string): Meta
  writeMeta(key: string, m: Meta): void | Promise<void>
  keys(): string[]
}

export interface ServerItem {
  key: string
  value: unknown
  updated_at: number
}

export interface ClientItem {
  key: string
  value: unknown
  client_updated_at: number
}

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
        await this.adapter.writeValue(it.key, it.value)
        await this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
      }
    }
  }

  markDirty(key: string, updatedAtLocal: number): void {
    void this.adapter.writeMeta(key, { updatedAtLocal, dirty: true })
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
      await this.adapter.writeValue(it.key, it.value)
      await this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
    }
  }
}
