import { describe, it, expect, vi, beforeEach } from 'vitest'

// Faithful stand-in for the Wails runtime event bus. The real
// wails/runtime.js EventsEmit does:
//
//   function EventsEmit(eventName) {
//     notifyListeners(payload)                     // <- synchronous re-entry
//     window.WailsInvoke("EE" + JSON.stringify(payload))
//   }
//
// i.e. emitting an event synchronously invokes every EventsOn listener for
// that name in the *same* page, and additionally ships the event to Go. The
// vi.fn() stubs used by wails.test.ts cannot reproduce that, so this file
// models it — it is the only way to catch the self-recursion guarded here.
const listeners = new Map<string, Array<(...d: unknown[]) => void>>()
// Counts messages the real runtime would push over the Wails IPC bridge
// (window.WailsInvoke("EE"...)) — one per emit, including re-entrant ones.
let ipcMessages = 0

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((name: string, cb: (...d: unknown[]) => void) => {
    const list = listeners.get(name) ?? []
    list.push(cb)
    listeners.set(name, list)
    return () => {}
  }),
  EventsEmit: vi.fn((name: string, ...data: unknown[]) => {
    for (const cb of [...(listeners.get(name) ?? [])]) cb(...data)
    ipcMessages++
  }),
  WindowMinimise: vi.fn(),
  WindowShow: vi.fn(),
  WindowUnminimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(true),
  WindowSetTitle: vi.fn(),
  Quit: vi.fn(),
  Environment: vi.fn().mockResolvedValue({ platform: 'darwin', arch: 'arm64', buildType: 'production' }),
  BrowserOpenURL: vi.fn(),
}))

vi.mock('../../lib/api', () => ({
  getRelayConfig: vi.fn().mockResolvedValue({ url: '', token: '' }),
  setRelayConfig: vi.fn().mockResolvedValue(undefined),
  setUplinkPaused: vi.fn().mockResolvedValue(undefined),
  fetchRelayMe: vi.fn().mockResolvedValue({ user_id: 'u', email: 'e' }),
  getClipboardPastePayload: vi.fn().mockResolvedValue({ kind: 'none' }),
  showNotification: vi.fn().mockResolvedValue(undefined),
  pickLogFilePath: vi.fn().mockResolvedValue('/tmp/log'),
  newSession: vi.fn().mockResolvedValue({ session_id: 's1' }),
  closeSession: vi.fn().mockResolvedValue(undefined),
  listShells: vi.fn().mockResolvedValue([]),
  getUpdateState: vi.fn().mockResolvedValue({ state: 'idle' }),
  checkUpdate: vi.fn().mockResolvedValue(undefined),
  startDownload: vi.fn().mockResolvedValue(undefined),
  downloadVersion: vi.fn().mockResolvedValue(undefined),
  installUpdate: vi.fn().mockResolvedValue(undefined),
  markSessionsSeen: vi.fn().mockResolvedValue(undefined),
  getPinnedSessionIds: vi.fn().mockResolvedValue([]),
  setPinnedSessionIds: vi.fn().mockResolvedValue(undefined),
  listRelaySessions: vi.fn().mockResolvedValue([]),
  revokeRelaySession: vi.fn().mockResolvedValue(undefined),
  signOutOtherRelaySessions: vi.fn().mockResolvedValue({ deleted: 0 }),
}))

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetPluginConfig: vi.fn().mockResolvedValue({ enabled_plugins: [] }),
  SetPluginConfig: vi.fn().mockResolvedValue(undefined),
  GetAppVersion: vi.fn().mockResolvedValue('v0.4.0'),
}))

import { createWailsPlatform } from '../wails'

beforeEach(() => {
  listeners.clear()
  ipcMessages = 0
})

describe('wails platform event bus — self-recursion', () => {
  // Regression: main.ts bridges Go's 'prefs:changed' into the platform bus by
  // calling platform.events.emit('prefs:changed') from *inside* an
  // EventsOn('prefs:changed') listener. Because on/emit are both backed by the
  // same Wails bus, that emit re-entered its own listener ~1286 times per
  // event (measured in a live dev build) and threw
  // "RangeError: Maximum call stack size exceeded", which aborted the whole
  // dispatch — so the real listeners (pin reload, template reload) never ran,
  // while ~1286 IPC messages per event flooded the main thread and froze the
  // UI for seconds. Pin/unpin is the highest-frequency trigger of
  // 'prefs:changed' (SetPinnedSessionIds -> updatePref -> markPrefDirtyAndPush,
  // plus a second emit from the relay prefs-watch pull).
  it('does not re-enter its own listener when a handler re-emits the same event', () => {
    const platform = createWailsPlatform()
    let calls = 0
    platform.events.on('prefs:changed', () => {
      calls++
      // The exact shape of the main.ts bridge.
      platform.events.emit('prefs:changed', undefined)
    })

    expect(() => platform.events.emit('prefs:changed', undefined)).not.toThrow()
    // One outer emit delivers the event exactly once. Without the guard this
    // recurses until the JS stack overflows.
    expect(calls).toBe(1)
  })

  it('sends a bounded number of IPC messages per emit', () => {
    const platform = createWailsPlatform()
    platform.events.on('prefs:changed', () => {
      platform.events.emit('prefs:changed', undefined)
    })

    platform.events.emit('prefs:changed', undefined)
    // Exactly one trip over the Wails bridge, not one per recursion level.
    expect(ipcMessages).toBe(1)
  })

  it('still delivers unrelated events normally', () => {
    const platform = createWailsPlatform()
    const seen: unknown[] = []
    platform.events.on('quickTemplates:changed', (d: unknown) => seen.push(d))

    platform.events.emit('quickTemplates:changed', 'a')
    platform.events.emit('quickTemplates:changed', 'b')

    expect(seen).toEqual(['a', 'b'])
    expect(ipcMessages).toBe(2)
  })

  it('re-allows an event once its dispatch has finished', () => {
    const platform = createWailsPlatform()
    let calls = 0
    platform.events.on('prefs:changed', () => { calls++ })

    platform.events.emit('prefs:changed', undefined)
    platform.events.emit('prefs:changed', undefined)

    // The guard is scoped to one dispatch, not a permanent mute.
    expect(calls).toBe(2)
  })
})
