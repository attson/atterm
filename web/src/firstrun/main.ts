import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import { initI18n } from '@shared/i18n'
import '@shared/pwa'

if (!applyMobileEntryGuard('firstrun')) {
  initI18n()
  createApp(App).mount('#app')
}
