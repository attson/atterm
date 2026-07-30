import { vi } from 'vitest'
import type { EventBus, Platform, UpdateState } from '../types'
import type { main as Models } from '../../../wailsjs/go/models'

// A minimal real EventEmitter matching the EventBus interface, for tests that
// need on()/emit() to actually fire handlers (createFakePlatform().events is
// just vi.fn() stubs that don't wire on() through to emit()).
export function fakeEventBus(): EventBus {
  const handlers = new Map<string, Set<(data: unknown) => void>>()
  return {
    on(event, handler) {
      let set = handlers.get(event)
      if (!set) {
        set = new Set()
        handlers.set(event, set)
      }
      set.add(handler)
      return () => set!.delete(handler)
    },
    emit(event, data) {
      handlers.get(event)?.forEach((h) => h(data))
    },
  }
}

// Returns a fresh fake Platform with all bridges present and every method
// wired to vi.fn() returning a sensible default. Tests should override the
// specific methods they care about, e.g.:
//   const p = createFakePlatform()
//   p.system.windowMinimize = vi.fn().mockResolvedValue(undefined)
//   __setPlatformForTests(p)
export function createFakePlatform(): Platform {
  const fakeUpdateState: UpdateState = {
    current: '0.0.0',
    latest: '0.0.0',
    available: false,
    notes: '',
    checking: false,
    last_check_at: 0,
    downloading: false,
    download_pct: 0,
    ready: false,
    error: '',
    asset_url: '',
    asset_size: 0,
    download_dir: '',
    download_path: '',
    lines: [],
    downloaded_exists: false,
  }

  return {
    caps: {
      localPty: true,
      autoUpdate: true,
      pluginHost: true,
      windowControls: true,
      systemClipboard: true,
      notifications: true,
      fileDialog: true,
      wailsBindings: true,
      capacitor: false,
    },
    relay: {
      load: vi.fn().mockResolvedValue(null),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      fetchMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
      setUplinkPaused: vi.fn().mockResolvedValue(undefined),
      login: vi.fn().mockResolvedValue(undefined),
      logout: vi.fn().mockResolvedValue(undefined),
      loadSavedPassword: vi.fn().mockResolvedValue(''),
    },
    sessions: {
      newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
      closeSession: vi.fn().mockResolvedValue(undefined),
      listShells: vi.fn().mockResolvedValue([]),
      listRemoteSessions: vi.fn().mockResolvedValue([]),
      markSessionsSeen: vi.fn().mockResolvedValue(undefined),
      getPins: vi.fn().mockResolvedValue([]),
      setPins: vi.fn().mockResolvedValue(undefined),
      listRelaySessions: vi.fn().mockResolvedValue([]),
      revokeRelaySession: vi.fn().mockResolvedValue(undefined),
      signOutOtherRelaySessions: vi.fn().mockResolvedValue({ deleted: 0 }),
    },
    system: {
      showNotification: vi.fn().mockResolvedValue(undefined),
      getClipboardPaste: vi.fn().mockResolvedValue({ kind: 'none' }),
      pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
      openExternalURL: vi.fn().mockResolvedValue(undefined),
      getEnvironment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
      getAppVersion: vi.fn().mockResolvedValue('dev'),
      windowMinimize: vi.fn().mockResolvedValue(undefined),
      windowShow: vi.fn().mockResolvedValue(undefined),
      windowUnminimize: vi.fn().mockResolvedValue(undefined),
      windowToggleMaximize: vi.fn().mockResolvedValue(undefined),
      windowIsMaximized: vi.fn().mockResolvedValue(false),
      windowSetTitle: vi.fn().mockResolvedValue(undefined),
      quit: vi.fn().mockResolvedValue(undefined),
    },
    events: {
      on: vi.fn(() => () => {}),
      emit: vi.fn(),
    },
    templates: {
      load: vi.fn().mockResolvedValue([]),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
      loadHidden: vi.fn().mockResolvedValue(false),
      saveHidden: vi.fn().mockResolvedValue(undefined),
    },
    auxKeys: {
      load: vi.fn().mockResolvedValue([]),
      save: vi.fn().mockResolvedValue(undefined),
      clear: vi.fn().mockResolvedValue(undefined),
    },
    updater: {
      getState: vi.fn().mockResolvedValue(fakeUpdateState),
      checkUpdate: vi.fn().mockResolvedValue(undefined),
      startDownload: vi.fn().mockResolvedValue(undefined),
      downloadVersion: vi.fn().mockResolvedValue(undefined),
      installUpdate: vi.fn().mockResolvedValue(undefined),
    },
    pluginHost: {
      getPluginConfig: vi.fn().mockResolvedValue({
        fileExplorer: {
          enabled: false,
          panelWidthPx: 0,
          panelCollapsed: false,
          innerTreeRatio: 0,
          showHidden: false,
          showLineNumbers: false,
        },
        translate: {
          enabled: false,
          provider: '',
          baseUrl: '',
          apiKey: '',
          model: '',
          defaultTargetLang: '',
        },
        shortcuts: { bindings: {} },
      } as unknown as Models.PluginConfig),
      setPluginConfig: vi.fn().mockResolvedValue(undefined),
      fs: {
        listDir: vi.fn().mockResolvedValue([]),
        watchDir: vi.fn().mockResolvedValue(1),     // returns a fake watch id
        unwatchDir: vi.fn().mockResolvedValue(undefined),
        readFile: vi.fn().mockResolvedValue({ path: '/x', data: [], isBinary: false }),
        fileMeta: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isDir: false, exists: true }),
        openExternal: vi.fn().mockResolvedValue(undefined),
        assetUrlFor: vi.fn((p: string) => "/pluginfs/" + btoa(p)),
        writeFile: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isBinary: false }),
        createFile: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isBinary: false }),
        rename: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isBinary: false }),
        remove: vi.fn().mockResolvedValue(undefined),
        mkdir: vi.fn().mockResolvedValue({ path: '/x', size: 0, modTime: 0, isBinary: false }),
        trash: vi.fn().mockResolvedValue(undefined),
      },
    },
  }
}
