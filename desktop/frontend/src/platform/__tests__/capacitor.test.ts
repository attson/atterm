import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createCapacitorPlatform } from '../capacitor'

describe('createCapacitorPlatform', () => {
  beforeEach(() => {
    localStorage.clear()
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

  it('relay.save persists to localStorage under atterm.relay and load reads it back', async () => {
    const p = createCapacitorPlatform()
    const cfg = {
      url: 'https://relay.example.com', token: 'atk_xyz',
      allow_insecure_relay: false, remote_permission: 'full', connected: false,
    }
    await p.relay.save(cfg)
    expect(JSON.parse(localStorage.getItem('atterm.relay')!)).toMatchObject({ url: cfg.url, token: cfg.token })
    expect(await p.relay.load()).toMatchObject({ url: cfg.url, token: cfg.token })
  })

  it('relay.load returns null on malformed JSON', async () => {
    localStorage.setItem('atterm.relay', '{not json')
    const p = createCapacitorPlatform()
    expect(await p.relay.load()).toBeNull()
  })

  it('relay.clear removes the localStorage entry', async () => {
    const p = createCapacitorPlatform()
    await p.relay.save({ url: 'https://r', token: 'atk_x', allow_insecure_relay: false, remote_permission: 'full', connected: false })
    await p.relay.clear()
    expect(localStorage.getItem('atterm.relay')).toBeNull()
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

  it('sessions.listShells + listRemoteSessions return empty arrays', async () => {
    const p = createCapacitorPlatform()
    expect(await p.sessions.listShells()).toEqual([])
    expect(await p.sessions.listRemoteSessions()).toEqual([])
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
})
