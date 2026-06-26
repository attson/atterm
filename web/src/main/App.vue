<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NConfigProvider, NMessageProvider, NButton, NAlert, darkTheme } from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import Topbar from '@shared/components/Topbar.vue'
import SessionList from './components/SessionList.vue'
import TerminalView from './components/TerminalView.vue'
import ShortcutBar from './components/ShortcutBar.vue'
import PasteFallback from './components/PasteFallback.vue'
import PasteImagePreviewHost from './components/PasteImagePreviewHost.vue'
import { pasteImageBus } from './lib/pasteImageBus'
import InstallHint from './components/InstallHint.vue'
import ConnHealthPill from '@shared/components/ConnHealthPill.vue'
import ConnHealthDrawer from '@shared/components/ConnHealthDrawer.vue'
import { parseSessionRoute, parseSessionRouteAction, formatSessionRoute } from './lib/sessionRoute'
import { useI18n } from '@shared/i18n/useI18n'
import { naiveLocale } from '@shared/i18n/naive-locale'
import type { ConnHealthSnapshot } from '@shared/connhealth/connhealth'

const sessionId = ref<string | null>(parseSessionRoute(location.hash))
const routeAction = ref(parseSessionRouteAction(location.hash))
const pasteOpen = ref(false)
const termRef = ref<InstanceType<typeof TerminalView> | null>(null)
const { t } = useI18n()

const connHealth = ref<ConnHealthSnapshot | null>(null)
const connHealthDrawerOpen = ref(false)
let connHealthTimer: ReturnType<typeof setTimeout> | null = null
let connHealthCancelled = false

function pollConnHealth() {
  if (connHealthTimer !== null) clearTimeout(connHealthTimer)
  if (connHealthCancelled) return
  const ref = termRef.value as (InstanceType<typeof TerminalView> & { getHealth?: () => ConnHealthSnapshot | null }) | null
  const snap = typeof ref?.getHealth === 'function' ? ref.getHealth() : null
  connHealth.value = snap ?? null
  const delay = connHealthDrawerOpen.value ? 1000 : 5000
  connHealthTimer = setTimeout(pollConnHealth, delay)
}

const pillLabels = computed(() => ({
  connecting: t('connHealth.pillLabelConnecting'),
  reconnecting: t('connHealth.pillLabelReconnecting'),
  off: t('connHealth.pillLabelOff'),
}))

const drawerLabels = computed(() => ({
  title: t('connHealth.drawerTitle'),
  rttNow: t('connHealth.drawerRttNow'),
  rttP50P95: t('connHealth.drawerRttP50P95'),
  bytesIn: t('connHealth.drawerBytesIn'),
  bytesOut: t('connHealth.drawerBytesOut'),
  state: t('connHealth.drawerState'),
  reconnectsLastHour: t('connHealth.drawerReconnectsLastHour'),
  reconnectsTime: t('connHealth.drawerReconnectsTime'),
  reconnectsReason: t('connHealth.drawerReconnectsReason'),
  reconnectsDowntime: t('connHealth.drawerReconnectsDowntime'),
  seqGaps: t('connHealth.drawerSeqGaps'),
  close: t('common.close'),
}))

function onHashChange() {
  sessionId.value = parseSessionRoute(location.hash)
  routeAction.value = parseSessionRouteAction(location.hash)
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
  pasteImageBus.emit(file, file.name)
  void termRef.value?.sendPasteImage(file, file.name)
}

const overrides = getNaiveOverrides()

const inSession = computed(() => Boolean(sessionId.value))
const focusInput = computed(() => routeAction.value.focus === 'input')
const viewOnlyNotice = computed(() => routeAction.value.permission === 'view')

onMounted(() => {
  window.addEventListener('hashchange', onHashChange)
  pollConnHealth()
})
onUnmounted(() => {
  window.removeEventListener('hashchange', onHashChange)
  connHealthCancelled = true
  if (connHealthTimer !== null) clearTimeout(connHealthTimer)
})
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale.locale"
    :date-locale="naiveLocale.dateLocale"
  >
    <n-message-provider>
      <PasteImagePreviewHost />
      <Topbar active="home" />
      <div v-if="inSession && connHealth" class="conn-health-anchor">
        <ConnHealthPill
          :health="connHealth"
          :labels="pillLabels"
          @click="connHealthDrawerOpen = !connHealthDrawerOpen"
        />
        <ConnHealthDrawer
          :health="connHealth"
          :open="connHealthDrawerOpen"
          :labels="drawerLabels"
          @close="connHealthDrawerOpen = false"
        />
      </div>
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
          <n-alert
            v-if="viewOnlyNotice"
            type="info"
            class="permission-notice"
            data-testid="view-only-notice"
          >
            {{ t('main.viewOnlyNotice') }}
          </n-alert>
          <TerminalView
            ref="termRef"
            :session-id="sessionId!"
            :focus-input="focusInput"
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
/* Conn-health pill floats next to the back button, top-right corner under
   the topbar. The pill itself doesn't occupy layout flow; the drawer is
   fixed-positioned per its own styles. */
.conn-health-anchor {
  position: fixed;
  top: 88px;
  right: 9.5rem;
  z-index: 5;
}
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
.permission-notice {
  margin: 0.75rem 0.75rem 0;
  z-index: 2;
}
</style>
