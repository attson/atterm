import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { initI18n, type LocalePreference } from './i18n'
import MobileApp from './mobile/MobileApp.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import { Capacitor } from '@capacitor/core'
import { App as CapacitorApp } from '@capacitor/app'
import { Keyboard } from '@capacitor/keyboard'
import { createCapacitorPrefsSync, setSharedPrefsSync, notifyLocalChange } from './lib/prefsSync.capacitor'
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

  // iOS only: hide WKWebView's input accessory bar (the floating ✓ ↑ ↓ strip
  // that lands above the on-screen keyboard) so it doesn't overlap our own
  // control panel (template + aux keys + paste/image).
  if (Capacitor.getPlatform() === 'ios') {
    Keyboard.setAccessoryBarVisible({ isVisible: false }).catch(() => { /* no-op */ })
  }

  const platform = initPlatform(createCapacitorPlatform)

  const prefsSync = createCapacitorPrefsSync()
  setSharedPrefsSync(prefsSync)
  // Initial PULL (fire-and-forget; mobile may not be logged in yet)
  void prefsSync.pull().then(() => platform.events.emit('prefs:changed', undefined)).catch(() => {})
  // Foreground PULL
  CapacitorApp.addListener('appStateChange', (state) => {
    if (state.isActive) void prefsSync.pull().then(() => platform.events.emit('prefs:changed', undefined)).catch(() => {})
  })

  const app = createApp(MobileApp)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
