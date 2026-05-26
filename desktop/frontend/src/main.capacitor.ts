import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { initI18n, type LocalePreference } from './i18n'
import MobileApp from './mobile/MobileApp.vue'
import { initPlatform } from './platform'
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
  } catch {
    // Some WebViews disable storage; keep the runtime locale for this session.
  }
}

async function bootstrap() {
  await initI18n({ loadPreference: loadLocalePreference, savePreference: saveLocalePreference })

  const platform = initPlatform(createCapacitorPlatform)

  const app = createApp(MobileApp)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
