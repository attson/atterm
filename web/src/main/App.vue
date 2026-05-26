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
import { useI18n } from '@shared/i18n/useI18n'
import { naiveLocale } from '@shared/i18n/naive-locale'

const sessionId = ref<string | null>(parseSessionRoute(location.hash))
const pasteOpen = ref(false)
const termRef = ref<InstanceType<typeof TerminalView> | null>(null)
const { t } = useI18n()

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
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale.locale"
    :date-locale="naiveLocale.dateLocale"
  >
    <n-message-provider>
      <Topbar active="home" />
      <InstallHint v-if="!inSession" />

      <n-button
        v-if="inSession"
        class="back-floating"
        size="small"
        tertiary
        :aria-label="t('main.backToSessions')"
        @click="onBack"
      >
        ← {{ t('main.back') }}
      </n-button>

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
/* Floating "back to sessions" button — sits at top-left under the topbar,
   does not occupy layout flow, so xterm can use the full area. Half-faded
   by default so it does not visually compete with terminal output; opaque
   on hover. */
.back-floating {
  position: fixed;
  top: 88px;
  right: 0.75rem;
  z-index: 5;
  opacity: 0.45;
  transition: opacity 0.15s ease;
}
.back-floating:hover { opacity: 1; }
.home-main {
  display: flex;
  flex-direction: column;
  min-height: calc(100vh - 80px);
}
</style>
