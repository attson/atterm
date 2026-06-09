<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import type { Endpoint } from '../lib/connection'
import { relayBaseToWsUrl } from './relay'
import MobileSetup from './MobileSetup.vue'
import MobileSessionList from './MobileSessionList.vue'
import MobileSettings from './MobileSettings.vue'
import MobileTerminalHost, { type OpenTerminal } from './MobileTerminalHost.vue'

const MAX_OPEN_TERMINALS = 4

type View = 'setup' | 'list' | 'terminal' | 'settings'
const platform = usePlatform()

const view = ref<View>('setup')
const reason = ref<'token_invalid' | null>(null)
const openTerminals = ref<OpenTerminal[]>([])   // stable open order (tab strip order)
const recency = ref<string[]>([])               // MRU order (last = most recent), for eviction
const activeSessionId = ref<string>('')
const endpoint = ref<Endpoint>({ url: '', session_token: '' })

const openSessionIds = computed(() => openTerminals.value.map((t) => t.sessionId))

async function refreshEndpoint(): Promise<void> {
  const cfg = await platform.relay.load()
  if (cfg) endpoint.value = { url: relayBaseToWsUrl(cfg.url), session_token: cfg.token }
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

function touchRecency(sessionId: string): void {
  const i = recency.value.indexOf(sessionId)
  if (i >= 0) recency.value.splice(i, 1)
  recency.value.push(sessionId)
}

// activate switches the visible terminal WITHOUT reordering the tab strip —
// tabs keep their open order, only recency (used for eviction) moves.
function activate(sessionId: string): void {
  touchRecency(sessionId)
  activeSessionId.value = sessionId
  view.value = 'terminal'
}

function onOpenSession(info: RemoteSession): void {
  const existing = openTerminals.value.find((t) => t.sessionId === info.session_id)
  if (existing) { activate(info.session_id); return }
  if (openTerminals.value.length >= MAX_OPEN_TERMINALS) {
    const victim = recency.value.find((id) => id !== info.session_id) ?? openTerminals.value[0]?.sessionId
    if (victim) removeTerminal(victim)
  }
  openTerminals.value.push({ sessionId: info.session_id, info })
  activate(info.session_id)
}

function onSwitch(sessionId: string): void { activate(sessionId) }

function onMeta(sessionId: string, meta: { cwd?: string; title?: string }): void {
  const t = openTerminals.value.find((t) => t.sessionId === sessionId)
  if (!t) return
  if (meta.cwd !== undefined) t.info.cwd = meta.cwd
  if (meta.title) t.info.title = meta.title
}

function removeTerminal(sessionId: string): void {
  const idx = openTerminals.value.findIndex((t) => t.sessionId === sessionId)
  if (idx < 0) return
  openTerminals.value.splice(idx, 1)
  const r = recency.value.indexOf(sessionId)
  if (r >= 0) recency.value.splice(r, 1)
  if (activeSessionId.value === sessionId) {
    const next = recency.value[recency.value.length - 1]
    if (next) { activeSessionId.value = next; view.value = 'terminal' }
    else { activeSessionId.value = ''; view.value = 'list' }
  }
}

function onClose(sessionId: string): void { removeTerminal(sessionId) }
function onEnded(sessionId: string): void { removeTerminal(sessionId) }
function onBack(): void { view.value = 'list' }
function onOpenSettings(): void { view.value = 'settings' }
function onSettingsBack(): void { view.value = 'list' }

// Logout returns to the connect screen but deliberately preserves the saved
// config (no relay.clear()): MobileSetup re-fills url + token from Keychain so
// the user can reconnect or tweak without re-entering credentials. Open
// terminals are torn down so we don't leave WS connections behind a screen the
// user logically left (same teardown as onTokenInvalid).
function onLogout(): void {
  openTerminals.value = []
  recency.value = []
  activeSessionId.value = ''
  reason.value = null
  view.value = 'setup'
}

function onTokenInvalid(): void {
  openTerminals.value = []
  recency.value = []
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
    @open-settings="onOpenSettings"
    @token-invalid="onTokenInvalid"
  />
  <MobileSettings
    v-else-if="view === 'settings'"
    @back="onSettingsBack"
    @logout="onLogout"
  />
  <!-- Stay mounted whenever sessions are open so going back to the list does
       NOT tear down the WS connections. A remount would mint a fresh clientID,
       and since the upstream host keeps the old id as driver, the phone would
       re-attach as a viewer and deadlock both ends on the viewer overlay. -->
  <MobileTerminalHost
    v-if="openTerminals.length > 0"
    v-show="view === 'terminal'"
    :endpoint="endpoint"
    :open-terminals="openTerminals"
    :active-session-id="activeSessionId"
    @switch="onSwitch"
    @close="onClose"
    @back="onBack"
    @ended="onEnded"
    @meta="onMeta"
    @token-invalid="onTokenInvalid"
  />
</template>
