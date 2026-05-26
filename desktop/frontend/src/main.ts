import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
import { getLocalePreference, setLocalePreference } from './lib/api'
import { initPlatform } from './platform'
import { createWailsPlatform } from './platform/wails'
import './style.css'

async function bootstrap() {
  await initI18n({ loadPreference: getLocalePreference, savePreference: setLocalePreference })

  const platform = initPlatform(createWailsPlatform)

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
