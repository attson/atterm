import type { Platform, RelayConfig, RelayMe, RemoteSession, SessionSummary } from './types'
import type { QuickTemplate } from '../lib/templates'
import type { AuxKey } from '../lib/auxKeys'
import { secureStorage } from './secureStorage'
import { notifyLocalChange } from '../lib/prefsSync.capacitor'

const STORAGE_KEY = 'atterm.relay.session'
const PASSWORD_KEY = 'atterm.relay.password'
const TEMPLATES_KEY = 'atterm.templates'
const AUXKEYS_KEY = 'atterm.auxkeys'

// loadLegacyFromLocalStorage reads (but does not clear) the legacy
// localStorage blob. Returned as parsed RelayConfig or null. Malformed JSON
// returns null. Only used by the migration branch in relay.load().
function loadLegacyFromLocalStorage(): RelayConfig | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as RelayConfig
  } catch {
    return null
  }
}

// parseRelayJSON is a tolerant parser shared by both storage paths.
function parseRelayJSON(raw: string | null): RelayConfig | null {
  if (!raw) return null
  try {
    return JSON.parse(raw) as RelayConfig
  } catch {
    return null
  }
}

function createEventBus() {
  const handlers = new Map<string, Set<(data: unknown) => void>>()
  return {
    on(event: string, handler: (data: unknown) => void): () => void {
      let set = handlers.get(event)
      if (!set) {
        set = new Set()
        handlers.set(event, set)
      }
      set.add(handler)
      return () => { set!.delete(handler) }
    },
    emit(event: string, data: unknown): void {
      const set = handlers.get(event)
      if (!set) return
      for (const h of [...set]) h(data)
    },
  }
}

export function createCapacitorPlatform(): Platform {
  return {
    caps: {
      localPty: false,
      autoUpdate: false,
      pluginHost: false,
      windowControls: false,
      systemClipboard: true,
      notifications: true,
      fileDialog: false,
    },
    relay: {
      // load: prefer Keychain; if empty AND localStorage has a legacy blob,
      // migrate it (write to Keychain, clear localStorage), then return.
      load: async () => {
        const fromSecure = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
        if (fromSecure) return fromSecure

        const legacy = loadLegacyFromLocalStorage()
        if (!legacy) return null
        await secureStorage.set(STORAGE_KEY, JSON.stringify(legacy))
        if (typeof localStorage !== 'undefined') localStorage.removeItem(STORAGE_KEY)
        return legacy
      },
      // save: write only to Keychain. localStorage is never written.
      save: async (cfg) => {
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))
      },
      // clear: wipe both stores. localStorage clear is belt-and-braces in case
      // a previous migration was interrupted between the Keychain write and
      // the localStorage remove.
      clear: async () => {
        await secureStorage.remove(STORAGE_KEY)
        if (typeof localStorage !== 'undefined') localStorage.removeItem(STORAGE_KEY)
      },
      fetchMe: async (): Promise<RelayMe> => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg || !cfg.url || !cfg.token) throw new Error('relay_not_configured')
        const base = cfg.url.replace(/\/$/, '')
        const res = await fetch(base + '/api/me', {
          method: 'GET',
          headers: { Authorization: `Bearer ${cfg.token}` },
          credentials: 'omit',
        })
        if (!res.ok) throw new Error(`relay fetchMe failed: HTTP ${res.status}`)
        return (await res.json()) as RelayMe
      },
      consumePairing: async (relayBase, token) => {
        const base = relayBase.replace(/\/$/, '')
        const res = await fetch(base + '/api/pair/consume', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token }),
          credentials: 'omit',
        })
        if (res.status === 404) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body.code || 'pair_invalid')
        }
        if (!res.ok) throw new Error(`pair_consume_http_${res.status}`)
        return (await res.json()) as { relay_url: string; session_token: string; expires_at: number; user: { id: string; email: string } }
      },
      login: async (url, email, password, allowInsecure) => {
        const base = url.replace(/\/$/, '')
        let res: Response
        try {
          res = await fetch(base + '/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password }),
            credentials: 'omit',
          })
        } catch {
          throw new Error('cannot_reach_relay')
        }
        if (res.status === 401) throw new Error('invalid_credentials')
        if (res.status === 429) throw new Error('rate_limited')
        if (!res.ok) throw new Error('http_' + res.status)
        const body = (await res.json()) as {
          session_token: string
          expires_at: number
          user: { id: string; email: string }
        }
        const cfg: RelayConfig = {
          url: base,
          token: body.session_token,
          session_expires_at: body.expires_at,
          allow_insecure_relay: allowInsecure,
          remote_permission: 'full',
          last_email: body.user.email,
          connected: false,
        }
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))
        await secureStorage.set(PASSWORD_KEY, password)
      },
      logout: async () => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg) return
        if (cfg.url && cfg.token) {
          const base = cfg.url.replace(/\/$/, '')
          try {
            await fetch(base + '/api/auth/logout', {
              method: 'POST',
              headers: { Authorization: `Bearer ${cfg.token}` },
              credentials: 'omit',
            })
          } catch {
            // Best-effort. Local clear still happens below.
          }
        }
        const cleared: RelayConfig = {
          ...cfg,
          token: '',
          session_expires_at: 0,
        }
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cleared))
      },
      loadSavedPassword: async () => {
        const v = await secureStorage.get(PASSWORD_KEY)
        return v ?? ''
      },
      // setUplinkPaused omitted — desktop-only
    },
    sessions: {
      // newSession omitted — capacitor cannot fork local PTYs
      closeSession: async () => {
        // Attach-only client: closing a tab detaches the local WS (handled in
        // MobileApp by dropping it from the keepalive registry). It does NOT
        // kill the remote PTY — that stays owned by the host that started it.
      },
      listShells: async () => [],
      listRemoteSessions: async (): Promise<RemoteSession[]> => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg || !cfg.url || !cfg.token) return []
        const base = cfg.url.replace(/\/$/, '')
        const res = await fetch(base + '/api/sessions', {
          headers: { Authorization: `Bearer ${cfg.token}` },
          credentials: 'omit',
        })
        if (res.status === 401) throw new Error('relay_unauthorized')
        if (!res.ok) throw new Error(`list sessions: HTTP ${res.status}`)
        const raw = (await res.json()) as Array<{
          id: string; command: string; title: string; cwd: string; cols: number; rows: number;
          host_id: string; host: string; user: string; remote_permission?: string; task_state?: RemoteSession['task_state'];
          current_command?: string; command_started_at?: number; command_ended_at?: number; command_duration_ms?: number;
          command_exit_code?: number; last_output_at?: number; type?: string; summary?: SessionSummary;
          unread?: boolean; attention_at?: number;
        }>
        return raw.map((s) => {
          const out: RemoteSession = {
            session_id: s.id,
            host_id: s.host_id,
            host: s.host,
            user: s.user,
            title: s.title || s.command,
            cwd: s.cwd,
            cols: s.cols,
            rows: s.rows,
          }
          if (s.remote_permission !== undefined) out.remote_permission = s.remote_permission
          if (s.task_state !== undefined) out.task_state = s.task_state
          if (s.current_command !== undefined) out.current_command = s.current_command
          if (s.command_started_at !== undefined) out.command_started_at = s.command_started_at
          if (s.command_ended_at !== undefined) out.command_ended_at = s.command_ended_at
          if (s.command_duration_ms !== undefined) out.command_duration_ms = s.command_duration_ms
          if (s.command_exit_code !== undefined) out.command_exit_code = s.command_exit_code
          if (s.last_output_at !== undefined) out.last_output_at = s.last_output_at
          if (s.type !== undefined) out.type = s.type
          if (s.summary !== undefined) out.summary = s.summary
          if (s.unread !== undefined) out.unread = s.unread
          if (s.attention_at !== undefined) out.attention_at = s.attention_at
          return out
        })
      },
      markSessionsSeen: async (opts) => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg?.url || !cfg.token) return
        const base = cfg.url.replace(/\/$/, '')
        const body = 'all' in opts && opts.all
          ? { all: true }
          : { session_ids: (opts as { ids: string[] }).ids }
        const res = await fetch(base + '/api/sessions/seen', {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${cfg.token}`,
            'Content-Type': 'application/json',
          },
          credentials: 'omit',
          body: JSON.stringify(body),
        })
        if (res.status === 401) throw new Error('relay_unauthorized')
        if (!res.ok) throw new Error(`mark-seen: HTTP ${res.status}`)
      },
    },
    system: {
      showNotification: async () => {
        // PR-D wires @capacitor/local-notifications
      },
      getClipboardPaste: async () => ({ kind: 'none' }),
      openExternalURL: async (url: string) => {
        if (typeof window !== 'undefined' && typeof window.open === 'function') {
          window.open(url, '_blank')
        }
      },
      getEnvironment: async () => ({ buildType: 'capacitor', platform: 'ios', arch: 'arm64' }),
      // window* + pickLogFilePath omitted — desktop-only
    },
    events: createEventBus(),
    templates: {
      load: async () => {
        if (typeof localStorage === 'undefined') return []
        const raw = localStorage.getItem(TEMPLATES_KEY)
        if (!raw) return []
        try {
          const parsed = JSON.parse(raw)
          return Array.isArray(parsed) ? (parsed as QuickTemplate[]) : []
        } catch {
          return []
        }
      },
      save: async (list) => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem(TEMPLATES_KEY, JSON.stringify(list))
        try {
          localStorage.setItem('atterm.quick_templates.value', JSON.stringify(list))
        } catch { /* ignore */ }
        notifyLocalChange('quick_templates')
      },
      clear: async () => {
        if (typeof localStorage === 'undefined') return
        localStorage.removeItem(TEMPLATES_KEY)
      },
      loadHidden: async () => {
        if (typeof localStorage === 'undefined') return false
        return localStorage.getItem(TEMPLATES_KEY + '.hidden') === '1'
      },
      saveHidden: async (hidden: boolean) => {
        if (typeof localStorage === 'undefined') return
        if (hidden) localStorage.setItem(TEMPLATES_KEY + '.hidden', '1')
        else localStorage.removeItem(TEMPLATES_KEY + '.hidden')
      },
    },
    auxKeys: {
      load: async () => {
        if (typeof localStorage === 'undefined') return []
        const raw = localStorage.getItem(AUXKEYS_KEY)
        if (!raw) return []
        try {
          const parsed = JSON.parse(raw)
          return Array.isArray(parsed) ? (parsed as AuxKey[]) : []
        } catch {
          return []
        }
      },
      save: async (list) => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem(AUXKEYS_KEY, JSON.stringify(list))
      },
      clear: async () => {
        if (typeof localStorage === 'undefined') return
        localStorage.removeItem(AUXKEYS_KEY)
      },
    },
    // updater + pluginHost omitted — desktop-only
  }
}
