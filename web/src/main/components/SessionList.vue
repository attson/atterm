<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NButton, NTag } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listSessions } from '@shared/api/sessions'
import type { SessionInfo } from '@shared/api/types'

const POLL_MS = 2000

const emit = defineEmits<{
  (e: 'navigate', sessionId: string): void
}>()

const rows = ref<SessionInfo[]>([])
const loading = ref(true)
const errorMsg = ref('')
let pollHandle: ReturnType<typeof setInterval> | null = null

async function reload() {
  try {
    rows.value = await listSessions()
    errorMsg.value = ''
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Failed to load sessions.'
  } finally {
    loading.value = false
  }
}

interface HostGroup {
  hostId: string
  hostname: string
  sessions: SessionInfo[]
}

const groups = computed<HostGroup[]>(() => {
  const byHost = new Map<string, HostGroup>()
  for (const s of rows.value) {
    const key = s.host_id || s.host || ''
    const existing = byHost.get(key)
    if (existing) {
      existing.sessions.push(s)
    } else {
      byHost.set(key, { hostId: s.host_id, hostname: s.host || 'unknown host', sessions: [s] })
    }
  }
  return Array.from(byHost.values())
})

function shortID(id: string): string {
  return id.slice(0, 8)
}

function onCardClick(id: string) {
  emit('navigate', id)
}

onMounted(async () => {
  await reload()
  pollHandle = setInterval(reload, POLL_MS)
})

onUnmounted(() => {
  if (pollHandle !== null) clearInterval(pollHandle)
  pollHandle = null
})
</script>

<template>
  <section class="session-list">
    <div class="section-head">
      <h1>active sessions</h1>
      <n-button size="small" tertiary @click="reload">refresh</n-button>
    </div>

    <p v-if="!loading && rows.length === 0" class="empty">
      No live sessions. Start one from the desktop app or <code>atterm-agent</code>.
    </p>

    <div v-for="g in groups" :key="g.hostId || g.hostname" class="host-group">
      <header>
        <span class="hostname">{{ g.hostname }}</span>
        <n-tag v-if="g.hostId" size="small" type="default">
          <code>{{ shortID(g.hostId) }}</code>
        </n-tag>
        <span class="count">{{ g.sessions.length }} {{ g.sessions.length === 1 ? 'session' : 'sessions' }}</span>
      </header>
      <div class="grid">
        <button
          v-for="s in g.sessions"
          :key="s.id"
          type="button"
          class="card"
          :data-testid="`session-card-${s.id}`"
          @click="onCardClick(s.id)"
        >
          <div class="cmd">{{ s.command || '(unknown)' }}</div>
          <div class="meta">
            <span class="id"><code>{{ shortID(s.id) }}</code></span>
            <span class="size">{{ s.cols }}×{{ s.rows }}</span>
            <span class="cwd">{{ s.cwd }}</span>
          </div>
        </button>
      </div>
    </div>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </section>
</template>

<style scoped>
.session-list {
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  color: var(--fg);
}
.section-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1.5rem; }
.section-head h1 { margin: 0; font-size: 1.5rem; letter-spacing: 0.08em; }
.host-group { margin-bottom: 1.5rem; }
.host-group header { display: flex; gap: 0.75rem; align-items: center; margin-bottom: 0.75rem; }
.hostname { font-weight: 600; color: var(--fg); }
.count { color: var(--fg-dim); font-size: 0.875rem; margin-left: auto; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 0.75rem; }
.card {
  background: var(--panel);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.75rem;
  text-align: left;
  cursor: pointer;
  font: inherit;
}
.card:hover { border-color: var(--accent); }
.cmd { font-weight: 600; margin-bottom: 0.5rem; }
.meta { display: flex; gap: 0.75rem; color: var(--fg-dim); font-size: 0.75rem; flex-wrap: wrap; }
.meta .size { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.meta .cwd { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.empty { color: var(--fg-dim); font-size: 0.875rem; }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
