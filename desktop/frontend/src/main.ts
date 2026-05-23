import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initPlatform } from './platform'
import { createWailsPlatform } from './platform/wails'
import './style.css'

const platform = initPlatform(createWailsPlatform)

const app = createApp(App)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
