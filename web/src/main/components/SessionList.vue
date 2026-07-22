<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NButton, NTag } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listSessions } from '@shared/api/sessions'
import { hasAccountKey } from '@shared/api/account-key'
import { loadRelayConfig } from '@shared/api/relay-config'
import type { SessionInfo } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'
import { displayForType } from '@shared/sessionType'

const POLL_MS = 2000

const emit = defineEmits<{
  (e: 'navigate', sessionId: string): void
}>()

const rows = ref<SessionInfo[]>([])
const loading = ref(true)
const errorMsg = ref('')
const needsAccountUnlock = ref(false)
const { t } = useI18n()
let pollHandle: ReturnType<typeof setInterval> | null = null

function hasSessionWithoutAccountKey(): boolean {
  const cfg = loadRelayConfig()
  return Boolean(cfg?.sessionToken) && !hasAccountKey()
}

const unlockHref = computed(() => {
  const next = `${location.pathname || '/'}${location.search || ''}${location.hash || ''}`
  return `/login.html?next=${encodeURIComponent(next || '/')}`
})

async function reload() {
  if (hasSessionWithoutAccountKey()) {
    needsAccountUnlock.value = true
    rows.value = []
    errorMsg.value = ''
    loading.value = false
    return
  }
  needsAccountUnlock.value = false
  try {
    rows.value = await listSessions()
    errorMsg.value = ''
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('main.errors.loadSessions')
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
      byHost.set(key, { hostId: s.host_id, hostname: s.host || t('main.unknownHost'), sessions: [s] })
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

function sessionCount(count: number): string {
  return t(count === 1 ? 'main.sessionCountOne' : 'main.sessionCount', { count })
}

function typeForSession(s: SessionInfo) {
  return displayForType(s.type)
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
      <h1>{{ t('main.activeSessions') }}</h1>
      <n-button size="small" tertiary @click="reload">{{ t('main.refresh') }}</n-button>
    </div>

    <p v-if="!loading && !needsAccountUnlock && rows.length === 0" class="empty">
      {{ t('main.empty') }} <code>atterm-agent</code>.
    </p>

    <div v-if="!loading && needsAccountUnlock" class="unlock-panel" role="status">
      <h2>{{ t('main.unlock.title') }}</h2>
      <p>{{ t('main.unlock.text') }}</p>
      <a
        class="unlock-link"
        data-testid="unlock-account-key"
        :href="unlockHref"
      >{{ t('main.unlock.action') }}</a>
    </div>

    <template v-if="!needsAccountUnlock">
      <div v-for="g in groups" :key="g.hostId || g.hostname" class="host-group">
        <header>
          <span class="hostname">{{ g.hostname }}</span>
          <n-tag v-if="g.hostId" size="small" type="default">
            <code>{{ shortID(g.hostId) }}</code>
          </n-tag>
          <span class="count">{{ sessionCount(g.sessions.length) }}</span>
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
            <div class="cmd">
              <span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s)!.color }">
                {{ t(`main.taskTypes.${typeForSession(s)!.key}`) }}
              </span>
              {{ (s.type === 'ai' && s.title) ? s.title : (s.command || t('main.unknownCommand')) }}
            </div>
            <div class="meta">
              <span class="id"><code>{{ shortID(s.id) }}</code></span>
              <span class="size">{{ s.cols }}×{{ s.rows }}</span>
              <span class="cwd">{{ s.cwd }}</span>
            </div>
            <span
              v-if="s.task_state === 'failed' && s.summary?.error_lines?.length"
              class="err-line"
              :data-testid="`task-err-${s.id}`"
            >{{ s.summary.error_lines[0] }}</span>
          </button>
        </div>
      </div>
    </template>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </section>
</template>

<style scoped>
.session-list {
  width: 100%;
  max-width: 980px;
  margin: 0 auto;
  padding: 2rem 1rem;
  box-sizing: border-box;
  color: var(--fg);
}
.section-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1.5rem; }
.section-head h1 { margin: 0; font-size: 1.5rem; letter-spacing: 0.08em; }
.host-group { margin-bottom: 1.5rem; }
.host-group header { display: flex; gap: 0.75rem; align-items: center; margin-bottom: 0.75rem; }
.hostname { font-weight: 600; color: var(--fg); }
.count { color: var(--fg-dim); font-size: 0.875rem; margin-left: auto; }
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 0.75rem;
}
.card {
  min-width: 0;
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
.type-chip {
  font-size: 0.66rem; line-height: 1;
  padding: 2px 6px; border-radius: 4px;
  border: 1px solid color-mix(in srgb, var(--chip) 60%, transparent);
  color: var(--chip);
  background: color-mix(in srgb, var(--chip) 12%, transparent);
  text-transform: uppercase; letter-spacing: 0.04em;
  margin-right: 6px;
}
.meta { display: flex; gap: 0.75rem; color: var(--fg-dim); font-size: 0.75rem; flex-wrap: wrap; }
.meta .size { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.meta .cwd { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; }
.err-line {
  display: block;
  color: #f87171;
  font-size: 0.72rem;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.empty { color: var(--fg-dim); font-size: 0.875rem; }
.unlock-panel {
  max-width: 520px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  padding: 1rem;
}
.unlock-panel h2 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
}
.unlock-panel p {
  margin: 0 0 0.875rem;
  color: var(--fg-dim);
  line-height: 1.5;
}
.unlock-link {
  display: inline-flex;
  align-items: center;
  min-height: 34px;
  padding: 0 0.875rem;
  border-radius: 4px;
  background: var(--accent);
  color: #06101f;
  font-weight: 700;
  text-decoration: none;
}
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
