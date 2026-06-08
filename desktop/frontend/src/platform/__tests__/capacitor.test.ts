import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createCapacitorPlatform } from '../capacitor'
import { secureStorage } from '../secureStorage'

describe('createCapacitorPlatform', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay')
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
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await p.relay.save(cfg)
    expect(JSON.parse((await secureStorage.get('atterm.relay'))!)).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await p.relay.load()).toMatchObject({ url: cfg.url, token: cfg.token })
  })

  it('relay.load returns null on malformed JSON in secureStorage', async () => {
    await secureStorage.set('atterm.relay', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.clear removes both storage backends', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r', token: 'atk_x', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    await p.relay.clear()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
    expect(await secureStorage.get('atterm.relay')).toBeNull()
  })

  it('relay.fetchMe GETs base/api/me with Bearer + credentials omit', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bear', allow_insecure_relay: false, remote_permission: 'full', connected: false })
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
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_t', allow_insecure_relay: false, remote_permission: 'full', connected: false })
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
    await p.relay.save({ url: 'https://r.example.com', token: 'atk_bad', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    await expect(p.sessions.listRemoteSessions()).rejects.toThrow(/relay_unauthorized/)
  })
})

describe('createCapacitorPlatform — secure storage migration', () => {
  beforeEach(async () => {
    localStorage.clear()
    await secureStorage.remove('atterm.relay')
  })

  it('migrates from localStorage to secureStorage on first load, then clears localStorage', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_legacy',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    localStorage.setItem('atterm.relay', JSON.stringify(cfg))
    expect(await secureStorage.get('atterm.relay')).toBeNull()

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await secureStorage.get('atterm.relay')).not.toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })

  it('prefers secureStorage over localStorage when both are present', async () => {
    const fromSecure = {
      url: 'https://secure.example.com', token: 'atk_secure',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    const fromLocal = {
      url: 'https://local.example.com', token: 'atk_local',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await secureStorage.set('atterm.relay', JSON.stringify(fromSecure))
    localStorage.setItem('atterm.relay', JSON.stringify(fromLocal))

    const p = createCapacitorPlatform()
    const loaded = await p.relay.load()

    expect(loaded).toMatchObject({ url: fromSecure.url, token: fromSecure.token })
    // localStorage was not the source; we don't touch it on this path.
    expect(localStorage.getItem('atterm.relay')).not.toBeNull()
  })

  it('returns null when both stores are empty', async () => {
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('save writes only to secureStorage, not localStorage', async () => {
    const cfg = {
      url: 'https://r.example.com', token: 'atk_x',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    const p = createCapacitorPlatform()
    await p.relay.save(cfg)
    expect(await secureStorage.get('atterm.relay')).not.toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
  })

  it('clear wipes both stores (defensive)', async () => {
    const cfg = {
      url: 'https://r', token: 'atk_x',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await secureStorage.set('atterm.relay', JSON.stringify(cfg))
    localStorage.setItem('atterm.relay', JSON.stringify(cfg))

    const p = createCapacitorPlatform()
    await p.relay.clear()

    expect(await secureStorage.get('atterm.relay')).toBeNull()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
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

  it('templates.clear removes the key', async () => {
    const p = createCapacitorPlatform()
    await p.templates.save([{ id: 'x', label: 'x', text: 'x' }])
    await p.templates.clear()
    expect(localStorage.getItem('atterm.templates')).toBeNull()
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
    await secureStorage.remove('atterm.relay')
    localStorage.clear()
    localStorage.setItem('atterm.relay', STORED_RELAY)
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
    await secureStorage.remove('atterm.relay')
    localStorage.clear()
    localStorage.setItem('atterm.relay', STORED_RELAY)
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
