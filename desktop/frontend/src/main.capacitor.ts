import { Capacitor } from '@capacitor/core'
import { App as CapacitorApp } from '@capacitor/app'
import { Keyboard } from '@capacitor/keyboard'

import type { LocalePreference } from './i18n'
import { bindCapacitorPrefsSync } from './lib/capacitorPrefsSyncBinder'
import { bootstrapApp } from './lib/bootstrapApp'
import { createCapacitorPrefsSync, notifyLocalChange, setSharedPrefsSync } from './lib/prefsSync.capacitor'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const LOCALE_STORAGE_KEY = 'atterm.locale'

async function loadLocalePreference(): Promise<unknown> {
  try {
    return window.localStorage.getItem(LOCALE_STORAGE_KEY)
  } catch {
    return 'system'
  }
}

async function saveLocalePreference(preference: LocalePreference): Promise<void> {
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, preference)
    // keep the prefsSync namespaced value in sync with the public localStorage key
    try {
      window.localStorage.setItem('atterm.locale_preference.value', JSON.stringify(preference))
    } catch { /* ignore */ }
    notifyLocalChange('locale_preference')
  } catch {
    // Some WebViews disable storage; keep the runtime locale for this session.
  }
}

void bootstrapApp({
  i18n: { loadPreference: loadLocalePreference, savePreference: saveLocalePreference },
  createPlatform: createCapacitorPlatform,
  beforeMount: (platform) => {
    // iOS only: hide WKWebView's input accessory bar (the floating ✓ ↑ ↓ strip
    // that lands above the on-screen keyboard) so it doesn't overlap our own
    // control panel (template + aux keys + paste/image).
    if (Capacitor.getPlatform() === 'ios') {
      Keyboard.setAccessoryBarVisible({ isVisible: false }).catch(() => { /* no-op */ })
    }

    const prefsSync = createCapacitorPrefsSync()
    setSharedPrefsSync(prefsSync)
    bindCapacitorPrefsSync(platform, prefsSync, CapacitorApp)
  },
})
