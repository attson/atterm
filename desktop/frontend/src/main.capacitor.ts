import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { initI18n, type LocalePreference } from './i18n'
import { bridgeSharedI18n } from './i18n/shared-bridge'
import App from './App.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import { Capacitor } from '@capacitor/core'
import { App as CapacitorApp } from '@capacitor/app'
import { Keyboard } from '@capacitor/keyboard'
import { createCapacitorPrefsSync, setSharedPrefsSync, notifyLocalChange } from './lib/prefsSync.capacitor'
import { bindCapacitorPrefsSync } from './lib/capacitorPrefsSyncBinder'
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

async function bootstrap() {
  await initI18n({ loadPreference: loadLocalePreference, savePreference: saveLocalePreference })
  // @shared/i18n powers components under `@shared/*` (admin panel, shared
  // Topbar). Keep the three entries consistent so shared components pick up
  // the user's locale automatically instead of silently rendering in English.
  bridgeSharedI18n()

  // iOS only: hide WKWebView's input accessory bar (the floating ✓ ↑ ↓ strip
  // that lands above the on-screen keyboard) so it doesn't overlap our own
  // control panel (template + aux keys + paste/image).
  if (Capacitor.getPlatform() === 'ios') {
    Keyboard.setAccessoryBarVisible({ isVisible: false }).catch(() => { /* no-op */ })
  }

  const platform = initPlatform(createCapacitorPlatform)

  const prefsSync = createCapacitorPrefsSync()
  setSharedPrefsSync(prefsSync)
  bindCapacitorPrefsSync(platform, prefsSync, CapacitorApp)

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
