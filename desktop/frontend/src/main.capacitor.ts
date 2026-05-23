import { createApp } from 'vue'
import { createPinia } from 'pinia'
import MobilePlaceholder from './MobilePlaceholder.vue'
import { initPlatform } from './platform'
import { createCapacitorPlatform } from './platform/capacitor'
import './style.css'

const platform = initPlatform(createCapacitorPlatform)

const app = createApp(MobilePlaceholder)
app.use(createPinia())
app.provide('platform', platform)
app.config.globalProperties.$platform = platform
app.mount('#app')
