import type { Platform, Capabilities, RelayBridge, SessionBridge, RemoteSession } from './types'
import { apiFetch } from '@webshared/api/client'
// ^ resolved via the '@webshared' vite alias to web/src/shared/api/client
// (desktop/frontend/vite.config.ts).
import { loadRelayConfig, saveRelayConfig, clearRelayConfig } from '@webshared/api/relay-config'
// ^ single source of truth for web's relay storage key ('atterm.relay') and
// shape ({ baseURL, sessionToken, expiresAt, allowInsecure, ... }) — apiFetch
// itself reads auth through loadRelayConfig, so the bridge below must adapt
// to/from exactly this shape or requests silently stop carrying a token.

const CAPS: Capabilities = {
  localPty: false,
  autoUpdate: false,
  pluginHost: false,
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: true,
}

const relay: RelayBridge = {
  async load() {
    const cfg = loadRelayConfig()
    if (!cfg) return null
    // Adapt web's { baseURL, sessionToken, expiresAt, allowInsecure } shape
    // to the internal RelayConfig shape ({ url, token, session_expires_at,
    // allow_insecure_relay, ... }). remote_permission/last_email/connected
    // have no web-side equivalent (they're Wails/Go uplink concepts) so they
    // get the same defaults setRelayConfig() uses in lib/api.ts.
    return {
      url: cfg.baseURL,
      token: cfg.sessionToken ?? '',
      session_expires_at: cfg.expiresAt ?? 0,
      allow_insecure_relay: cfg.allowInsecure,
      remote_permission: 'full',
      last_email: '',
      connected: false,
      ...(cfg.realmId !== undefined ? { realmId: cfg.realmId } : {}),
      ...(cfg.homeInstanceURL !== undefined ? { homeInstanceURL: cfg.homeInstanceURL } : {}),
    }
  },
  async save(cfg) {
    saveRelayConfig({
      baseURL: cfg.url,
      sessionToken: cfg.token || null,
      expiresAt: cfg.session_expires_at || null,
      allowInsecure: cfg.allow_insecure_relay,
      ...(cfg.realmId !== undefined ? { realmId: cfg.realmId } : {}),
      ...(cfg.homeInstanceURL !== undefined ? { homeInstanceURL: cfg.homeInstanceURL } : {}),
    })
  },
  async clear() {
    clearRelayConfig()
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
