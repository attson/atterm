import type { Platform, Capabilities, RelayBridge, SessionBridge, RemoteSession, SystemBridge, EventBus, TemplateBridge, AuxKeyBridge } from './types'
import { apiFetch } from '@webshared/api/client'
// ^ resolved via the '@webshared' vite alias to web/src/shared/api/client
// (desktop/frontend/vite.config.ts).
import { loadRelayConfig, saveRelayConfig, clearRelayConfig } from '@webshared/api/relay-config'
import { logout as webLogout } from '@webshared/api/auth'
import { loadAccountKey } from '@webshared/api/account-key'
import { setAccountKeyProvider } from '../lib/account-key'
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
  wailsBindings: false,
  capacitor: false,
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
  async logout() {
    await webLogout()
  },
}

const sessions: SessionBridge = {
  async closeSession(sid) {
    await apiFetch(`/api/sessions/${encodeURIComponent(sid)}`, { method: 'DELETE' })
  },
  async listShells() { return [] },
  async listRemoteSessions(): Promise<RemoteSession[]> {
    // /api/sessions returns SessionInfo[] directly, not {items:[...]}.
    // Reuse web's listSessions helper for E2EE-sealed field decryption
    // (title / cwd / command / current_command); otherwise those fields
    // stay blank on the sidebar.
    const { listSessions } = await import('@webshared/api/sessions')
    const items = await listSessions()
    // Map SessionInfo (id) → RemoteSession (session_id) same way
    // App.vue's adaptSession does.
    return items.map((s) => ({ ...s, session_id: s.id })) as unknown as RemoteSession[]
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
  async listRelaySessions() {
    const { data } = await apiFetch<import('../lib/api').RelaySessionRow[]>('/api/me/sessions')
    return data
  },
  async revokeRelaySession(idHash) {
    await apiFetch(`/api/me/sessions/${encodeURIComponent(idHash)}`, { method: 'DELETE' })
  },
  async signOutOtherRelaySessions() {
    const { data } = await apiFetch<import('../lib/api').SignOutOthersResult>(
      '/api/me/sessions/sign-out-others',
      { method: 'POST' },
    )
    return data
  },
}

const events: EventBus = (() => {
  const map = new Map<string, Set<(data: unknown) => void>>()
  return {
    on(name, handler) {
      let set = map.get(name)
      if (!set) { set = new Set(); map.set(name, set) }
      set.add(handler)
      return () => { set!.delete(handler) }
    },
    emit(name, data) {
      const set = map.get(name)
      if (!set) return
      for (const fn of Array.from(set)) { try { fn(data) } catch { /* listener errors don't break emit */ } }
    },
  }
})()

const system: SystemBridge = {
  async showNotification(title, body, data) {
    if (typeof Notification === 'undefined') return
    try {
      const n = new Notification(title, { body })
      if (data) n.onclick = () => events.emit('notification:click', data)
    } catch { /* permission denied or unsupported — silent, matches capacitor stub */ }
  },
  async getClipboardPaste() {
    if (typeof navigator === 'undefined' || !navigator.clipboard?.readText) return { kind: 'none' }
    try {
      const text = await navigator.clipboard.readText()
      return text ? { kind: 'text', text } : { kind: 'none' }
    } catch {
      return { kind: 'none' }
    }
  },
  async openExternalURL(url) {
    window.open(url, '_blank', 'noopener')
  },
  async getEnvironment() {
    return { buildType: 'web', platform: navigator.userAgent, arch: '' }
  },
  async getAppVersion() {
    const { fetchVersion } = await import('@webshared/api/version')
    return fetchVersion()
  },
}

const TEMPLATES_KEY = 'atterm.quick_templates.value'
const TEMPLATES_HIDDEN_KEY = 'atterm.templates_hidden.value'
const AUXKEYS_KEY = 'atterm.aux_keys.value'

const templates: TemplateBridge = {
  async load() {
    try {
      const raw = localStorage.getItem(TEMPLATES_KEY)
      if (!raw) return []
      const v = JSON.parse(raw)
      return Array.isArray(v) ? v : []
    } catch { return [] }
  },
  async save(list) {
    localStorage.setItem(TEMPLATES_KEY, JSON.stringify(list))
    const { notifyLocalChange } = await import('@webshared/sync/prefsSync')
    notifyLocalChange('quick_templates')
  },
  async clear() {
    localStorage.removeItem(TEMPLATES_KEY)
  },
  async loadHidden() {
    try { return JSON.parse(localStorage.getItem(TEMPLATES_HIDDEN_KEY) ?? 'false') }
    catch { return false }
  },
  async saveHidden(hidden) {
    localStorage.setItem(TEMPLATES_HIDDEN_KEY, JSON.stringify(hidden))
  },
}

const auxKeys: AuxKeyBridge = {
  async load() {
    try {
      const raw = localStorage.getItem(AUXKEYS_KEY)
      if (!raw) return []
      const v = JSON.parse(raw)
      return Array.isArray(v) ? v : []
    } catch { return [] }
  },
  async save(list) {
    localStorage.setItem(AUXKEYS_KEY, JSON.stringify(list))
  },
  async clear() {
    localStorage.removeItem(AUXKEYS_KEY)
  },
}

export function createWebPlatform(): Platform {
  setAccountKeyProvider(() => loadAccountKey())
  return { caps: CAPS, relay, sessions, system, events, templates, auxKeys }
}
