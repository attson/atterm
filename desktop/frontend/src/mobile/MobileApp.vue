<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import type { Endpoint } from '../lib/connection'
import { relayBaseToWsUrl } from './relay'
import MobileSetup from './MobileSetup.vue'
import MobileSessionList from './MobileSessionList.vue'
import MobileTerminalHost, { type OpenTerminal } from './MobileTerminalHost.vue'

const MAX_OPEN_TERMINALS = 4

type View = 'setup' | 'list' | 'terminal'
const platform = usePlatform()

const view = ref<View>('setup')
const reason = ref<'token_invalid' | null>(null)
const openTerminals = ref<OpenTerminal[]>([])   // insertion order = LRU recency
const activeSessionId = ref<string>('')
const endpoint = ref<Endpoint>({ url: '', token: '' })

const openSessionIds = computed(() => openTerminals.value.map((t) => t.sessionId))

async function refreshEndpoint(): Promise<void> {
  const cfg = await platform.relay.load()
  if (cfg) endpoint.value = { url: relayBaseToWsUrl(cfg.url), token: cfg.token }
}

onMounted(async () => {
  const cfg = await platform.relay.load()
  if (cfg) { await refreshEndpoint(); view.value = 'list' } else { view.value = 'setup' }
})

async function onConnected(): Promise<void> {
  await refreshEndpoint()
  reason.value = null
  view.value = 'list'
}

function activate(sessionId: string): void {
  const idx = openTerminals.value.findIndex((t) => t.sessionId === sessionId)
  if (idx >= 0) {
    const [t] = openTerminals.value.splice(idx, 1)
    openTerminals.value.push(t!)
  }
  activeSessionId.value = sessionId
  view.value = 'terminal'
}

function onOpenSession(info: RemoteSession): void {
  const existing = openTerminals.value.find((t) => t.sessionId === info.session_id)
  if (existing) { activate(info.session_id); return }
  if (openTerminals.value.length >= MAX_OPEN_TERMINALS) {
    openTerminals.value.shift()
  }
  openTerminals.value.push({ sessionId: info.session_id, info })
  activate(info.session_id)
}

function onSwitch(sessionId: string): void { activate(sessionId) }

function removeTerminal(sessionId: string): void {
  const idx = openTerminals.value.findIndex((t) => t.sessionId === sessionId)
  if (idx < 0) return
  openTerminals.value.splice(idx, 1)
  if (activeSessionId.value === sessionId) {
    const next = openTerminals.value[openTerminals.value.length - 1]
    if (next) { activeSessionId.value = next.sessionId; view.value = 'terminal' }
    else { view.value = 'list' }
  }
}

function onClose(sessionId: string): void { removeTerminal(sessionId) }
function onEnded(sessionId: string): void { removeTerminal(sessionId) }
function onBack(): void { view.value = 'list' }
function onEditRelay(): void { reason.value = null; view.value = 'setup' }

function onTokenInvalid(): void {
  openTerminals.value = []
  activeSessionId.value = ''
  reason.value = 'token_invalid'
  view.value = 'setup'
}
</script>

<template>
  <MobileSetup v-if="view === 'setup'" :reason="reason" @connected="onConnected" />
  <MobileSessionList
    v-else-if="view === 'list'"
    :open-session-ids="openSessionIds"
    @open="onOpenSession"
    @edit-relay="onEditRelay"
    @token-invalid="onTokenInvalid"
  />
  <MobileTerminalHost
    v-else
    :endpoint="endpoint"
    :open-terminals="openTerminals"
    :active-session-id="activeSessionId"
    @switch="onSwitch"
    @close="onClose"
    @back="onBack"
    @ended="onEnded"
    @token-invalid="onTokenInvalid"
  />
</template>
