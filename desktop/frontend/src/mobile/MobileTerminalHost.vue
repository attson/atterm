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
  (e: 'tokenInvalid'): void
}>()

function activeTitle(): string {
  return props.openTerminals.find((t) => t.sessionId === props.activeSessionId)?.info.title ?? ''
}
</script>

<template>
  <div class="host">
    <header class="bar">
      <button data-testid="term-back" class="back" @click="emit('back')">‹</button>
      <span class="title">{{ activeTitle() }}</span>
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
        <span class="lbl">{{ t.info.title }}</span>
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
          @token-invalid="emit('tokenInvalid')"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.host { display: flex; flex-direction: column; height: 100vh; background: #000; color: #e6e7ea; }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 8px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.back { background: none; border: none; color: #3b82f6; font-size: 1.5rem; width: 28px; }
.title { flex: 1; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 0.95rem; }
.tabstrip { display: flex; gap: 6px; padding: 7px 8px; background: #0b1020; border-bottom: 1px solid #1e2638; overflow-x: auto; }
.tab { flex: 0 0 auto; height: 28px; padding: 0 8px; border-radius: 7px; display: flex; align-items: center; gap: 6px; background: #11182b; border: 1px solid #1e2638; color: #8d93a3; font-size: 0.75rem; }
.tab.active { background: rgba(59,130,246,.16); border-color: rgba(59,130,246,.5); color: #cfe0ff; }
.tab .x { color: #5b6478; font-size: 0.85rem; }
.stage { flex: 1; min-height: 0; position: relative; }
.pane { position: absolute; inset: 0; }
</style>
