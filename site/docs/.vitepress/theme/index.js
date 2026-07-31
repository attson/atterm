import DefaultTheme from 'vitepress/theme'
import { createPinia } from 'pinia'
import HomeDemo from './components/HomeDemo.vue'
import './custom.css'

// 真实 App.vue 及其组件用 Pinia store(如 pluginConfig)。把 Pinia 装到
// VitePress 的 app 实例上,HomeDemo 组件树才能用这些 store。platform / i18n
// 的初始化在 HomeDemo onMounted 里做(需在 App.vue 动态 import 之前)。
export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.use(createPinia())
    app.component('HomeDemo', HomeDemo)
  },
}
