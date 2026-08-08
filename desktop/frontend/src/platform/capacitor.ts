import type { Platform, RelayConfig, RelayMe, RemoteSession, SessionSummary } from './types'
import type { QuickTemplate } from '../lib/templates'
import type { AuxKey } from '../lib/auxKeys'
import { CapacitorHttp } from '@capacitor/core'
import { Keyboard } from '@capacitor/keyboard'
import { secureStorage } from './secureStorage'
import { notifyLocalChange } from '../lib/prefsSync.capacitor'
import {
  SERVER_IDENTITY,
  defaultKDFParams,
  openAccountKeyWrap,
  openSessionFields,
  unwrapWithPassword,
  wrapAccountKey,
  type AccountKeyWrap,
} from '../lib/opaque'
import {
  opaqueLoginInit,
  opaqueLoginFinish,
  opaqueRegisterInit,
  opaqueRegisterFinish,
} from '../lib/opaqueWasm'
import { setAccountKeyProvider } from '../lib/account-key'
import { errText, logWarn } from "../lib/log";

const STORAGE_KEY = 'atterm.relay.session'
const PASSWORD_KEY = 'atterm.relay.password'
const ACCOUNT_KEY_KEY = 'atterm.relay.account-key'
const TEMPLATES_KEY = 'atterm.templates'
const SYNCED_TEMPLATES_KEY = 'atterm.quick_templates.value'
const AUXKEYS_KEY = 'atterm.auxkeys'

// Shared base64 helpers for the OPAQUE / wrap envelope bytes. Same
// conventions as web/src/shared/api/auth.ts so a wrap sealed on web
// can unwrap on mobile (matters when the same user is logged in
// across both clients).
function bytesToB64Std(b: Uint8Array): string {
  return btoa(String.fromCharCode(...b))
}

function parseQuickTemplateList(raw: string | null): QuickTemplate[] | null {
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as QuickTemplate[]) : null
  } catch {
    return null
  }
}

function b64StdToBytes(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

// loginFlow drives the two-stage OPAQUE handshake against a relay base
// URL. Captures the response shapes the Go relay returns at each step
// so the mobile bundle does not depend on the @shared/api types. On
// success, returns the user_id, session_token, and the unwrapped 32-
// byte account_key — the caller persists each in the right place.
//
// Errors are mapped to the historical strings the UI already handles
// (invalid_credentials, cannot_reach_relay, http_*) so the
// SettingsAccount.vue + the Capacitor login flow do not need to change.
async function opaqueLogin(
  base: string,
  email: string,
  password: string,
): Promise<{ user_id: string; session_token: string; account_key: Uint8Array; realm_id: string; home_instance_url: string }> {
  // ke1 / login_response / ke3 are all standard-base64 strings straight off the
  // WASM client; they go on the wire verbatim (no re-encode, no deserialize).
  let handle: number
  let ke1: string
  try {
    ;({ handle, ke1 } = await opaqueLoginInit(password))
  } catch {
    throw new Error('cannot_reach_relay')
  }

  let initRes: Response
  try {
    initRes = await fetch(base + '/api/auth/login/init', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, login_ke: ke1 }),
      credentials: 'omit',
    })
  } catch {
    throw new Error('cannot_reach_relay')
  }
  if (initRes.status === 401) throw new Error('invalid_credentials')
  if (initRes.status === 429) throw new Error('rate_limited')
  if (!initRes.ok) throw new Error('http_' + initRes.status)
  const initBody = (await initRes.json()) as { login_response: string; session_id: string }

  let ke3: string
  try {
    ;({ ke3 } = await opaqueLoginFinish(handle, initBody.login_response, SERVER_IDENTITY, email))
  } catch {
    throw new Error('invalid_credentials')
  }

  let finRes: Response
  try {
    finRes = await fetch(base + '/api/auth/login/finalize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email,
        session_id: initBody.session_id,
        login_ke3: ke3,
      }),
      credentials: 'omit',
    })
  } catch {
    throw new Error('cannot_reach_relay')
  }
  if (finRes.status === 401) throw new Error('invalid_credentials')
  if (!finRes.ok) throw new Error('http_' + finRes.status)
  const finBody = (await finRes.json()) as {
    user_id: string
    session_token: string
    account_key_wrap: AccountKeyWrap
    realm_id: string
    home_instance_url: string
  }
  const accountKey = unwrapWithPassword(password, finBody.account_key_wrap)
  return { user_id: finBody.user_id, session_token: finBody.session_token, account_key: accountKey, realm_id: finBody.realm_id, home_instance_url: finBody.home_instance_url }
}

// opaqueRegister mints a fresh account_key, wraps it, and runs the
// OPAQUE register init/finalize round-trip. Returns the same shape as
// opaqueLogin so callers can treat both as the entry point that unlocks
// the user's local state.
async function opaqueRegister(
  base: string,
  email: string,
  password: string,
  claim_token = '',
): Promise<{ user_id: string; session_token: string; account_key: Uint8Array }> {
  let handle: number
  let ke1: string
  try {
    ;({ handle, ke1 } = await opaqueRegisterInit(password))
  } catch {
    throw new Error('cannot_reach_relay')
  }

  let initRes: Response
  try {
    initRes = await fetch(base + '/api/auth/register/init', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, registration_ke: ke1 }),
      credentials: 'omit',
    })
  } catch {
    throw new Error('cannot_reach_relay')
  }
  if (initRes.status === 409) throw new Error('email_taken')
  if (!initRes.ok) throw new Error('http_' + initRes.status)
  const initBody = (await initRes.json()) as { registration_response: string }

  let record: string
  try {
    ;({ record } = await opaqueRegisterFinish(
      handle,
      initBody.registration_response,
      SERVER_IDENTITY,
      email,
    ))
  } catch {
    throw new Error('http_400')
  }

  const accountKey = new Uint8Array(32)
  crypto.getRandomValues(accountKey)
  const wrap = wrapAccountKey(password, accountKey, defaultKDFParams())

  const finBody: Record<string, unknown> = {
    email,
    registration_record: record,
    account_key_wrap: wrap,
  }
  if (claim_token) finBody.claim_token = claim_token

  let finRes: Response
  try {
    finRes = await fetch(base + '/api/auth/register/finalize', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(finBody),
      credentials: 'omit',
    })
  } catch {
    throw new Error('cannot_reach_relay')
  }
  if (finRes.status === 409) throw new Error('email_taken')
  if (!finRes.ok) throw new Error('http_' + finRes.status)
  const fin = (await finRes.json()) as { user_id: string; session_token: string }
  return { user_id: fin.user_id, session_token: fin.session_token, account_key: accountKey }
}

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

// In-memory cache of the unlocked account_key. Populated synchronously
// after every login / register and at platform-init time after a
// secureStorage read. The Sealed / SessionInfo decrypt paths in
// connection.ts need a synchronous getter, but secureStorage is
// async; the cache bridges that gap.
let cachedAccountKey: Uint8Array | null = null

function setCachedAccountKey(bytes: Uint8Array | null): void {
  cachedAccountKey = bytes && bytes.length > 0 ? bytes : null
}

// Best-effort one-shot bootstrap: read the persisted key on platform
// creation so an already-logged-in user can decrypt sealed META on
// the first session attach without waiting for an explicit re-login.
async function bootstrapCachedAccountKey(): Promise<void> {
  try {
    const v = await secureStorage.get(ACCOUNT_KEY_KEY)
    if (v) setCachedAccountKey(b64StdToBytes(v))
  } catch {
    // ignore — the user can re-login if it matters
  }
}

export function createCapacitorPlatform(): Platform {
  setAccountKeyProvider(() => cachedAccountKey)
  void bootstrapCachedAccountKey()
  return {
    caps: {
      localPty: false,
      autoUpdate: false,
      pluginHost: false,
      windowControls: false,
      systemClipboard: true,
      notifications: true,
      fileDialog: false,
      wailsBindings: false,
      capacitor: true,
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
        const fromLocal = loadLegacyFromLocalStorage()
        if (fromLocal) return fromLocal

        const fromSecure = await Promise.race([
          secureStorage.get(STORAGE_KEY).catch(() => null),
          new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000)),
        ]).then(parseRelayJSON)
        if (fromSecure) {
          if (typeof localStorage !== 'undefined') {
            try { localStorage.setItem(STORAGE_KEY, JSON.stringify(fromSecure)) } catch {}
          }
          return fromSecure
        }
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
        const json = JSON.stringify(cfg)
        secureStorage.set(STORAGE_KEY, json).catch((e) => {
          logWarn("capacitor", "keychain set failed; relay config saved to localStorage only", { error: errText(e) })
        })
        if (typeof localStorage !== 'undefined') {
          localStorage.setItem(STORAGE_KEY, json)
        }
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
      consumePairing: async (relayBase, token, wrapKey?: Uint8Array) => {
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
        // The relay server does not actually return relay_url (a leftover
        // from when /api/pair/consume was going to redirect pairing to a
        // different host). Use the base we just called as the relay URL —
        // it is by definition the relay we want to talk to.
        const body = resp.data as {
          session_token: string
          expires_at: number
          user: { id: string; email: string }
          realm_id?: string
          home_instance_url?: string
          wrap?: string
        }

        // wrap (present when the pairing desktop had an unlocked account_key
        // and the QR carried a &k= wrap key) lets the mobile app decrypt
        // session details (cwd/title/command, live META) without ever
        // running the OPAQUE password flow. A missing wrapKey, missing
        // wrap, or a decrypt failure must never fail the pair — it just
        // means session details stay sealed until the user logs in with
        // their password instead.
        if (body.wrap && wrapKey) {
          try {
            const ak = openAccountKeyWrap(b64StdToBytes(body.wrap), wrapKey)
            if (ak) {
              await secureStorage.set(ACCOUNT_KEY_KEY, bytesToB64Std(ak))
              setCachedAccountKey(ak)
            } else {
              logWarn("capacitor", "pair: wrap decrypt failed; session details will not decrypt")
            }
          } catch (e) {
            logWarn("capacitor", "pair: wrap open threw", { error: errText(e) })
          }
        }

        return {
          relay_url: base,
          session_token: body.session_token,
          expires_at: body.expires_at,
          user: body.user,
          realm_id: body.realm_id ?? '',
          home_instance_url: body.home_instance_url ?? '',
        }
      },
      login: async (url, email, password, allowInsecure) => {
        const base = url.replace(/\/$/, '')
        const result = await opaqueLogin(base, email, password)
        const cfg: RelayConfig = {
          url: base,
          token: result.session_token,
          // The OPAQUE login response no longer carries an expiry;
          // the relay is authoritative and surfaces it via 401 when
          // the token rolls. Store 0 so the existing UI treats it as
          // "unknown" rather than "expired".
          session_expires_at: 0,
          allow_insecure_relay: allowInsecure,
          remote_permission: 'full',
          last_email: email,
          connected: false,
          realmId: result.realm_id,
          homeInstanceURL: result.home_instance_url,
        }
        await secureStorage.set(STORAGE_KEY, JSON.stringify(cfg))
        if (typeof localStorage !== 'undefined') {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg))
        }
        await secureStorage.set(PASSWORD_KEY, password)
        // Persist the unlocked account_key to the same secure-storage
        // backend the session token uses. AttermSecureStorage stores
        // these in the iOS Keychain (see PR #101); when the app
        // relaunches we re-read it without prompting the user again.
        await secureStorage.set(ACCOUNT_KEY_KEY, bytesToB64Std(result.account_key))
        setCachedAccountKey(result.account_key)
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
        if (typeof localStorage !== 'undefined') {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(cleared))
        }
        // Wipe the unlocked account_key alongside the session token.
        // Leaving it persisted after an explicit logout would let any
        // other process holding the Keychain item recover the user's
        // E2EE state.
        try { await secureStorage.set(ACCOUNT_KEY_KEY, '') } catch {}
        setCachedAccountKey(null)
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
        // Attach-only client: closing a tab detaches the local WS. It does
        // NOT kill the remote PTY — that stays owned by the host that started it.
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
          unread?: boolean; attention_at?: number; sealed?: string;
        }>
        // M3b: decrypt SessionInfo.Sealed when an account_key is in
        // secureStorage. Mirrors web/src/shared/api/sessions.ts. On any
        // cipher failure we silently keep the plaintext fields that
        // the additive-rollout agent still ships.
        const accountKeyB64 = await secureStorage.get(ACCOUNT_KEY_KEY)
        const accountKey = accountKeyB64 ? b64StdToBytes(accountKeyB64) : null
        return raw.map((s) => {
          let title = s.title
          let cwd = s.cwd
          let command = s.command
          let currentCommand = s.current_command
          if (accountKey && s.sealed) {
            try {
              const env = b64StdToBytes(s.sealed)
              const fields = openSessionFields(env, accountKey, s.id)
              if (fields) {
                if (fields.title !== undefined) title = fields.title
                if (fields.cwd !== undefined) cwd = fields.cwd
                if (fields.command !== undefined) command = fields.command
                if (fields.current_command !== undefined) currentCommand = fields.current_command
              }
            } catch {
              // ignore — fall back to plaintext
            }
          }
          s = { ...s, title, cwd, command, current_command: currentCommand }
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
      getPins: async () => {
        try {
          const raw = localStorage.getItem('atterm.pinned_session_ids.value')
          if (raw === null) return []
          const v = JSON.parse(raw)
          return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
        } catch {
          return []
        }
      },
      setPins: async (ids) => {
        localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(ids))
        notifyLocalChange('pinned_session_ids')
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
      // No Wails binding to ask on mobile; the app version is only shown
      // for desktop builds today.
      getAppVersion: async () => 'dev',
      hideSoftKeyboard: async () => {
        try {
          await Keyboard.hide()
        } catch {
          // Some platforms reject when no software keyboard is visible.
        }
      },
      // window* + pickLogFilePath omitted — desktop-only
    },
    events: createEventBus(),
    templates: {
      load: async () => {
        if (typeof localStorage === 'undefined') return []
        return parseQuickTemplateList(localStorage.getItem(SYNCED_TEMPLATES_KEY))
          ?? parseQuickTemplateList(localStorage.getItem(TEMPLATES_KEY))
          ?? []
      },
      save: async (list) => {
        if (typeof localStorage === 'undefined') return
        localStorage.setItem(TEMPLATES_KEY, JSON.stringify(list))
        localStorage.setItem(SYNCED_TEMPLATES_KEY, JSON.stringify(list))
        notifyLocalChange('quick_templates')
      },
      clear: async () => {
        if (typeof localStorage === 'undefined') return
        localStorage.removeItem(TEMPLATES_KEY)
        localStorage.setItem(SYNCED_TEMPLATES_KEY, JSON.stringify([]))
        notifyLocalChange('quick_templates')
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
