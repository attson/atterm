import { createApp } from 'vue'
import App from './App.vue'
import '@shared/tokens.css'
import { applyMobileEntryGuard } from '@shared/mobile-guard'

if (!applyMobileEntryGuard('setup')) {
  createApp(App).mount('#app')
}
