// Cross-device user-preference sync engine. Mirror of internal/prefssync
// (Go) — same key list, same per-key LWW semantics. Shared between the
// Wails desktop entry and the Capacitor mobile entry; the adapter is the
// per-platform persistence layer (Wails RPC vs Capacitor localStorage).

// This file's engine is shared by two platforms that have NO UI for the
// Wails-desktop-only preferences (terminal_theme, the six terminal-appearance
// keys, default_shell, shortcut_bindings, ssh_hosts_encrypted): Capacitor
// mobile (prefsSync.capacitor.ts) and, historically, a plain browser tab.
// A key belongs in SYNCED_KEYS iff this adapter's `keys()` needs to offer it
// to Push — i.e. some setter on this platform can mark it dirty via
// notifyLocalChange. Desktop-only keys are never marked dirty here, so they
// don't need to be in this list to work correctly: pull adopts non-retired
// server keys independently of SYNCED_KEYS. PUT reconciliation only adopts
// keys sent in that request, leaving pull-only preferences to pull().
//
// The Go list (internal/prefssync/sync.go::syncedKeys) is the superset source
// of truth. Keep this list a subset of it — the drift-check test at the
// bottom of prefsSync.test.ts reads sync.go directly and asserts that.
export const SYNCED_KEYS = [
  'locale_preference',
  'traffic_time_zone',
  'quick_templates',
  'snippets',
  'notifications_enabled',
  'ai_notifications_only',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
  'pinned_session_ids',
] as const

export type SyncedKey = (typeof SYNCED_KEYS)[number]

function isRetiredKey(key: string): boolean {
  return key === 'profiles_encrypted'
}

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
      if (isRetiredKey(it.key)) continue
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
    const established: ClientItem[] = []
    const snippets: ClientItem[] = []
    for (const k of this.adapter.keys()) {
      const m = this.adapter.readMeta(k)
      if (!m.dirty) continue
      const v = this.adapter.readValue(k)
      if (v === undefined) continue
      const item = { key: k, value: v, client_updated_at: m.updatedAtLocal }
      if (k === 'snippets') snippets.push(item)
      else established.push(item)
    }
    await this.pushItems(established)
    await this.pushItems(snippets)
  }

  private async pushItems(items: ClientItem[]): Promise<void> {
    if (items.length === 0) return
    const sentAt = new Map(items.map((item) => [item.key, item.client_updated_at]))
    const resp = await this.relay.put(items)
    for (const it of resp) {
      if (isRetiredKey(it.key)) continue
      const sentTimestamp = sentAt.get(it.key)
      if (sentTimestamp === undefined) continue
      if (this.adapter.readMeta(it.key).updatedAtLocal !== sentTimestamp) continue
      await this.adapter.writeValue(it.key, it.value)
      await this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
    }
  }
}
