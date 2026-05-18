<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NConfigProvider, NMessageProvider, NButton, darkTheme } from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import Topbar from '@shared/components/Topbar.vue'
import SessionList from './components/SessionList.vue'
import TerminalView from './components/TerminalView.vue'
import ShortcutBar from './components/ShortcutBar.vue'
import PasteFallback from './components/PasteFallback.vue'
import InstallHint from './components/InstallHint.vue'
import { parseSessionRoute, formatSessionRoute } from './lib/sessionRoute'

const sessionId = ref<string | null>(parseSessionRoute(location.hash))
const pasteOpen = ref(false)
const termRef = ref<InstanceType<typeof TerminalView> | null>(null)

function onHashChange() {
  sessionId.value = parseSessionRoute(location.hash)
}

function onNavigate(id: string) {
  location.hash = formatSessionRoute(id)
}

function onBack() {
  location.hash = ''
}

function onShortcutInput(text: string) {
  termRef.value?.sendInput(text)
}

async function onShortcutPaste() {
  try {
    const text = await navigator.clipboard.readText()
    if (text) {
      termRef.value?.sendInput(text)
      return
    }
  } catch {
    /* clipboard read denied or unavailable */
  }
  pasteOpen.value = true
}

function onShortcutCopy() {
  termRef.value?.copySelection()
}

function onPasteText(text: string) {
  termRef.value?.sendInput(text)
}

function onPasteImage(file: File) {
  void termRef.value?.sendPasteImage(file, file.name)
}

const overrides = getNaiveOverrides()

const inSession = computed(() => Boolean(sessionId.value))

onMounted(() => window.addEventListener('hashchange', onHashChange))
onUnmounted(() => window.removeEventListener('hashchange', onHashChange))
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <Topbar active="home" />
      <InstallHint v-if="!inSession" />

      <div v-if="inSession" class="page-bar">
        <n-button size="small" tertiary aria-label="Back to sessions" @click="onBack">
          ← back
        </n-button>
        <div class="subtitle">terminal</div>
      </div>

      <main class="home-main">
        <SessionList v-if="!inSession" @navigate="onNavigate" />
        <template v-else>
          <TerminalView
            ref="termRef"
            :session-id="sessionId!"
          />
          <ShortcutBar
            @input="onShortcutInput"
            @copy="onShortcutCopy"
            @paste="onShortcutPaste"
          />
          <PasteFallback
            :open="pasteOpen"
            @paste-text="onPasteText"
            @paste-image="onPasteImage"
            @close="pasteOpen = false"
          />
        </template>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.page-bar {
  max-width: 980px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 1rem 1rem 0;
  color: var(--fg-dim);
  font-size: 0.875rem;
}
.subtitle { font-weight: 600; color: var(--fg); }
.home-main {
  display: flex;
  flex-direction: column;
  min-height: calc(100vh - 140px);
}
</style>
