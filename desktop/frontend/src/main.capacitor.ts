import { createApp } from 'vue'
import { createPinia } from 'pinia'
import MobileApp from './mobile/MobileApp.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const platform = initPlatform(createCapacitorPlatform)

const app = createApp(MobileApp)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
