import { getLocalePreference, setLocalePreference } from './lib/api'
import { bootstrapApp } from './lib/bootstrapApp'
import { createWailsPlatform } from './platform/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import './style.css'

void bootstrapApp({
  i18n: {
    loadPreference: getLocalePreference,
    savePreference: setLocalePreference,
  },
  createPlatform: createWailsPlatform,
  afterMount: () => {
    // The Go side (internal/prefssync) emits this event after a PULL or PUSH
    // reconciles synced fields, so the Vue components can re-read them.
    //
    // Only the window-level CustomEvent needs bridging. Do NOT re-emit
    // 'prefs:changed' onto the platform bus here: on Wails that bus IS the
    // Wails bus (platform.events.on === EventsOn), so every component
    // listening via platform.events.on already receives this exact event
    // directly from Go. Re-emitting it fed the listener back into itself —
    // ~1286 levels of recursion and ~1286 IPC messages per event, which threw
    // "Maximum call stack size exceeded", aborted the dispatch (so the pin /
    // template reload listeners never ran) and froze the UI for seconds on
    // every pin/unpin. The web and Capacitor entrypoints keep their re-emit
    // because their buses are private Maps that Go events do not reach.
    EventsOn('prefs:changed', () => {
      window.dispatchEvent(new CustomEvent('atterm:prefs-changed'))
    })
  },
})
