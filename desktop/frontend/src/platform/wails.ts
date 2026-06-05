import * as api from '../lib/api'
import {
  EventsOn,
  EventsEmit,
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  Quit,
  Environment,
  BrowserOpenURL,
} from '../../wailsjs/runtime/runtime'
import { GetPluginConfig, SetPluginConfig } from '../../wailsjs/go/main/App'
import {
  ListDir,
  WatchDir,
  UnwatchDir,
  ReadFile,
  FileMeta,
} from '../../wailsjs/go/main/PluginFS'
import type { Platform, EnvironmentInfo, RemoteSession } from './types'

export function createWailsPlatform(): Platform {
  return {
    caps: {
      localPty: true,
      autoUpdate: true,
      pluginHost: true,
      windowControls: true,
      systemClipboard: true,
      notifications: true,
      fileDialog: true,
    },
    relay: {
      load: () => api.getRelayConfig().then((c) => c ?? null),
      save: async (cfg) => {
        await api.setRelayConfig({
          url: cfg.url,
          token: cfg.token,
          allow_insecure_relay: cfg.allow_insecure_relay,
          remote_permission: cfg.remote_permission,
        })
      },
      clear: async () => {
        await api.setRelayConfig({
          url: '',
          token: '',
          allow_insecure_relay: false,
          remote_permission: 'full',
        })
      },
      fetchMe: () => api.fetchRelayMe(),
      setUplinkPaused: (paused: boolean) => api.setUplinkPaused(paused),
    },
    sessions: {
      newSession: api.newSession,
      closeSession: api.closeSession,
      listShells: api.listShells,
      // Remote session listing currently goes through relay HTTP API in
      // existing components (RemoteSessionsDialog uses lib/api or direct
      // fetch). For now expose an empty implementation; consumers continue
      // to use their existing path until a follow-up PR consolidates.
      listRemoteSessions: async (): Promise<RemoteSession[]> => [],
    },
    system: {
      showNotification: api.showNotification,
      getClipboardPaste: api.getClipboardPastePayload,
      pickLogFilePath: api.pickLogFilePath,
      openExternalURL: async (url: string) => {
        BrowserOpenURL(url)
      },
      getEnvironment: async (): Promise<EnvironmentInfo | null> => {
        try {
          const info = await Environment()
          return {
            platform: info.platform,
            arch: info.arch,
            buildType: info.buildType,
          }
        } catch {
          return null
        }
      },
      windowMinimize: async () => WindowMinimise(),
      windowToggleMaximize: async () => WindowToggleMaximise(),
      windowIsMaximized: () => WindowIsMaximised(),
      quit: async () => Quit(),
    },
    events: {
      on: (event, handler) => EventsOn(event, handler as (...data: unknown[]) => void),
      emit: (event, data) => EventsEmit(event, data),
    },
    templates: {
      load: async () => {
        const raw = await api.getQuickTemplates()
        return Array.isArray(raw) ? raw : []
      },
      save: async (list) => {
        await api.setQuickTemplates(list)
      },
      clear: async () => {
        await api.setQuickTemplates([])
      },
    },
    // Desktop has a real keyboard and never renders the mobile aux-key bar,
    // so there is nothing to persist — a no-op bridge satisfies the Platform
    // type without a Go binding.
    auxKeys: {
      load: async () => [],
      save: async () => {},
      clear: async () => {},
    },
    updater: {
      getState: api.getUpdateState,
      checkUpdate: api.checkUpdate,
      startDownload: api.startDownload,
      installUpdate: api.installUpdate,
    },
    pluginHost: {
      getPluginConfig: GetPluginConfig as () => Promise<import('../../wailsjs/go/models').main.PluginConfig>,
      setPluginConfig: SetPluginConfig as (cfg: import('../../wailsjs/go/models').main.PluginConfig) => Promise<void>,
      fs: {
        listDir: ListDir as (path: string) => Promise<import('./types').DirEntry[]>,
        watchDir: WatchDir,
        unwatchDir: UnwatchDir,
        readFile: ReadFile as (path: string, maxBytes?: number) => Promise<import('./types').FileContent>,
        fileMeta: FileMeta as (path: string) => Promise<import('./types').FileMetaInfo>,
      },
    },
  }
}
