<script setup>
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { initPlatform } from '@/platform'
import { initI18n } from '@/i18n'
import { createMockPlatform } from './mock/mockPlatform'
import { installMockWebSocket } from './mock/mockSocket'

const FrontendApp = ref(null)
const ready = ref(false)
const toast = ref(null)
let restoreWs = null

onMounted(async () => {
  // 真实 App.vue 在挂载前依赖:全局 WebSocket 被 mock、platform 已注入、i18n
  // 已初始化(main.web.ts 的 boot 顺序)。这些必须先于组件加载。
  restoreWs = installMockWebSocket()
  await initI18n({})
  const platform = initPlatform(createMockPlatform)
  platform.events.on('demo:toast', (d) => {
    toast.value = d
    setTimeout(() => {
      toast.value = null
    }, 4000)
  })

  // 动态 import 真实 App.vue + 样式,确保上面的初始化已完成再加载组件树。
  const [{ default: App }] = await Promise.all([
    import('@/App.vue'),
    import('@/style.css'),
  ])
  FrontendApp.value = App
  ready.value = true
})

onBeforeUnmount(() => {
  restoreWs?.()
})
</script>

<template>
  <ClientOnly>
    <section class="home-demo" aria-label="AT Term interactive demo">
      <p class="home-demo-hint">
        👇 这是一个纯前端 demo,数据都是假的,随便玩 —— 切换会话、在 idle 的 <code>zsh</code> 里敲 <code>help</code>、打开右侧文件面板。
      </p>
      <div class="home-demo-frame">
        <component :is="FrontendApp" v-if="ready" />
        <div v-else class="home-demo-loading">正在加载 demo…</div>
      </div>
      <transition name="fade">
        <div v-if="toast" class="home-demo-toast">
          <strong>{{ toast.title }}</strong><span>{{ toast.body }}</span>
        </div>
      </transition>
    </section>
  </ClientOnly>
</template>

<style scoped>
.home-demo {
  width: min(1248px, calc(100vw - 24px));
  margin: 20px auto 0;
}
.home-demo-hint {
  font-size: 13px;
  opacity: 0.8;
  margin: 0 0 8px;
}
.home-demo-frame {
  position: relative;
  height: 640px;
  overflow: hidden;
  border: 1px solid var(--vp-c-divider);
  border-radius: 12px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  background: #0b1020;
}
.home-demo-frame :deep(*) {
  box-sizing: border-box;
}
.home-demo-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #9aa4bf;
  font-size: 14px;
}
.home-demo-toast {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 20;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 10px 14px;
  border-radius: 8px;
  background: #1b2030;
  color: #fff;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.4);
  font-size: 13px;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
@media (max-width: 720px) {
  .home-demo-frame {
    height: 560px;
    overflow-x: auto;
  }
  .home-demo-frame :deep(.app) {
    min-width: 1100px;
  }
}
</style>
