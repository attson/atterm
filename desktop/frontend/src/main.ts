import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
import { bridgeSharedI18n } from './i18n/shared-bridge'
import { getLocalePreference, setLocalePreference } from './lib/api'
import { initPlatform } from './platform'
import { createWailsPlatform } from './platform/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'
import './style.css'

async function bootstrap() {
  await initI18n({ loadPreference: getLocalePreference, savePreference: setLocalePreference })
  // @shared/i18n powers components under `@shared/*` (admin panel, shared
  // Topbar). Mirror the desktop-local locale into it so those components
  // render in the user's chosen language, not the shared-i18n default 'en'.
  bridgeSharedI18n()

  const platform = initPlatform(createWailsPlatform)

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')

  // The Go side (internal/prefssync) emits this event after a PULL or PUSH
  // reconciles synced fields, so the Vue components can re-read them.
  EventsOn('prefs:changed', () => {
    window.dispatchEvent(new CustomEvent('atterm:prefs-changed'))
    platform.events.emit('prefs:changed', undefined)
  })
}

void bootstrap()
