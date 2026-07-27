import type { Platform, Capabilities, RelayBridge, SessionBridge, RemoteSession } from './types'
import { apiFetch } from '@webshared/api/client'
// ^ resolved via the '@webshared' vite alias to web/src/shared/api/client
// (desktop/frontend/vite.config.ts).

const CAPS: Capabilities = {
  localPty: false,
  autoUpdate: false,
  pluginHost: false,
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: true,
}

// helper for relay config storage — mirrors web's current key
const RELAY_KEY = 'atterm.relay.session'

const relay: RelayBridge = {
  async load() {
    try {
      const raw = localStorage.getItem(RELAY_KEY)
      if (!raw) return null
      const j = JSON.parse(raw)
      // Adapt web's stored shape to the internal RelayConfig shape.
      return {
        base_url: j.baseURL ?? '',
        session_token: j.sessionToken ?? '',
        expires_at: j.expiresAt ?? 0,
        allow_insecure_relay: !!j.allowInsecure,
      } as any
    } catch { return null }
  },
  async save(cfg) {
    localStorage.setItem(RELAY_KEY, JSON.stringify({
      baseURL: (cfg as any).base_url,
      sessionToken: (cfg as any).session_token,
      expiresAt: (cfg as any).expires_at,
      allowInsecure: (cfg as any).allow_insecure_relay,
    }))
  },
  async clear() {
    localStorage.removeItem(RELAY_KEY)
  },
  async fetchMe() {
    const { data } = await apiFetch<any>('/api/me')
    return data
  },
}

const sessions: SessionBridge = {
  async closeSession(sid) {
    await apiFetch(`/api/sessions/${encodeURIComponent(sid)}`, { method: 'DELETE' })
  },
  async listShells() { return [] },
  async listRemoteSessions(): Promise<RemoteSession[]> {
    const { data } = await apiFetch<{ items?: any[] }>('/api/sessions')
    // Map SessionInfo → RemoteSession same way App.vue's adaptSession does.
    return (data.items ?? []).map((s: any) => ({ ...s, session_id: s.id })) as RemoteSession[]
  },
  async markSessionsSeen(opts) {
    await apiFetch('/api/sessions/seen', { method: 'POST', body: JSON.stringify(opts) })
  },
  async getPins() {
    try {
      const raw = localStorage.getItem('atterm.pinned_session_ids.value')
      if (raw === null) return []
      const v = JSON.parse(raw)
      return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
    } catch { return [] }
  },
  async setPins(ids) {
    localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(ids))
    const { notifyLocalChange } = await import('@webshared/sync/prefsSync')
    notifyLocalChange('pinned_session_ids')
  },
}

export function createWebPlatform(): Platform {
  // Populated in subsequent tasks.
  return {
    caps: CAPS,
    relay,
    sessions,
    system: {} as any,
    events: {} as any,
    templates: {} as any,
    auxKeys: {} as any,
  }
}
