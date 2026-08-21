import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// The OPAQUE protocol runs in the bytemare WASM client (browser/WebView-only
// loader); stub it so these tests exercise capacitor.ts's HTTP/endpoint wiring
// and error mapping without a real wasm instance. Real protocol interop is
// covered by web/tests/unit/opaque-interop.test.ts (same wasm binary).
vi.mock('../../lib/opaqueWasm', () => ({
  opaqueLoginInit: vi.fn(async () => ({ handle: 1, ke1: 'a2Ux' })),
  opaqueLoginFinish: vi.fn(async () => ({ ke3: 'a2Uz', exportKey: '', sessionKey: '' })),
  opaqueRegisterInit: vi.fn(async () => ({ handle: 1, ke1: 'a2Ux' })),
  opaqueRegisterFinish: vi.fn(async () => ({ record: 'cmVj', exportKey: '' })),
}))

// @capacitor/core's CapacitorHttp is a registerPlugin() Proxy: every property
// read (even after a plain `CapacitorHttp.post = fn` assignment) goes through
// a `get` trap that ignores the underlying target and dynamically resolves
// the web plugin implementation instead — so it cannot be stubbed by mutating
// the export. Replace just CapacitorHttp with a plain mock object; keep every
// other export (registerPlugin, Capacitor, …) real since secureStorage.ts's
// Keychain plugin registration depends on them.
vi.mock('@capacitor/core', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@capacitor/core')>()
  return { ...actual, CapacitorHttp: { post: vi.fn() } }
})

import { CapacitorHttp } from '@capacitor/core'
import { createCapacitorPlatform } from '../capacitor'
import { secureStorage } from '../secureStorage'
import { wrapAccountKey } from '../../lib/opaque'
import { TYPE, NIL_SID, encodeFrame, decodeFrame, encodeText, decodeText } from '../../lib/proto'

describe('createCapacitorPlatform', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    vi.restoreAllMocks()
  })

  it('caps disables all desktop-only flags', () => {
    const p = createCapacitorPlatform()
    expect(p.caps).toEqual({
      localPty: false,
      autoUpdate: false,
      pluginHost: false,
      windowControls: false,
      systemClipboard: true,
      notifications: true,
      fileDialog: false,
      wailsBindings: false,
      capacitor: true,
    })
  })

  it('omits updater and pluginHost bridges', () => {
    const p = createCapacitorPlatform()
    expect(p.updater).toBeUndefined()
    expect(p.pluginHost).toBeUndefined()
  })

  it('omits window control + log-file system methods', () => {
    const p = createCapacitorPlatform()
    expect(p.system.windowMinimize).toBeUndefined()
    expect(p.system.windowToggleMaximize).toBeUndefined()
    expect(p.system.windowIsMaximized).toBeUndefined()
    expect(p.system.quit).toBeUndefined()
    expect(p.system.pickLogFilePath).toBeUndefined()
  })

  it('omits sessions.newSession and relay.setUplinkPaused', () => {
    const p = createCapacitorPlatform()
    expect(p.sessions.newSession).toBeUndefined()
    expect(p.relay.setUplinkPaused).toBeUndefined()
  })

  it('relay.load returns null when nothing stored', async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.save persists to secureStorage under atterm.relay and load reads it back', async () => {
    const p = createCapacitorPlatform()
    const cfg = {
      url: 'https://relay.example.com', token: 'atk_xyz',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    await p.relay.save(cfg)
    expect(JSON.parse((await secureStorage.get('atterm.relay.session'))!)).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await p.relay.load()).toMatchObject({ url: cfg.url, token: cfg.token })
  })

  it('relay.load returns null on malformed JSON in secureStorage', async () => {
    await secureStorage.set('atterm.relay.session', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.clear removes both storage backends', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r', token: 'atk_x', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false })
    await p.relay.clear()
    expect(localStorage.getItem('atterm.relay.session')).toBeNull()
    expect(await secureStorage.get('atterm.relay.session')).toBeNull()
  })

  it('relay.fetchMe GETs base/api/me with Bearer + credentials omit', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bear', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false })
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1', email: 'e@x' }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    )
    vi.stubGlobal('fetch', fetchMock)
    const me = await p.relay.fetchMe()
    expect(me).toEqual({ user_id: 'u1', email: 'e@x' })
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/me')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_bear')
    expect((init as RequestInit).credentials).toBe('omit')
  })

  it('relay.fetchMe throws relay_not_configured when no config stored', async () => {
    const p = createCapacitorPlatform()
    await expect(p.relay.fetchMe()).rejects.toThrow(/relay_not_configured/i)
  })

  it('sessions.listShells returns empty array', async () => {
    const p = createCapacitorPlatform()
    expect(await p.sessions.listShells()).toEqual([])
  })

  it('sessions.closeSession is a no-op placeholder', async () => {
    const p = createCapacitorPlatform()
    await expect(p.sessions.closeSession('s1')).resolves.toBeUndefined()
  })

  it('getPins reads pinned_session_ids from localStorage', async () => {
    localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(['a', 'b']))
    const p = createCapacitorPlatform()
    await expect(p.sessions.getPins()).resolves.toEqual(['a', 'b'])
  })

  it('setPins writes localStorage + calls notifyLocalChange', async () => {
    const notify = vi.spyOn(await import('../../lib/prefsSync.capacitor'), 'notifyLocalChange')
    const p = createCapacitorPlatform()
    await p.sessions.setPins(['x'])
    expect(localStorage.getItem('atterm.pinned_session_ids.value')).toBe('["x"]')
    expect(notify).toHaveBeenCalledWith('pinned_session_ids')
  })

  it('system.openExternalURL calls window.open in a new tab', async () => {
    const p = createCapacitorPlatform()
    const open = vi.fn()
    vi.stubGlobal('open', open) // window.open is globalThis.open in happy-dom
    await p.system.openExternalURL('https://example.com')
    expect(open).toHaveBeenCalledWith('https://example.com', '_blank')
  })

  it('system.getEnvironment returns the Capacitor environment shape', async () => {
    const p = createCapacitorPlatform()
    expect(await p.system.getEnvironment()).toEqual({ buildType: 'capacitor', platform: 'ios', arch: 'arm64' })
  })

  it('system.getClipboardPaste returns kind=none placeholder', async () => {
    const p = createCapacitorPlatform()
    expect(await p.system.getClipboardPaste()).toEqual({ kind: 'none' })
  })

  it('system.showNotification resolves (native plugin lands PR-D)', async () => {
    const p = createCapacitorPlatform()
    await expect(p.system.showNotification('t', 'b')).resolves.toBeUndefined()
  })

  it("system.getAppVersion always resolves 'dev' (no Wails binding to ask)", async () => {
    const p = createCapacitorPlatform()
    await expect(p.system.getAppVersion()).resolves.toBe('dev')
  })

  it('events.on/emit invoke handlers in order; off unsubscribes', () => {
    const p = createCapacitorPlatform()
    const calls: unknown[] = []
    const off1 = p.events.on('x', (d) => calls.push(['a', d]))
    p.events.on('x', (d) => calls.push(['b', d]))
    p.events.emit('x', { n: 1 })
    expect(calls).toEqual([['a', { n: 1 }], ['b', { n: 1 }]])
    off1()
    p.events.emit('x', { n: 2 })
    expect(calls).toEqual([['a', { n: 1 }], ['b', { n: 1 }], ['b', { n: 2 }]])
  })

  it('listRemoteSessions GETs base/api/sessions with Bearer and maps SessionInfo→RemoteSession', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_t', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false })
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify([
      { id: 's1', command: 'bash', cwd: '/', title: '', cols: 80, rows: 24, started_at: 0, host_id: 'h1', host: 'box', user: 'me' },
      { id: 's2', command: 'zsh', cwd: '/', title: 'claude', cols: 100, rows: 30, started_at: 0, host_id: 'h1', host: 'box', user: 'me' },
    ]), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const sessions = await p.sessions.listRemoteSessions()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/sessions')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer atk_t')
    expect((init as RequestInit).credentials).toBe('omit')
    expect(sessions).toEqual([
      { session_id: 's1', host_id: 'h1', host: 'box', user: 'me', title: 'bash', cwd: '/', cols: 80, rows: 24 },
      { session_id: 's2', host_id: 'h1', host: 'box', user: 'me', title: 'claude', cwd: '/', cols: 100, rows: 30 },
    ])
  })

  it('listRemoteSessions returns [] when no config', async () => {
    const p = createCapacitorPlatform()
    expect(await p.sessions.listRemoteSessions()).toEqual([])
  })

  it('listRemoteSessions throws relay_unauthorized on 401', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bad', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    await expect(p.sessions.listRemoteSessions()).rejects.toThrow(/relay_unauthorized/)
  })
})

describe('createCapacitorPlatform — relay.login', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    await secureStorage.remove('atterm.relay.password')
    await secureStorage.remove('atterm.relay.account-key')
    vi.restoreAllMocks()
  })

  // After M1h the mobile/capacitor login flow goes through OPAQUE's
  // two-stage handshake. Full protocol correctness is exercised in the
  // shared opaque-interop.test.ts in web/; these tests only assert
  // that capacitor.login wires the right OPAQUE endpoints in the right
  // order and maps the documented HTTP error codes to the same
  // string-typed errors the mobile UI already handles.
  it('drives /api/auth/login/init then /finalize against the relay', async () => {
    const fetchMock = vi.fn()
      // /init returns a session_id + a base64 KE2 — the actual bytes
      // are garbage because the TS client's MAC check will fail
      // (different key material than the server would produce). Test
      // catches the rejected promise; we only assert which endpoints
      // got hit and in which order.
      .mockResolvedValueOnce(new Response(JSON.stringify({
        login_response: 'AAAA',
        session_id: 'sid-xyz',
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const p = createCapacitorPlatform()
    await p.relay.login!('https://r.example.com', 'me@example.com', 'pw', false).catch(() => undefined)

    expect(fetchMock).toHaveBeenCalled()
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/auth/login/init')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).credentials).toBe('omit')
    const body = JSON.parse((init as RequestInit).body as string)
    expect(body.email).toBe('me@example.com')
    expect(typeof body.login_ke).toBe('string')
    expect(body.login_ke.length).toBeGreaterThan(0)
  })

  it('successful login mirrors relay config into localStorage for the fast load path', async () => {
    const accountKey = new Uint8Array(32)
    accountKey[0] = 7
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        login_response: 'AAAA',
        session_id: 'sid-xyz',
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        user_id: 'user-1',
        session_token: 'sess_new',
        account_key_wrap: wrapAccountKey('pw', accountKey, { alg: 'argon2id', m: 32, t: 1, p: 1 }),
        realm_id: 'realm-1',
        home_instance_url: 'https://home.example.com',
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const p = createCapacitorPlatform()
    await p.relay.login!('https://r.example.com', 'me@example.com', 'pw', false)

    const fromSecure = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    const fromLocal = JSON.parse(localStorage.getItem('atterm.relay.session')!)
    expect(fromSecure.token).toBe('sess_new')
    expect(fromLocal).toEqual(fromSecure)
    expect(fromLocal.realmId).toBe('realm-1')
    expect(fromLocal.homeInstanceURL).toBe('https://home.example.com')
  })

  it('maps 401 on /init to invalid_credentials and leaves storage untouched', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('invalid_credentials')
    expect(await secureStorage.get('atterm.relay.session')).toBeNull()
    expect(await secureStorage.get('atterm.relay.password')).toBeNull()
    expect(await secureStorage.get('atterm.relay.account-key')).toBeNull()
  })

  it('maps 429 to rate_limited', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 429 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('rate_limited')
  })

  it('maps 5xx to http_<status>', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('http_500')
  })

  it('maps a network failure to cannot_reach_relay', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')))
    const p = createCapacitorPlatform()
    await expect(p.relay.login!('https://r', 'a@b', 'pw', false)).rejects.toThrow('cannot_reach_relay')
  })
})

describe('createCapacitorPlatform — relay.logout', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    await secureStorage.remove('atterm.relay.password')
    await secureStorage.remove('atterm.relay.account-key')
    vi.restoreAllMocks()
  })

  it('POSTs /api/auth/logout with Bearer, clears local token, preserves email + password', async () => {
    await secureStorage.set('atterm.relay.session', JSON.stringify({
      url: 'https://r.example.com',
      token: 'sess_old',
      session_expires_at: 99,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: 'me@example.com',
      connected: false,
    }))
    await secureStorage.set('atterm.relay.password', 'hunter2hunter2')
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    const p = createCapacitorPlatform()
    await p.relay.logout!()

    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('https://r.example.com/api/auth/logout')
    expect((init as RequestInit).method).toBe('POST')
    expect(new Headers((init as RequestInit).headers).get('Authorization')).toBe('Bearer sess_old')
    expect((init as RequestInit).credentials).toBe('omit')

    const saved = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    const savedLocal = JSON.parse(localStorage.getItem('atterm.relay.session')!)
    expect(saved.token).toBe('')
    expect(saved.session_expires_at).toBe(0)
    expect(saved.url).toBe('https://r.example.com')
    expect(saved.last_email).toBe('me@example.com')
    expect(saved.allow_insecure_relay).toBe(false)
    expect(saved.remote_permission).toBe('full')
    expect(savedLocal).toEqual(saved)
    expect(await secureStorage.get('atterm.relay.password')).toBe('hunter2hunter2')
  })

  it('swallows network errors but still clears the local token', async () => {
    await secureStorage.set('atterm.relay.session', JSON.stringify({
      url: 'https://r.example.com',
      token: 'sess_old',
      session_expires_at: 99,
      allow_insecure_relay: false,
      remote_permission: 'full',
      last_email: 'me@example.com',
      connected: false,
    }))
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network down')))

    const p = createCapacitorPlatform()
    await expect(p.relay.logout!()).resolves.toBeUndefined()
    const saved = JSON.parse((await secureStorage.get('atterm.relay.session'))!)
    expect(saved.token).toBe('')
  })

  it('is a no-op when no config is stored', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const p = createCapacitorPlatform()
    await expect(p.relay.logout!()).resolves.toBeUndefined()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('createCapacitorPlatform — relay.loadSavedPassword', () => {
  beforeEach(async () => {
    await secureStorage.remove('atterm.relay.password')
  })

  it("returns '' when nothing is stored", async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.loadSavedPassword!()).toBe('')
  })

  it('returns the value previously written by login', async () => {
    await secureStorage.set('atterm.relay.password', 'hunter2hunter2')
    const p = createCapacitorPlatform()
    expect(await p.relay.loadSavedPassword!()).toBe('hunter2hunter2')
  })
})

describe('createCapacitorPlatform — secure storage migration', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
  })

  it('reads localStorage synchronously when it has a config (does not touch Keychain)', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_legacy',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    localStorage.setItem('atterm.relay.session', JSON.stringify(cfg))
    expect(await secureStorage.get('atterm.relay.session')).toBeNull()

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: cfg.url, token: cfg.token })
    // localStorage stays as the canonical source — load never wrote to
    // Keychain on this path (avoids the bridge-hang risk).
    expect(localStorage.getItem('atterm.relay.session')).not.toBeNull()
  })

  it('falls back to Keychain when localStorage is empty, then mirrors back into localStorage', async () => {
    const fromSecure = {
      url: 'https://secure.example.com', token: 'atk_secure',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    await secureStorage.set('atterm.relay.session', JSON.stringify(fromSecure))
    expect(localStorage.getItem('atterm.relay.session')).toBeNull()

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: fromSecure.url, token: fromSecure.token })
    // After a Keychain-only read, load() mirrors back so the next boot uses
    // the fast localStorage path.
    expect(localStorage.getItem('atterm.relay.session')).not.toBeNull()
  })

  it('prefers localStorage when both stores are present (avoids Keychain bridge hangs)', async () => {
    const fromSecure = {
      url: 'https://secure.example.com', token: 'atk_secure',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    const fromLocal = {
      url: 'https://local.example.com', token: 'atk_local',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    await secureStorage.set('atterm.relay.session', JSON.stringify(fromSecure))
    localStorage.setItem('atterm.relay.session', JSON.stringify(fromLocal))

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    // localStorage wins — save() always writes localStorage, so it's the
    // canonical newest copy. Reading Keychain first risked hanging the boot
    // on the WebView's bridge state.
    expect(loaded).toMatchObject({ url: fromLocal.url, token: fromLocal.token })
  })

  it('returns null when both stores are empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('save writes to both localStorage (sync) and secureStorage (best-effort)', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_x',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    const p = createCapacitorPlatform()
    await p.relay.save(cfg)
    // localStorage commits synchronously so save() can return promptly even
    // when the Keychain bridge hangs (the "保存配置…" freeze pre-fix).
    expect(localStorage.getItem('atterm.relay.session')).not.toBeNull()
    // Keychain is best-effort — wait a tick for the fire-and-forget call.
    await new Promise((r) => setTimeout(r, 0))
    expect(await secureStorage.get('atterm.relay.session')).not.toBeNull()
  })

  it('clear wipes both stores (defensive)', async () => {
    const cfg = {
      url: 'https://r', token: 'atk_x',
      session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', last_email: '', connected: false,
    }
    await secureStorage.set('atterm.relay.session', JSON.stringify(cfg))
    localStorage.setItem('atterm.relay.session', JSON.stringify(cfg))

    const p = createCapacitorPlatform()
    await p.relay.clear()

    expect(await secureStorage.get('atterm.relay.session')).toBeNull()
    expect(localStorage.getItem('atterm.relay.session')).toBeNull()
  })
})

describe('createCapacitorPlatform — templates', () => {
  beforeEach(() => { localStorage.clear() })

  it('templates.load returns [] when localStorage is empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.templates.load()).toEqual([])
  })

  it('templates.save then load round-trips a list', async () => {
    const p = createCapacitorPlatform()
    const list = [{ id: 'a', label: 'A', text: 'a-text' }]
    await p.templates.save(list)
    expect(await p.templates.load()).toEqual(list)
  })

  it('templates.load prefers the synced quick_templates value over the legacy local key', async () => {
    localStorage.setItem('atterm.templates', JSON.stringify([{ id: 'old', label: 'old', text: 'old' }]))
    localStorage.setItem('atterm.quick_templates.value', JSON.stringify([{ id: 'new', label: 'new', text: 'new' }]))
    const p = createCapacitorPlatform()
    expect(await p.templates.load()).toEqual([{ id: 'new', label: 'new', text: 'new' }])
  })

  it('templates.clear resets the synced value so reset propagates cross-device', async () => {
    const p = createCapacitorPlatform()
    await p.templates.save([{ id: 'x', label: 'x', text: 'x' }])
    await p.templates.clear()
    expect(localStorage.getItem('atterm.templates')).toBeNull()
    expect(localStorage.getItem('atterm.quick_templates.value')).toBe('[]')
    expect(await p.templates.load()).toEqual([])
  })

  it('templates.load returns [] on malformed JSON', async () => {
    localStorage.setItem('atterm.templates', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.templates.load()).toEqual([])
  })

  it('auxKeys.load returns [] when localStorage is empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.auxKeys.load()).toEqual([])
  })

  it('auxKeys.save then load round-trips a list', async () => {
    const p = createCapacitorPlatform()
    const list = [{ id: 'aux-esc', label: 'esc', seq: '\x1b' }]
    await p.auxKeys.save(list)
    expect(await p.auxKeys.load()).toEqual(list)
  })

  it('auxKeys.clear removes the key', async () => {
    const p = createCapacitorPlatform()
    await p.auxKeys.save([{ id: 'aux-tab', label: 'tab', seq: '\t' }])
    await p.auxKeys.clear()
    expect(localStorage.getItem('atterm.auxkeys')).toBeNull()
    expect(await p.auxKeys.load()).toEqual([])
  })

  it('auxKeys.load returns [] on malformed JSON', async () => {
    localStorage.setItem('atterm.auxkeys', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.auxKeys.load()).toEqual([])
  })
})

// --- T5: list mapping + markSessionsSeen ---

const STORED_RELAY = JSON.stringify({ url: 'https://relay.example', token: 'atk_test' })

// capacitor.ts reads relay config from secureStorage first, then falls back to
// loadLegacyFromLocalStorage(). secureStorage uses an in-memory backend under
// vitest; we rely on that being empty and seed localStorage instead. Clearing
// both each test keeps tests independent.
describe('capacitor.listRemoteSessions', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(async () => {
    const { secureStorage } = await import('../secureStorage')
    await secureStorage.remove('atterm.relay.session')
    localStorage.clear()
    localStorage.setItem('atterm.relay.session', STORED_RELAY)
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => { vi.unstubAllGlobals() })

  it('maps unread and attention_at from the JSON payload', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => [{
        id: 's1', host_id: 'h', host: 'box', user: 'me',
        title: 'zsh', command: 'zsh', cols: 80, rows: 24,
        unread: true, attention_at: 1234567,
      }],
    } as unknown as Response)

    const p = createCapacitorPlatform()
    const list = await p.sessions.listRemoteSessions()
    expect(list).toHaveLength(1)
    expect(list[0].unread).toBe(true)
    expect(list[0].attention_at).toBe(1234567)
  })
})

describe('capacitor.markSessionsSeen', () => {
  let fetchMock: ReturnType<typeof vi.fn>
  beforeEach(async () => {
    const { secureStorage } = await import('../secureStorage')
    await secureStorage.remove('atterm.relay.session')
    localStorage.clear()
    localStorage.setItem('atterm.relay.session', STORED_RELAY)
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => { vi.unstubAllGlobals() })

  it('posts session_ids with Bearer auth', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 204,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await p.sessions.markSessionsSeen!({ ids: ['s1', 's2'] })

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('https://relay.example/api/sessions/seen')
    expect(init.method).toBe('POST')
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer atk_test')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(JSON.parse(init.body as string)).toEqual({ session_ids: ['s1', 's2'] })
  })

  it('posts {all: true} when called with { all: true }', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true, status: 204,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await p.sessions.markSessionsSeen!({ all: true })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({ all: true })
  })

  it('throws relay_unauthorized on HTTP 401', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false, status: 401,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await expect(p.sessions.markSessionsSeen!({ all: true })).rejects.toThrow('relay_unauthorized')
  })
})

describe('createCapacitorPlatform — relay.consumePairing', () => {
  // Same golden vector as desktop/frontend/src/lib/opaque.test.ts
  // (captured from desktop/wrap_account_key_test.go
  // TestWrapAccountKey_GoldenForTS) so this exercises a real Go-sealed
  // envelope, not just a TS round-trip.
  const AK = new Uint8Array(32).fill(0x42)
  const WK = new Uint8Array(32).fill(0x99)
  const WRAP_B64 =
    'AXd3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d0n14YBCoIjgFLbpQIXOw/BCPN4OFbqYLKyhS+9Vaqb9jy4iWn7LFub54SdU2lFXJg=='

  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay.session')
    await secureStorage.remove('atterm.relay.account-key')
    vi.mocked(CapacitorHttp.post).mockReset()
  })

  it('unwraps wrap and stores account_key in Keychain', async () => {
    vi.mocked(CapacitorHttp.post).mockResolvedValue({
      status: 200,
      data: {
        session_token: 'sess_pair',
        expires_at: 1735689600,
        user: { id: 'u1', email: 'alice@example.com' },
        realm_id: 'realm-1',
        home_instance_url: 'https://home.example.com',
        wrap: WRAP_B64,
      },
    } as unknown as Awaited<ReturnType<typeof CapacitorHttp.post>>)

    const p = createCapacitorPlatform()
    const result = await p.relay.consumePairing!('https://relay.example.com', 'pair_tok', WK)

    expect(result).toEqual({
      relay_url: 'https://relay.example.com',
      session_token: 'sess_pair',
      expires_at: 1735689600,
      user: { id: 'u1', email: 'alice@example.com' },
      realm_id: 'realm-1',
      home_instance_url: 'https://home.example.com',
    })
    const stored = await secureStorage.get('atterm.relay.account-key')
    expect(stored).not.toBeNull()
    const storedBytes = Uint8Array.from(atob(stored!), (c) => c.charCodeAt(0))
    expect(Array.from(storedBytes)).toEqual(Array.from(AK))
  })

  it('does not touch Keychain when wrap is absent', async () => {
    vi.mocked(CapacitorHttp.post).mockResolvedValue({
      status: 200,
      data: {
        session_token: 'sess_nowrap',
        expires_at: 0,
        user: { id: 'u1', email: 'alice@example.com' },
        realm_id: '',
        home_instance_url: '',
      },
    } as unknown as Awaited<ReturnType<typeof CapacitorHttp.post>>)

    const p = createCapacitorPlatform()
    const result = await p.relay.consumePairing!('https://relay.example.com', 'pair_tok', WK)

    expect(result.realm_id).toBe('')
    expect(result.home_instance_url).toBe('')
    expect(await secureStorage.get('atterm.relay.account-key')).toBeNull()
  })

  it('does not touch Keychain when wrap decrypt fails (wrong wrapKey)', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(CapacitorHttp.post).mockResolvedValue({
      status: 200,
      data: {
        session_token: 'sess_badwrap',
        expires_at: 0,
        user: { id: 'u1', email: 'alice@example.com' },
        realm_id: 'realm-1',
        home_instance_url: 'https://home.example.com',
        wrap: WRAP_B64,
      },
    } as unknown as Awaited<ReturnType<typeof CapacitorHttp.post>>)

    const wrongKey = new Uint8Array(32).fill(0xaa)
    const p = createCapacitorPlatform()
    const result = await p.relay.consumePairing!('https://relay.example.com', 'pair_tok', wrongKey)

    // Pair still succeeds; only the wrap-driven key install is skipped.
    expect(result.session_token).toBe('sess_badwrap')
    expect(await secureStorage.get('atterm.relay.account-key')).toBeNull()
    expect(warn).toHaveBeenCalled()
  })
})

// --- T6: createSessionWithProfile (the mobile "open with profile" flow) ---

describe('capacitor.createSessionWithProfile', () => {
  class FakeWebSocket {
    static instances: FakeWebSocket[] = []
    static CONNECTING = 0
    static OPEN = 1
    static CLOSED = 3

    readyState = FakeWebSocket.CONNECTING
    binaryType = ''
    sent: Uint8Array[] = []
    onopen: (() => void) | null = null
    onmessage: ((event: MessageEvent) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null

    constructor(public url: string, public protocols?: string[]) {
      FakeWebSocket.instances.push(this)
    }

    send(data: Uint8Array) {
      this.sent.push(data)
    }

    close() {
      if (this.readyState === FakeWebSocket.CLOSED) return
      this.readyState = FakeWebSocket.CLOSED
    }

    open() {
      this.readyState = FakeWebSocket.OPEN
      this.onopen?.()
    }

    // Simulates the relay delivering a TypeSessionCreated frame.
    emitCreated(payload: { request_id: string; ok: boolean; session_id?: string; error?: string }) {
      const json = encodeText(JSON.stringify(payload))
      const bytes = encodeFrame(TYPE.SESSION_CREATED, NIL_SID, json)
      this.onmessage?.({ data: bytes.buffer } as MessageEvent)
    }
  }

  beforeEach(async () => {
    const { secureStorage } = await import('../secureStorage')
    await secureStorage.remove('atterm.relay.session')
    localStorage.clear()
    localStorage.setItem('atterm.relay.session', STORED_RELAY)
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  function lastWS(): FakeWebSocket {
    return FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
  }

  it('sends TypeSessionCreate with NIL_SID and {request_id, host_id, profile_id}, dialed at wss://.../client', async () => {
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
    const ws = lastWS()
    expect(ws.url).toBe('wss://relay.example/client')
    ws.open()
    await vi.waitFor(() => expect(ws.sent).toHaveLength(1))

    const frame = decodeFrame(ws.sent[0])
    expect(frame.type).toBe(TYPE.SESSION_CREATE)
    expect(frame.sid).toEqual(NIL_SID)
    const payload = JSON.parse(decodeText(frame.payload)) as { request_id: string; host_id: string; profile_id: string }
    expect(payload.host_id).toBe('host-a')
    expect(payload.profile_id).toBe('profile-a')
    expect(payload.request_id).toEqual(expect.any(String))
    expect(payload.request_id.length).toBeGreaterThan(0)

    ws.emitCreated({ request_id: payload.request_id, ok: true, session_id: 's-new' })
    await expect(pending).resolves.toBe('s-new')
  })

  it('resolves with the session_id on ok:true', async () => {
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
    const ws = lastWS()
    ws.open()
    await vi.waitFor(() => expect(ws.sent).toHaveLength(1))
    const requestID = (JSON.parse(decodeText(decodeFrame(ws.sent[0]).payload)) as { request_id: string }).request_id
    ws.emitCreated({ request_id: requestID, ok: true, session_id: 's-ok' })
    await expect(pending).resolves.toBe('s-ok')
  })

  it('rejects with the raw wire error code on ok:false, unchanged', async () => {
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
    const ws = lastWS()
    ws.open()
    await vi.waitFor(() => expect(ws.sent).toHaveLength(1))
    const requestID = (JSON.parse(decodeText(decodeFrame(ws.sent[0]).payload)) as { request_id: string }).request_id
    ws.emitCreated({ request_id: requestID, ok: false, error: 'permission_denied' })
    await expect(pending).rejects.toThrow('permission_denied')
  })

  it('a mismatched request_id in the response is ignored (does not resolve or reject)', async () => {
    vi.useFakeTimers()
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.advanceTimersByTimeAsync(0)
    const ws = lastWS()
    ws.open()
    await vi.advanceTimersByTimeAsync(0)
    expect(ws.sent).toHaveLength(1)

    let settled = false
    pending.then(() => { settled = true }, () => { settled = true })
    ws.emitCreated({ request_id: 'not-the-real-one', ok: true, session_id: 's-wrong' })
    await Promise.resolve()
    await Promise.resolve()
    expect(settled).toBe(false)

    // The real response still resolves it — proves the connection was never
    // torn down by the bogus message.
    const requestID = (JSON.parse(decodeText(decodeFrame(ws.sent[0]).payload)) as { request_id: string }).request_id
    ws.emitCreated({ request_id: requestID, ok: true, session_id: 's-right' })
    await expect(pending).resolves.toBe('s-right')
  })

  it('rejects with Error("timeout") after 30s and sends exactly one request — no retry', async () => {
    vi.useFakeTimers()
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.advanceTimersByTimeAsync(0)
    const ws = lastWS()
    ws.open()
    await vi.advanceTimersByTimeAsync(0)
    expect(ws.sent).toHaveLength(1)

    const assertion = expect(pending).rejects.toThrow('timeout')
    await vi.advanceTimersByTimeAsync(30000)
    await assertion

    // Give any (incorrect) retry logic a chance to fire, then confirm it didn't.
    await vi.advanceTimersByTimeAsync(60000)
    expect(ws.sent).toHaveLength(1)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('rejects with relay_not_configured when no relay session is stored, without opening a socket', async () => {
    localStorage.clear()
    const p = createCapacitorPlatform()
    await expect(p.sessions.createSessionWithProfile!('host-a', 'profile-a')).rejects.toThrow('relay_not_configured')
    expect(FakeWebSocket.instances).toHaveLength(0)
  })

  it('rejects with upstream_unavailable when the socket closes before a response arrives', async () => {
    const p = createCapacitorPlatform()
    const pending = p.sessions.createSessionWithProfile!('host-a', 'profile-a')
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
    const ws = lastWS()
    ws.open()
    await vi.waitFor(() => expect(ws.sent).toHaveLength(1))
    ws.onclose?.()
    await expect(pending).rejects.toThrow('upstream_unavailable')
  })
})
