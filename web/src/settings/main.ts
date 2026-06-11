import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import { initI18n } from '@shared/i18n'
import '@shared/pwa'
import { PrefsSyncEngine, localStorageAdapter, apiRelayClient, setSharedPrefsSync } from '@shared/sync/prefsSync'

if (!applyMobileEntryGuard('settings')) {
  initI18n()
  const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
  setSharedPrefsSync(prefsSync)
  void prefsSync.pull().catch(() => {})
  window.addEventListener('focus', () => {
    void prefsSync.pull().catch(() => {})
  })
  createApp(App).mount('#app')
}
