import type { Platform, RelayConfig, RelayMe, RemoteSession, SessionSummary } from './types'
import type { QuickTemplate } from '../lib/templates'
import type { AuxKey } from '../lib/auxKeys'
import { CapacitorHttp } from '@capacitor/core'
import { secureStorage } from './secureStorage'
import { notifyLocalChange } from '../lib/prefsSync.capacitor'
import { debugLog } from '../lib/debugLog'

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
      // load: prefer localStorage (synchronous, never hangs). Keychain is a
      // last-resort fallback only consulted when localStorage is empty, with
      // its own short race against setTimeout — but if the JS timer pool is
      // frozen (the documented WKWebView issue that surfaced as "保存配置…
      // 0.1s" stuck), setTimeout never fires either, so we shield from that
      // by checking localStorage FIRST. Save now always writes localStorage,
      // so after the first pairing this branch is the hot path.
      load: async () => {
        debugLog('relay.load: enter')
        const fromLocal = loadLegacyFromLocalStorage()
        if (fromLocal) {
          debugLog('relay.load: hit localStorage, returning')
          return fromLocal
        }
        debugLog('relay.load: localStorage empty, trying Keychain (race with 3s)')

        const fromSecure = await Promise.race([
          secureStorage.get(STORAGE_KEY).catch((e) => { debugLog('relay.load: keychain.get rejected: ' + String(e?.message || e)); return null }),
          new Promise<null>((resolve) => setTimeout(() => { debugLog('relay.load: keychain race 3s timeout'); resolve(null) }, 3000)),
        ]).then(parseRelayJSON)
        if (fromSecure) {
          debugLog('relay.load: got from Keychain, mirroring to localStorage')
          if (typeof localStorage !== 'undefined') {
            try { localStorage.setItem(STORAGE_KEY, JSON.stringify(fromSecure)) } catch {}
          }
          return fromSecure
        }
        debugLog('relay.load: no config found, returning null')
        return null
      },
      // save: localStorage commits synchronously (the primary write);
      // Keychain runs best-effort in the background. The relay session token
      // is already scoped to this app's WKWebView data store and the iOS app
      // sandbox, so localStorage offers equivalent at-rest isolation to
      // Keychain for our threat model. We previously awaited the Keychain
      // call, which froze the pairing screen on "保存配置…" when the
      // Capacitor bridge to AttermSecureStorage hung after a CapacitorHttp
      // round-trip on the same bridge — likely a bridge-state interaction
      // bug, but the symptom is a flow that never completes. load() already
      // reads Keychain first then falls back to localStorage, so both stores
      // converge once the next app launch happens.
      save: async (cfg) => {
        debugLog('relay.save: enter, url=' + cfg.url + ' token.len=' + (cfg.token || '').length)
        const json = JSON.stringify(cfg)
        debugLog('relay.save: stringified ' + json.length + ' chars')
        try {
          secureStorage.set(STORAGE_KEY, json).then(
            () => debugLog('relay.save: keychain.set resolved (bg)'),
            (e) => debugLog('relay.save: keychain.set rejected (bg): ' + String(e?.message || e)),
          )
          debugLog('relay.save: keychain.set dispatched (not awaited)')
        } catch (e: any) {
          debugLog('relay.save: keychain.set threw sync: ' + String(e?.message || e))
        }
        if (typeof localStorage !== 'undefined') {
          try {
            localStorage.setItem(STORAGE_KEY, json)
            debugLog('relay.save: localStorage.setItem ok')
          } catch (e: any) {
            debugLog('relay.save: localStorage.setItem threw: ' + String(e?.message || e))
          }
        }
        debugLog('relay.save: returning')
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
        // Use CapacitorHttp explicitly instead of fetch. fetch() on iOS goes
        // through WKWebView; when a TLS handshake hangs there, the entire JS
        // timer pool freezes (setInterval / setTimeout / requestAnimationFrame
        // all stop firing), and AbortController.abort() is also dropped.
        // CapacitorHttp.post hits NSURLSession directly with native timeouts
        // that fire regardless of WebView state.
        let resp: { status: number; data: unknown }
        try {
          resp = await CapacitorHttp.post({
            url: base + '/api/pair/consume',
            headers: { 'Content-Type': 'application/json' },
            data: { token },
            connectTimeout: 10_000,
            readTimeout: 15_000,
          })
        } catch (e) {
          const msg = (e as { message?: string })?.message ?? String(e)
          if (/timeout|timed out/i.test(msg)) throw new Error('pair_timeout')
          throw new Error('cannot_reach_relay')
        }
        if (resp.status === 404) {
          const body = (resp.data as { code?: string }) || {}
          throw new Error(body.code || 'pair_invalid')
        }
        if (resp.status < 200 || resp.status >= 300) {
          throw new Error(`pair_consume_http_${resp.status}`)
        }
        return resp.data as { relay_url: string; session_token: string; expires_at: number; user: { id: string; email: string } }
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
