<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import { groupByHost, type HostGroup } from './sessionGroups'

defineProps<{ openSessionIds: string[] }>()
const emit = defineEmits<{
  (e: 'open', info: RemoteSession): void
  (e: 'editRelay'): void
  (e: 'tokenInvalid'): void
}>()

const platform = usePlatform()
const groups = ref<HostGroup[]>([])
const error = ref<string | null>(null)
const loading = ref(false)

async function refresh(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const sessions = await platform.sessions.listRemoteSessions()
    groups.value = groupByHost(sessions)
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'relay_unauthorized') { emit('tokenInvalid'); return }
    error.value = msg
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="list">
    <header class="bar">
      <span class="title">Sessions</span>
      <button data-testid="refresh" class="icon" :disabled="loading" @click="refresh">⟳</button>
      <button data-testid="gear" class="icon" @click="emit('editRelay')">⚙</button>
    </header>
    <div class="body">
      <p v-if="error" class="error">{{ error }}</p>
      <div v-for="g in groups" :key="g.host" class="group">
        <div data-testid="host-group" class="grouphdr">{{ g.host }} · {{ g.user }} · {{ g.sessions.length }}</div>
        <button
          v-for="s in g.sessions"
          :key="s.session_id"
          data-testid="session-row"
          class="sess"
          @click="emit('open', s)"
        >
          <span class="dot"></span>
          <span class="col2">
            <span class="ttl">{{ s.title }}</span>
            <span class="meta">{{ s.cols }}×{{ s.rows }}</span>
          </span>
          <span v-if="openSessionIds.includes(s.session_id)" :data-testid="`open-badge-${s.session_id}`" class="open">open</span>
        </button>
      </div>
      <p v-if="!loading && !error && groups.length === 0" class="empty">
        no remote sessions — start one from a desktop AT Term connected to this relay.
      </p>
    </div>
  </div>
</template>

<style scoped>
.list { min-height: 100vh; display: flex; flex-direction: column; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.bar .title { flex: 1; font-weight: 600; }
.icon { background: none; border: none; color: #8d93a3; font-size: 1.1rem; }
.body { flex: 1; overflow: auto; padding: 12px; }
.group { margin-bottom: 14px; }
.grouphdr { font-size: 0.72rem; color: #8d93a3; font-family: ui-monospace, Menlo, monospace; margin: 4px 2px 8px; }
.sess { width: 100%; display: flex; align-items: center; gap: 10px; padding: 11px 12px; margin-bottom: 8px; border-radius: 11px; background: #11182b; border: 1px solid #1e2638; color: inherit; text-align: left; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #22c55e; flex: 0 0 auto; }
.col2 { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.ttl { font-size: 0.9rem; font-weight: 600; }
.meta { font-size: 0.72rem; color: #8d93a3; font-family: ui-monospace, Menlo, monospace; }
.open { font-size: 0.62rem; color: #9dc1ff; border: 1px solid rgba(59,130,246,.4); background: rgba(59,130,246,.12); border-radius: 5px; padding: 1px 6px; }
.empty { color: #8d93a3; font-size: 0.85rem; text-align: center; padding: 40px 12px; line-height: 1.6; }
.error { color: #f87171; font-size: 0.8rem; }
</style>
