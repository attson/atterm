import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { initI18n, type LocalePreference } from './i18n'
import MobileApp from './mobile/MobileApp.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import { Capacitor } from '@capacitor/core'
import { Keyboard } from '@capacitor/keyboard'
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

  const app = createApp(MobileApp)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
