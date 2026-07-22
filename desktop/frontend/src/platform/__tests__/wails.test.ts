import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock lib/api before the impl import so createWailsPlatform sees the mocks.
vi.mock('../../lib/api', () => ({
  getRelayConfig: vi.fn().mockResolvedValue({ url: 'https://r', token: 'atk_x', session_expires_at: 0, allow_insecure_relay: false, remote_permission: 'full', connected: false }),
  setRelayConfig: vi.fn().mockResolvedValue(undefined),
  setUplinkPaused: vi.fn().mockResolvedValue(undefined),
  fetchRelayMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
  getClipboardPastePayload: vi.fn().mockResolvedValue({ kind: 'none' }),
  showNotification: vi.fn().mockResolvedValue(undefined),
  pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
  newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
  closeSession: vi.fn().mockResolvedValue(undefined),
  listShells: vi.fn().mockResolvedValue(['/bin/zsh']),
  getUpdateState: vi.fn().mockResolvedValue({ state: 'idle' }),
  checkUpdate: vi.fn().mockResolvedValue(undefined),
  startDownload: vi.fn().mockResolvedValue(undefined),
  downloadVersion: vi.fn().mockResolvedValue(undefined),
  installUpdate: vi.fn().mockResolvedValue(undefined),
  markSessionsSeen: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(() => () => {}),
  EventsEmit: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowShow: vi.fn(),
  WindowUnminimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(true),
  Quit: vi.fn(),
  Environment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
  BrowserOpenURL: vi.fn(),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetPluginConfig: vi.fn().mockResolvedValue({ enabled_plugins: [] }),
  SetPluginConfig: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/go/main/PluginFS', () => ({
  ListDir: vi.fn().mockResolvedValue([]),
  WatchDir: vi.fn().mockResolvedValue(undefined),
  UnwatchDir: vi.fn().mockResolvedValue(undefined),
  ReadFile: vi.fn().mockResolvedValue({ path: '/x', data: [], isBinary: false }),
  FileMeta: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isDir: false, exists: true }),
}))

import { createWailsPlatform } from '../wails'
import { WindowMinimise, WindowShow, WindowUnminimise, Environment, BrowserOpenURL, EventsOn, EventsEmit } from '../../../wailsjs/runtime/runtime'
import { GetPluginConfig, SetPluginConfig } from '../../../wailsjs/go/main/App'
import { ListDir, ReadFile } from '../../../wailsjs/go/main/PluginFS'
import { fetchRelayMe, showNotification, setUplinkPaused, markSessionsSeen } from '../../lib/api'

describe('createWailsPlatform', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('caps has all desktop flags true', () => {
    const p = createWailsPlatform()
    expect(p.caps).toEqual({
      localPty: true, autoUpdate: true, pluginHost: true, windowControls: true,
      systemClipboard: true, notifications: true, fileDialog: true,
    })
  })

  it('relay.fetchMe delegates to lib/api fetchRelayMe', async () => {
    const p = createWailsPlatform()
    const me = await p.relay.fetchMe()
    expect(fetchRelayMe).toHaveBeenCalledOnce()
    expect(me).toEqual({ user_id: 'u', email: 'e' })
  })

  it('relay.setUplinkPaused delegates', async () => {
    const p = createWailsPlatform()
    await p.relay.setUplinkPaused!(true)
    expect(setUplinkPaused).toHaveBeenCalledWith(true)
  })

  it('sessions.markSessionsSeen delegates to api.markSessionsSeen', async () => {
    const p = createWailsPlatform()
    await p.sessions.markSessionsSeen!({ ids: ['s1'] })
    expect(markSessionsSeen).toHaveBeenCalledWith({ ids: ['s1'] })
  })

  it('system.showNotification delegates', async () => {
    const p = createWailsPlatform()
    await p.system.showNotification('t', 'b')
    expect(showNotification).toHaveBeenCalledWith('t', 'b')
  })

  it('system.windowMinimize delegates to runtime WindowMinimise', async () => {
    const p = createWailsPlatform()
    await p.system.windowMinimize!()
    expect(WindowMinimise).toHaveBeenCalledOnce()
  })

  it('system.windowShow delegates to runtime WindowShow', async () => {
    const p = createWailsPlatform()
    await p.system.windowShow!()
    expect(WindowShow).toHaveBeenCalledOnce()
  })

  it('system.windowUnminimize delegates to runtime WindowUnminimise', async () => {
    const p = createWailsPlatform()
    await p.system.windowUnminimize!()
    expect(WindowUnminimise).toHaveBeenCalledOnce()
  })

  it('system.getEnvironment returns the EnvironmentInfo from runtime', async () => {
    const p = createWailsPlatform()
    const env = await p.system.getEnvironment()
    expect(Environment).toHaveBeenCalledOnce()
    expect(env).toEqual({ platform: 'darwin', arch: 'arm64', buildType: 'production' })
  })

  it('system.openExternalURL delegates to BrowserOpenURL', async () => {
    const p = createWailsPlatform()
    await p.system.openExternalURL('https://example.com')
    expect(BrowserOpenURL).toHaveBeenCalledWith('https://example.com')
  })

  it('events.on subscribes via EventsOn and returns the unsubscribe', () => {
    // createWailsPlatform calls EventsOn once for 'account-key:changed'
    // during construction (M5-meta-wails). Construct first, then prime
    // the next EventsOn return so the assertion lands on the test's
    // own subscription, not the platform's.
    const p = createWailsPlatform()
    const off = vi.fn()
    ;(EventsOn as ReturnType<typeof vi.fn>).mockReturnValueOnce(off)
    const handler = vi.fn()
    const u = p.events.on('relay:auth-error', handler)
    expect(EventsOn).toHaveBeenCalledWith('relay:auth-error', handler)
    expect(u).toBe(off)
  })

  it('events.emit delegates to EventsEmit', () => {
    const p = createWailsPlatform()
    p.events.emit('foo', { x: 1 })
    expect(EventsEmit).toHaveBeenCalledWith('foo', { x: 1 })
  })

  it('pluginHost.getPluginConfig delegates to App.GetPluginConfig', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.getPluginConfig()
    expect(GetPluginConfig).toHaveBeenCalledOnce()
  })

  it('pluginHost.fs.listDir delegates to PluginFS.ListDir', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.listDir('/tmp')
    expect(ListDir).toHaveBeenCalledWith('/tmp')
  })

  it('pluginHost.fs.readFile delegates to PluginFS.ReadFile', async () => {
    const p = createWailsPlatform()
    await p.pluginHost!.fs.readFile('/tmp/x')
    expect(ReadFile).toHaveBeenCalledWith('/tmp/x')
  })
})
