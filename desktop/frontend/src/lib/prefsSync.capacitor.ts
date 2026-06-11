import {
  PrefsSyncEngine,
  SYNCED_KEYS,
  type Adapter,
  type ClientItem,
  type Meta,
  type RelayClient,
  type ServerItem,
} from './prefsSync'

const VALUE_KEY = (k: string) => `atterm.${k}.value`
const META_KEY = (k: string) => `atterm.${k}.meta`

export function localStorageAdapter(): Adapter {
  return {
    readValue(k) {
      const raw = localStorage.getItem(VALUE_KEY(k))
      if (raw === null) return undefined
      try { return JSON.parse(raw) } catch { return undefined }
    },
    writeValue(k, v) {
      localStorage.setItem(VALUE_KEY(k), JSON.stringify(v))
    },
    readMeta(k): Meta {
      const raw = localStorage.getItem(META_KEY(k))
      if (raw === null) return { updatedAtLocal: 0, dirty: false }
      try {
        const m = JSON.parse(raw)
        return { updatedAtLocal: Number(m?.updatedAtLocal ?? 0), dirty: !!m?.dirty }
      } catch { return { updatedAtLocal: 0, dirty: false } }
    },
    writeMeta(k, m) {
      localStorage.setItem(META_KEY(k), JSON.stringify(m))
    },
    keys() { return [...SYNCED_KEYS] },
  }
}

interface StoredRelayConfig {
  baseURL: string
  sessionToken: string
  expiresAt?: number
  allowInsecure?: boolean
}

function loadRelay(): StoredRelayConfig | null {
  const raw = localStorage.getItem('atterm.relay.session')
  if (!raw) return null
  try { return JSON.parse(raw) as StoredRelayConfig } catch { return null }
}

export function capacitorRelayClient(): RelayClient {
  return {
    async get(): Promise<ServerItem[]> {
      const cfg = loadRelay()
      if (!cfg?.sessionToken) throw new Error('not_logged_in')
      const res = await fetch(cfg.baseURL.replace(/\/$/, '') + '/api/me/preferences', {
        method: 'GET',
        headers: { Authorization: `Bearer ${cfg.sessionToken}` },
        credentials: 'omit',
      })
      if (!res.ok) throw new Error(`get prefs: HTTP ${res.status}`)
      const body = await res.json() as { items: ServerItem[] }
      return body.items ?? []
    },
    async put(items: ClientItem[]): Promise<ServerItem[]> {
      const cfg = loadRelay()
      if (!cfg?.sessionToken) throw new Error('not_logged_in')
      const res = await fetch(cfg.baseURL.replace(/\/$/, '') + '/api/me/preferences', {
        method: 'PUT',
        headers: {
          Authorization: `Bearer ${cfg.sessionToken}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ items }),
        credentials: 'omit',
      })
      if (!res.ok) throw new Error(`put prefs: HTTP ${res.status}`)
      const body = await res.json() as { items: ServerItem[] }
      return body.items ?? []
    },
  }
}

export function createCapacitorPrefsSync(): PrefsSyncEngine {
  return new PrefsSyncEngine(localStorageAdapter(), capacitorRelayClient())
}

// Shared engine accessor — set by main.capacitor.ts at bootstrap, consumed
// by setting setters that want to mark dirty + push.
let SHARED_ENGINE: PrefsSyncEngine | null = null
export function setSharedPrefsSync(e: PrefsSyncEngine): void { SHARED_ENGINE = e }
export function notifyLocalChange(key: string): void {
  if (!SHARED_ENGINE) return
  SHARED_ENGINE.markDirty(key, Date.now())
  void SHARED_ENGINE.push().catch(() => {})
}
