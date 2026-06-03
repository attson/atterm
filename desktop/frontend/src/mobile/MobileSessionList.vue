<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import { useI18n } from '../i18n/useI18n'
import { displayForType } from '../lib/sessionType'

defineProps<{ openSessionIds: string[] }>()
const emit = defineEmits<{
  (e: 'open', info: RemoteSession): void
  (e: 'editRelay'): void
  (e: 'tokenInvalid'): void
}>()

const platform = usePlatform()
const sessions = ref<RemoteSession[]>([])
const error = ref<string | null>(null)
const loading = ref(false)
const { t } = useI18n()

type TaskBucket = 'needs_attention' | 'running' | 'completed' | 'failed' | 'disconnected'

const bucketOrder: TaskBucket[] = ['needs_attention', 'running', 'failed', 'completed', 'disconnected']

const taskGroups = computed(() => {
  return bucketOrder
    .map((bucket) => ({
      bucket,
      sessions: sessions.value.filter((session) => bucketFor(session) === bucket),
    }))
    .filter((group) => group.sessions.length > 0)
})

async function refresh(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    sessions.value = await platform.sessions.listRemoteSessions()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'relay_unauthorized') { emit('tokenInvalid'); return }
    error.value = msg
    sessions.value = []
  } finally {
    loading.value = false
  }
}

onMounted(refresh)

function bucketFor(session: RemoteSession): TaskBucket {
  switch (session.task_state) {
    case 'waiting_input':
      return 'needs_attention'
    case 'failed':
      return 'failed'
    case 'completed':
      return 'completed'
    case 'disconnected':
    case 'closed':
      return 'disconnected'
    case 'running':
    case 'idle':
    default:
      return 'running'
  }
}

function taskTitle(session: RemoteSession): string {
  return session.current_command || session.title || 'shell'
}

function typeForSession(s: RemoteSession) {
  return displayForType(s.type)
}

function taskMeta(session: RemoteSession): string {
  const parts = [
    `${session.host} · ${session.user}`,
    taskStateLabel(session),
    `${session.cols}×${session.rows}`,
    session.remote_permission || 'full',
  ]
  const elapsed = formatDuration(session.command_duration_ms)
  if (elapsed) parts.push(elapsed)
  else if (session.command_started_at) parts.push(`${t('mobile.started')} ${formatClock(session.command_started_at)}`)
  if (session.command_exit_code !== undefined) parts.push(`exit ${session.command_exit_code}`)
  if (session.last_output_at) parts.push(`${t('mobile.lastOutput')} ${formatClock(session.last_output_at)}`)
  return parts.join(' · ')
}

function taskStateLabel(session: RemoteSession): string {
  return t(`mobile.taskStates.${session.task_state || 'idle'}`)
}

function formatDuration(ms?: number): string {
  if (ms === undefined || ms < 0) return ''
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s`
  return `${Math.floor(sec / 60)}m${sec % 60}s`
}

function formatClock(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="list">
    <header class="bar">
      <span class="title">{{ t('mobile.sessionsTitle') }}</span>
      <button data-testid="refresh" class="icon" :disabled="loading" @click="refresh" :aria-label="t('common.refresh')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36" /><path d="M21 3v6h-6" /></svg>
      </button>
      <button data-testid="gear" class="icon" @click="emit('editRelay')" :aria-label="t('common.settings')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" /></svg>
      </button>
    </header>
    <div class="body">
      <p v-if="error" data-testid="relay-disconnected" class="empty disconnected">
        {{ t('mobile.relayDisconnected') }} · {{ error }}
      </p>
      <div v-for="g in taskGroups" :key="g.bucket" class="group" :data-testid="`task-section-${g.bucket}`">
        <div class="grouphdr">{{ t(`mobile.taskSections.${g.bucket}`) }} · {{ g.sessions.length }}</div>
        <button
          v-for="s in g.sessions"
          :key="s.session_id"
          data-testid="task-card"
          class="task"
          :class="[`state-${bucketFor(s)}`]"
          @click="emit('open', s)"
        >
          <span class="dot"></span>
          <span :data-testid="`task-card-${s.session_id}`" class="col2">
            <span class="title-row">
              <span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s)!.color }">
                {{ t(`mobile.taskTypes.${typeForSession(s)!.key}`) }}
              </span>
              <span class="ttl">{{ taskTitle(s) }}</span>
            </span>
            <span v-if="s.cwd" :data-testid="`session-cwd-${s.session_id}`" class="cwd">{{ s.cwd }}</span>
            <span class="meta">{{ taskMeta(s) }}</span>
          </span>
          <span v-if="openSessionIds.includes(s.session_id)" :data-testid="`open-badge-${s.session_id}`" class="open">{{ t('mobile.openBadge') }}</span>
        </button>
      </div>
      <p v-if="!loading && !error && taskGroups.length === 0" class="empty">
        {{ t('mobile.noRemoteSessions') }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.list { min-height: 100vh; box-sizing: border-box; padding: env(safe-area-inset-top) 0 env(safe-area-inset-bottom); display: flex; flex-direction: column; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: var(--font-sans); }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.bar .title { flex: 1; font-weight: 600; }
.icon { display: inline-flex; align-items: center; justify-content: center; background: none; border: none; color: #8d93a3; padding: 4px; }
.body { flex: 1; overflow: auto; padding: 12px; }
.group { margin-bottom: 14px; }
.grouphdr { font-size: 0.72rem; color: #8d93a3; font-family: var(--font-mono); margin: 4px 2px 8px; }
.task { width: 100%; display: flex; align-items: center; gap: 10px; padding: 11px 12px; margin-bottom: 8px; border-radius: 11px; background: #11182b; border: 1px solid #1e2638; color: inherit; text-align: left; }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #22c55e; flex: 0 0 auto; }
.state-needs_attention .dot { background: #f59e0b; box-shadow: 0 0 14px rgba(245, 158, 11, .55); }
.state-failed .dot { background: #ef4444; box-shadow: 0 0 14px rgba(239, 68, 68, .45); }
.state-completed .dot { background: #60a5fa; }
.state-disconnected .dot { background: #64748b; }
.col2 { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.title-row { display: flex; align-items: center; gap: 6px; min-width: 0; }
.type-chip {
  font-size: 0.66rem; line-height: 1;
  padding: 2px 6px; border-radius: 4px;
  border: 1px solid color-mix(in srgb, var(--chip) 60%, transparent);
  color: var(--chip);
  background: color-mix(in srgb, var(--chip) 12%, transparent);
  text-transform: uppercase; letter-spacing: 0.04em;
  flex: 0 0 auto;
}
.ttl { font-size: 0.9rem; font-weight: 600; }
.cwd { font-size: 0.74rem; color: #9aa3b2; font-family: var(--font-mono); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.meta { font-size: 0.72rem; color: #8d93a3; font-family: var(--font-mono); }
.open { font-size: 0.62rem; color: #9dc1ff; border: 1px solid rgba(59,130,246,.4); background: rgba(59,130,246,.12); border-radius: 5px; padding: 1px 6px; }
.empty { color: #8d93a3; font-size: 0.85rem; text-align: center; padding: 40px 12px; line-height: 1.6; }
.disconnected { color: #f87171; }
</style>
