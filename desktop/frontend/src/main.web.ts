import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
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

  const platform = initPlatform(createWebPlatform)

  const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
  setSharedPrefsSync(prefsSync)
  void prefsSync.pull()
    .then(() => platform.events.emit('prefs:changed', undefined))
    .catch(() => {})

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
