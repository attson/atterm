import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
import { bridgeSharedI18n } from './i18n/shared-bridge'
import { initPlatform } from './platform'
import { createWebPlatform } from './platform/web'
import { PrefsSyncEngine, localStorageAdapter, apiRelayClient, setSharedPrefsSync } from '@webshared/sync/prefsSync'
import './style.css'

async function bootstrap() {
  await initI18n({
    // TS-shared uses localStorage locale key `atterm.locale`; mirror the
    // pattern used by main.capacitor.ts
    loadPreference: async () => localStorage.getItem('atterm.locale'),
    savePreference: async (p) => localStorage.setItem('atterm.locale', String(p)),
  })
  // @shared/i18n powers components under `@shared/*` (admin panel, shared
  // Topbar). Mirror the desktop-local locale into it so those components
  // render in the user's chosen language, not the shared-i18n default 'en'.
  bridgeSharedI18n()

  const platform = initPlatform(createWebPlatform)

  const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
  setSharedPrefsSync(prefsSync)
  const pullPrefsAndNotify = () => {
    void prefsSync.pull()
      .then(() => platform.events.emit('prefs:changed', undefined))
      .catch(() => {})
  }
  pullPrefsAndNotify()
  platform.events.on('prefs:remote-changed', pullPrefsAndNotify)

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
