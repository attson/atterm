<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { withBase } from 'vitepress'
import {
  __setPlatformForTests,
  initPlatform,
  useOptionalPlatform,
} from '@/platform'
import { initI18n, resetI18nForTest } from '@/i18n'
import { createMockPlatform } from './mock/mockPlatform'
import { installMockWebSocket } from './mock/mockSocket'
import { installMockGoApp } from './mock/mockGoApp'

const props = defineProps({
  locale: { type: String, default: 'zh' },
})

const copy = computed(() => props.locale === 'en' ? {
  aria: 'AT Term interactive demo',
  alt: 'AT Term session sidebar, terminal, and file browser',
  kicker: 'Interactive demo',
  title: 'Experience AT Term in your browser',
  start: 'Start demo',
  loading: 'Starting the interactive demo',
  loadingHint: 'The real terminal interface may take a moment to load.',
  error: 'Demo failed to load',
  errorHint: 'The interactive interface is unavailable. Documentation and downloads still work.',
  retry: 'Retry',
} : {
  aria: 'AT Term 交互演示',
  alt: 'AT Term 会话侧栏、终端和文件浏览界面',
  kicker: '交互 Demo',
  title: '在浏览器中体验 AT Term',
  start: '开始体验',
  loading: '正在启动交互 Demo',
  loadingHint: '首次加载真实终端界面需要一点时间。',
  error: 'Demo 加载失败',
  errorHint: '无法加载交互界面，文档和下载仍可正常使用。',
  retry: '重试',
})

const FrontendApp = ref(null)
const frameRef = ref(null)
const phase = ref('idle')
const loadError = ref('')
const toast = ref(null)

let restoreWs = null
let restoreGo = null
let offToast = null
let toastTimer = null
let previousPlatform = null
let previousDocumentLang = ''
let runtimeInstalled = false
let frameResizeObserver = null

const DEMO_RUNTIME_CLASS = 'atterm-demo-runtime-active'
const DEMO_BOUND_VARS = [
  '--atterm-demo-top',
  '--atterm-demo-left',
  '--atterm-demo-width',
  '--atterm-demo-height',
]

const RETRY_QUERY = 'atterm-demo-retry'

function clearDemoHash() {
  history.replaceState({}, '', `${location.pathname}${location.search}`)
}

function clearTimers() {
  if (toastTimer !== null) window.clearTimeout(toastTimer)
  toastTimer = null
}

function syncDemoBounds() {
  const frame = frameRef.value
  if (!frame) return
  const rect = frame.getBoundingClientRect()
  const top = Math.max(0, rect.top)
  const left = Math.max(0, rect.left)
  const right = Math.min(window.innerWidth, rect.right)
  const bottom = Math.min(window.innerHeight, rect.bottom)
  const root = document.documentElement
  root.style.setProperty('--atterm-demo-top', `${top}px`)
  root.style.setProperty('--atterm-demo-left', `${left}px`)
  root.style.setProperty('--atterm-demo-width', `${Math.max(0, right - left)}px`)
  root.style.setProperty('--atterm-demo-height', `${Math.max(0, bottom - top)}px`)
}

function activateDemoBounds() {
  document.documentElement.classList.add(DEMO_RUNTIME_CLASS)
  syncDemoBounds()
  frameResizeObserver?.disconnect()
  frameResizeObserver = new ResizeObserver(syncDemoBounds)
  if (frameRef.value) frameResizeObserver.observe(frameRef.value)
  window.addEventListener('resize', syncDemoBounds)
  window.addEventListener('scroll', syncDemoBounds, true)
}

function deactivateDemoBounds() {
  frameResizeObserver?.disconnect()
  frameResizeObserver = null
  window.removeEventListener('resize', syncDemoBounds)
  window.removeEventListener('scroll', syncDemoBounds, true)
  const root = document.documentElement
  root.classList.remove(DEMO_RUNTIME_CLASS)
  for (const name of DEMO_BOUND_VARS) root.style.removeProperty(name)
}

function cleanupRuntime({ resetLocation = false } = {}) {
  clearTimers()
  deactivateDemoBounds()
  if (!runtimeInstalled) return

  offToast?.()
  restoreWs?.()
  restoreGo?.()
  offToast = null
  restoreWs = null
  restoreGo = null
  __setPlatformForTests(previousPlatform)
  previousPlatform = null
  resetI18nForTest()
  if (previousDocumentLang) document.documentElement.lang = previousDocumentLang
  previousDocumentLang = ''
  runtimeInstalled = false
  if (resetLocation) clearDemoHash()
}

async function startDemo() {
  if (phase.value === 'loading' || phase.value === 'ready') return

  phase.value = 'loading'
  loadError.value = ''
  clearDemoHash()

  try {
    previousPlatform = useOptionalPlatform()
    previousDocumentLang = document.documentElement.lang
    runtimeInstalled = true
    __setPlatformForTests(null)
    restoreWs = installMockWebSocket()
    restoreGo = installMockGoApp()
    await initI18n({ getLanguages: () => [document.documentElement.lang] })
    const platform = initPlatform(createMockPlatform)
    offToast = platform.events.on('demo:toast', (detail) => {
      toast.value = detail
      if (toastTimer !== null) window.clearTimeout(toastTimer)
      toastTimer = window.setTimeout(() => {
        toast.value = null
        toastTimer = null
      }, 4000)
    })

    const [{ default: App }] = await Promise.all([
      import('@/App.vue'),
      import('@/style.css'),
    ])
    FrontendApp.value = App
    phase.value = 'ready'
    activateDemoBounds()
  } catch (error) {
    FrontendApp.value = null
    phase.value = 'error'
    loadError.value = error instanceof Error ? error.message : String(error)
    cleanupRuntime({ resetLocation: true })
  }
}

function retryDemo() {
  const url = new URL(window.location.href)
  url.searchParams.set(RETRY_QUERY, '1')
  window.location.replace(url)
}

onMounted(() => {
  const url = new URL(window.location.href)
  if (url.searchParams.get(RETRY_QUERY) !== '1') return

  url.searchParams.delete(RETRY_QUERY)
  history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`)
  void startDemo()
})

onUnmounted(() => {
  cleanupRuntime()
})
</script>

<template>
  <ClientOnly>
    <section class="home-demo" :aria-label="copy.aria">
      <div ref="frameRef" class="home-demo-frame" :class="`is-${phase}`">
        <component :is="FrontendApp" v-if="phase === 'ready' && FrontendApp" />

        <div v-else-if="phase === 'idle'" class="home-demo-poster" data-testid="demo-poster">
          <img
            :src="withBase('/atterm-demo-poster.png')"
            :alt="copy.alt"
            width="1200"
            height="630"
          >
          <div class="home-demo-launcher">
            <p class="home-demo-kicker">{{ copy.kicker }}</p>
            <h2>{{ copy.title }}</h2>
            <button class="home-demo-start" type="button" data-testid="demo-start" @click="startDemo">
              {{ copy.start }}
            </button>
          </div>
        </div>

        <div v-else-if="phase === 'loading'" class="home-demo-state" role="status" aria-live="polite">
          <span class="home-demo-spinner" aria-hidden="true" />
          <strong>{{ copy.loading }}</strong>
          <span>{{ copy.loadingHint }}</span>
        </div>

        <div v-else class="home-demo-state is-error" role="alert">
          <strong>{{ copy.error }}</strong>
          <span>{{ loadError || copy.errorHint }}</span>
          <button type="button" data-testid="demo-retry" @click="retryDemo">{{ copy.retry }}</button>
        </div>
      </div>

      <transition name="fade">
        <div v-if="toast" class="home-demo-toast" role="status" aria-live="polite">
          <strong>{{ toast.title }}</strong><span>{{ toast.body }}</span>
        </div>
      </transition>
    </section>
  </ClientOnly>
</template>

<style scoped>
.home-demo {
  position: relative;
  width: 100%;
  max-width: 1248px;
  margin: 8px auto 0;
}
.home-demo-frame {
  position: relative;
  height: 680px;
  overflow: hidden;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.18);
  background: #070a10;
}
.home-demo-frame :deep(*) { box-sizing: border-box; }
.home-demo-frame :deep(.app) {
  position: relative;
  height: 100%;
  min-height: 0;
  color: var(--fg);
  font-family: var(--font-sans);
  font-size: 14px;
}
.home-demo-frame :deep(.titlebar .drawer) {
  position: absolute;
  top: 36px;
  right: 8px;
  width: min(280px, calc(100% - 16px));
}
.home-demo-frame :deep(.session-input-history-header h2) {
  padding: 0;
  border: 0;
  line-height: 1.4;
}
:global(html.atterm-demo-runtime-active .snippet-run-overlay) {
  inset: auto;
  top: var(--atterm-demo-top);
  left: var(--atterm-demo-left);
  width: var(--atterm-demo-width);
  height: var(--atterm-demo-height);
}
:global(html.atterm-demo-runtime-active .snippet-run-overlay > .snippet-run-modal) {
  width: min(960px, calc(100% - 40px));
  height: min(760px, calc(100% - 40px));
  min-height: 0;
}
.home-demo-frame :deep(.paste-preview-host) {
  position: absolute;
}
.home-demo-poster,
.home-demo-poster img { width: 100%; height: 100%; }
.home-demo-poster img {
  display: block;
  object-fit: cover;
  object-position: center top;
  opacity: 0.64;
}
.home-demo-launcher {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 24px;
  text-align: center;
  background: rgba(7, 10, 16, 0.52);
  backdrop-filter: blur(1px);
}
.home-demo-kicker {
  margin: 0 0 6px;
  color: #8ea9ff;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
}
.home-demo-launcher h2 {
  margin: 0 0 22px;
  border: 0;
  color: #eef2ff;
  font-size: 24px;
  letter-spacing: 0;
}
.home-demo-start {
  min-width: 160px;
  min-height: 44px;
  padding: 10px 22px;
  border: 1px solid #8ea9ff;
  border-radius: 6px;
  background: #4f6fdd;
  color: #fff;
  cursor: pointer;
  font-size: 14px;
  font-weight: 700;
}
.home-demo-start:hover { border-color: #a7b9ff; background: #6683e8; color: #fff; }
.home-demo-start:focus-visible { outline: 2px solid #a7b9ff; outline-offset: 3px; }
.home-demo-state {
  display: flex;
  height: 100%;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px;
  color: #aab4ca;
  text-align: center;
  font-size: 13px;
}
.home-demo-state strong { color: #eef2ff; font-size: 16px; }
.home-demo-state.is-error span { max-width: 560px; }
.home-demo-state button {
  margin-top: 8px;
  border: 1px solid #6f8bec;
  border-radius: 6px;
  padding: 7px 16px;
  background: #4f6fdd;
  color: white;
  cursor: pointer;
}
.home-demo-spinner {
  width: 22px;
  height: 22px;
  border: 2px solid rgba(255, 255, 255, 0.18);
  border-top-color: #8ea9ff;
  border-radius: 50%;
  animation: home-demo-spin 0.8s linear infinite;
}
.home-demo-toast {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 20;
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-width: min(360px, calc(100% - 32px));
  padding: 10px 14px;
  border-radius: 6px;
  background: #1b2030;
  color: #fff;
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.4);
  font-size: 13px;
}
.fade-enter-active, .fade-leave-active { transition: opacity 0.3s; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
@keyframes home-demo-spin { to { transform: rotate(360deg); } }

@media (max-width: 720px) {
  .home-demo-frame { height: 560px; }
  .home-demo-launcher { justify-content: flex-end; padding: 18px 14px 22px; }
  .home-demo-launcher h2 { margin-bottom: 14px; font-size: 20px; }
  .home-demo-start { width: min(100%, 240px); }
  .home-demo-poster img { object-position: 35% top; }
}

@media (prefers-reduced-motion: reduce) {
  .home-demo-spinner { animation: none; }
}
</style>
