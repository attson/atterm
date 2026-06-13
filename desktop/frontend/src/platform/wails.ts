import * as api from '../lib/api'
import {
  EventsOn,
  EventsEmit,
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  WindowSetTitle,
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
  OpenExternal,
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
          session_expires_at: cfg.session_expires_at,
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
      webhooks: {
        list: () => api.listWebhooks(),
        create: (req) => api.createWebhook(req),
        delete: (id: string) => api.deleteWebhook(id),
      },
    },
    sessions: {
      newSession: api.newSession,
      closeSession: api.closeSession,
      listShells: api.listShells,
      // Remote session listing currently goes through SessionListConnection
      // in App.vue. Keep this empty until the platform bridge is unified.
      listRemoteSessions: async (): Promise<RemoteSession[]> => [],
      markSessionsSeen: api.markSessionsSeen,
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
      windowSetTitle: async (title: string) => WindowSetTitle(title),
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
      // Hidden flag is a per-device UI preference, not a session secret —
      // localStorage in the WebView is fine and avoids a new Wails binding.
      loadHidden: async () => {
        return typeof localStorage !== 'undefined' && localStorage.getItem('atterm.templates.hidden') === '1'
      },
      saveHidden: async (hidden: boolean) => {
        if (typeof localStorage === 'undefined') return
        if (hidden) localStorage.setItem('atterm.templates.hidden', '1')
        else localStorage.removeItem('atterm.templates.hidden')
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
        openExternal: (path: string) => OpenExternal(path),
        assetUrlFor: (path: string) => {
          // base64 URL encoding (RFC 4648 §5), matches Go's base64.URLEncoding.
          const bytes = new TextEncoder().encode(path);
          let bin = "";
          for (const b of bytes) bin += String.fromCharCode(b);
          const b64 = btoa(bin).replace(/\+/g, "-").replace(/\//g, "_");
          return "/pluginfs/" + b64;
        },
      },
    },
  }
}
