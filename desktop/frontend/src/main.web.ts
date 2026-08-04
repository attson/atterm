import { PrefsSyncEngine, localStorageAdapter, apiRelayClient, setSharedPrefsSync } from '@webshared/sync/prefsSync'

import { bootstrapApp } from './lib/bootstrapApp'
import { createWebPlatform } from './platform/web'
import './style.css'

void bootstrapApp({
  i18n: {
    // TS-shared uses localStorage locale key `atterm.locale`; mirror the
    // pattern used by main.capacitor.ts.
    loadPreference: async () => localStorage.getItem('atterm.locale'),
    savePreference: async (p) => localStorage.setItem('atterm.locale', String(p)),
  },
  createPlatform: createWebPlatform,
  beforeMount: (platform) => {
    const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
    setSharedPrefsSync(prefsSync)
    const pullPrefsAndNotify = () => {
      void prefsSync.pull()
        .then(() => platform.events.emit('prefs:changed', undefined))
        .catch(() => {})
    }
    pullPrefsAndNotify()
    platform.events.on('prefs:remote-changed', pullPrefsAndNotify)
  },
})
