<script setup lang="ts">
import MobileTerminal from './MobileTerminal.vue'
import type { Endpoint } from '../lib/connection'
import type { RemoteSession } from '../platform/types'

export interface OpenTerminal { sessionId: string; info: RemoteSession }

const props = defineProps<{
  endpoint: Endpoint
  openTerminals: OpenTerminal[]
  activeSessionId: string
}>()
const emit = defineEmits<{
  (e: 'switch', sessionId: string): void
  (e: 'close', sessionId: string): void
  (e: 'back'): void
  (e: 'ended', sessionId: string): void
  (e: 'meta', sessionId: string, meta: { cwd?: string; title?: string }): void
  (e: 'tokenInvalid'): void
}>()

function activeInfo(): RemoteSession | undefined {
  return props.openTerminals.find((t) => t.sessionId === props.activeSessionId)?.info
}

// Tab label = basename of the live cwd (mirrors desktop TabBar), falling back
// to the first word of the title/command when cwd is not known yet.
function shortTitle(info: RemoteSession): string {
  const stripped = (info.cwd ?? '').replace(/\/+$/, '')
  if (info.cwd) {
    if (stripped === '') return '/'
    const base = stripped.split('/').pop()
    if (base) return base
  }
  const first = (info.title || '').split(/\s+/)[0] || 'shell'
  return first.split('/').pop() || first
}

function activeCwd(): string {
  const info = activeInfo()
  if (!info) return ''
  return info.cwd || info.title || ''
}

function formatWho(info?: RemoteSession): string {
  if (!info) return ''
  const u = info.user || ''
  const h = info.host || ''
  if (u && h) return `${u}@${h}`
  return h || u || ''
}
</script>

<template>
  <div class="host">
    <header class="bar">
      <button data-testid="term-back" class="back" @click="emit('back')" aria-label="Back">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6" /></svg>
      </button>
      <div class="head-text">
        <span class="title">{{ activeCwd() }}</span>
        <span v-if="formatWho(activeInfo())" class="who" data-testid="term-who">{{ formatWho(activeInfo()) }}</span>
      </div>
    </header>
    <div class="tabstrip">
      <div
        v-for="t in openTerminals"
        :key="t.sessionId"
        data-testid="term-tab"
        class="tab"
        :class="{ active: t.sessionId === activeSessionId }"
        @click="emit('switch', t.sessionId)"
      >
        <span class="lbl">{{ shortTitle(t.info) }}</span>
        <span :data-testid="`tab-close-${t.sessionId}`" class="x" @click.stop="emit('close', t.sessionId)">×</span>
      </div>
    </div>
    <div class="stage">
      <div
        v-for="t in openTerminals"
        :key="t.sessionId"
        v-show="t.sessionId === activeSessionId"
        class="pane"
      >
        <MobileTerminal
          :endpoint="endpoint"
          :session-id="t.sessionId"
          :info="t.info"
          :active="t.sessionId === activeSessionId"
          @ended="emit('ended', t.sessionId)"
          @meta="(m) => emit('meta', t.sessionId, m)"
          @token-invalid="emit('tokenInvalid')"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.host { display: flex; flex-direction: column; height: 100vh; box-sizing: border-box; padding: env(safe-area-inset-top) 0 env(safe-area-inset-bottom); background: #000; color: #e6e7ea; }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 8px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.back { display: inline-flex; align-items: center; justify-content: center; background: none; border: none; color: #3b82f6; width: 28px; padding: 0; }
.head-text { flex: 1; min-width: 0; display: flex; flex-direction: column; justify-content: center; line-height: 1.2; }
.title { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.95rem; }
.who { font-size: 0.72rem; color: #8d93a3; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tabstrip { display: flex; gap: 6px; padding: 7px 8px; background: #0b1020; border-bottom: 1px solid #1e2638; overflow-x: auto; }
.tab { flex: 0 0 auto; height: 28px; padding: 0 8px; border-radius: 7px; display: flex; align-items: center; gap: 6px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; }
.tab.active { background: rgba(59,130,246,.16); border-color: rgba(59,130,246,.5); color: #cfe0ff; }
.tab .x { color: #5b6478; font-size: 0.85rem; }
.stage { flex: 1; min-height: 0; position: relative; }
.pane { position: absolute; inset: 0; }
</style>
