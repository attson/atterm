import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'
import '@shared/pwa'

if (!applyMobileEntryGuard('settings')) {
  createApp(App).mount('#app')
}
