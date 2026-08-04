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
  afterMount: (platform) => {
    // The Go side (internal/prefssync) emits this event after a PULL or PUSH
    // reconciles synced fields, so the Vue components can re-read them.
    EventsOn('prefs:changed', () => {
      window.dispatchEvent(new CustomEvent('atterm:prefs-changed'))
      platform.events.emit('prefs:changed', undefined)
    })
  },
})
