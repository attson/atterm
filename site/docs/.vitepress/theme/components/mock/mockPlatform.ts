import type { Platform, Capabilities } from '@/platform/types'
import { DEFAULT_TEMPLATES } from '@/lib/templates'
import { fakeSessions } from './fakeSessions'
import { createMockPluginFs } from './mockFs'
import { MockSocket } from './mockSocket'

// 以 desktop/frontend/src/platform/web.ts 为模板,把网络调用换成内存假数据。
// 目标:让真实 App.vue 以「已登录 + 已连接」状态直接进入主界面,并打开文件
// 浏览器(pluginHost=true)。

const CAPS: Capabilities = {
  // localPty + wailsBindings=true 让 App 走桌面 boot 链:显示新建 tab 按钮、
  // 建立本机会话列表连接、终端连本机 endpoint —— 全部走注入的 window.go
  // (mockGoApp)与 mockSocket。这样网页也能新建本机会话。
  localPty: true,
  autoUpdate: false,
  pluginHost: true, // 打开文件浏览器 + 插件面板
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: false,
  wailsBindings: true,
  capacitor: false,
}

// 内存 KV,替代 localStorage,避免污染真实站点存储
const mem = new Map<string, string>()

export function createMockPlatform(): Platform {
  const events = makeEventBus()

  const platform = {
    caps: CAPS,
    relay: {
      async load() {
        return {
          url: 'https://demo.atterm.local',
          token: 'demo-token',
          session_expires_at: 4_102_444_800,
          allow_insecure_relay: false,
          remote_permission: 'full',
          last_email: 'you@example.com',
          connected: true,
        }
      },
      async save() {},
      async clear() {},
      async fetchMe() {
        return { user: { id: 'demo', email: 'you@example.com' } }
      },
      async logout() {},
    },
    sessions: {
      async closeSession() {},
      async listShells() {
        return []
      },
      async listRemoteSessions() {
        return fakeSessions
      },
      async markSessionsSeen() {},
      async getPins() {
        const v = mem.get('pins')
        return v ? (JSON.parse(v) as string[]) : []
      },
      async setPins(ids: string[]) {
        mem.set('pins', JSON.stringify(ids))
        events.emit('prefs:remote-changed', undefined)
      },
      async listRelaySessions() {
        return []
      },
      async revokeRelaySession() {},
      async signOutOtherRelaySessions() {
        return { count: 0 }
      },
    },
    system: {
      async showNotification(title: string, body: string) {
        try {
          if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
            new Notification(title, { body })
            return
          }
        } catch {
          /* fall through to toast */
        }
        events.emit('demo:toast', { title, body })
      },
      async getClipboardPaste() {
        return { kind: 'none' }
      },
      async openExternalURL(url: string) {
        if (typeof window !== 'undefined') window.open(url, '_blank', 'noopener')
      },
      async getEnvironment() {
        return {
          buildType: 'web',
          platform: typeof navigator !== 'undefined' ? navigator.userAgent : 'web',
          arch: '',
        }
      },
      async getAppVersion() {
        return { version: 'demo', tag: 'demo' }
      },
    },
    events,
    templates: {
      async load() {
        // 默认快捷模板(yes / ok / continue / commit / push …),让模板条显示出来。
        return [...DEFAULT_TEMPLATES]
      },
      async save() {},
      async clear() {},
      async loadHidden() {
        return false
      },
      async saveHidden() {},
    },
    auxKeys: {
      async load() {
        return []
      },
      async save() {},
      async clear() {},
    },
    pluginHost: {
      async getPluginConfig() {
        return {
          fileExplorer: {
            enabled: true,
            panelWidthPx: 380,
            panelCollapsed: true, // 默认折叠,用户点开才浏览文件
            innerTreeRatio: 0.4,
            showHidden: false,
            showLineNumbers: true,
          },
          translate: { enabled: false },
          shortcuts: {},
        }
      },
      async setPluginConfig() {},
      fs: createMockPluginFs(),
    },
  } as unknown as Platform

  // 通知回调接到 MockSocket:长命令完成时弹通知
  MockSocket.onNotify = (title, body) => {
    void platform.system.showNotification(title, body)
  }

  return platform
}

function makeEventBus() {
  const map = new Map<string, Set<(d: unknown) => void>>()
  return {
    on(name: string, handler: (d: unknown) => void) {
      let set = map.get(name)
      if (!set) {
        set = new Set()
        map.set(name, set)
      }
      set.add(handler)
      return () => set!.delete(handler)
    },
    emit(name: string, data: unknown) {
      for (const fn of Array.from(map.get(name) || [])) {
        try {
          fn(data)
        } catch {
          /* ignore listener errors */
        }
      }
    },
  }
}
