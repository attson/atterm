<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import { useI18n } from '../i18n/useI18n'
import { useSessions } from '../composables/useSessions'
import { useTaskGroupBy } from '../composables/useTaskGroupBy'
import { useCollapsedGroups } from '../composables/useCollapsedGroups'
import { hostName as hostNameHelper, taskStateLabel } from '../lib/sessionLabel'
import { getUserHomeDir } from '../lib/api'
import type { TaskState } from '../lib/taskState'
import TaskStateIcon from '../components/TaskStateIcon.vue'
import MobileSessionCard from './MobileSessionCard.vue'

defineProps<{ openSessionIds: string[] }>()
const emit = defineEmits<{
  (e: 'open', info: RemoteSession): void
  (e: 'openSettings'): void
  (e: 'tokenInvalid'): void
}>()

const platform = usePlatform()
const { t } = useI18n()
const groupByState = useTaskGroupBy()
const groupBy = computed(() => groupByState.activeId.value)

const remote = ref<RemoteSession[]>([])
const local = ref<RemoteSession[]>([])  // mobile has no local PTYs
const sessions = useSessions(local, remote)

// Single source of truth for which sessions belong to the bottom fold —
// reuse useSessions.completedSeen verbatim so mobile and desktop never drift.
const foldedIds = computed(() => new Set(sessions.completedSeen.value.map((s) => s.session_id)))
function inFold(s: RemoteSession): boolean { return foldedIds.value.has(s.session_id) }

const error = ref<string | null>(null)
const loading = ref(false)
const foldOpen = ref(false)
const home = ref('')

// Collapse state lives in a module-scope composable so it survives the
// MobileApp v-if remount cycle (terminal → list, settings → list). Per-
// session only — no persistence to disk.
const { isCollapsed: isGroupCollapsed, toggle: toggleGroupCollapsed } = useCollapsedGroups()

const STATE_ORDER: TaskState[] = [
  'waiting_input', 'failed', 'running',
  'completed', 'idle', 'disconnected', 'closed',
]

// Group keys for whichever mode is active. State mode honours STATE_ORDER;
// host mode is alphabetical by host_id. Either way, drop groups that have no
// non-folded sessions (e.g. a host group whose only sessions are completed+seen).
const groupKeys = computed<string[]>(() => {
  const candidates = groupBy.value === 'state'
    ? STATE_ORDER.filter((s) => (sessions.byState.value[s] ?? []).length > 0)
    : Object.keys(sessions.byHost.value).sort()
  return candidates.filter((k) => byGroup(k).some((s) => !inFold(s)))
})

function byGroup(key: string): RemoteSession[] {
  if (groupBy.value === 'state') return sessions.byState.value[key] ?? []
  return sessions.byHost.value[key] ?? []
}

function unreadCount(key: string): number {
  if (groupBy.value === 'state') return sessions.unreadByState.value[key] ?? 0
  return sessions.unreadByHost.value[key] ?? 0
}

function primaryState(key: string): TaskState {
  if (groupBy.value === 'state') return key as TaskState
  return sessions.primaryStateForHost(key)
}

function groupHeader(key: string): string {
  if (groupBy.value === 'state') return taskStateLabel(key, t)
  return hostNameHelper(key, sessions.byHost.value[key], t('sessions.unknownHost'))
}

function unreadIdsForGroup(key: string): string[] {
  return byGroup(key).filter((s) => s.unread).map((s) => s.session_id)
}

function groupTestId(key: string): string {
  return groupBy.value === 'state' ? `state-group-${key}` : `host-group-${key}`
}

function groupMarkAllTestId(key: string): string {
  // Distinct prefix so [data-testid^="state-group-"] / [data-testid^="host-group-"]
  // does NOT pick up the ✓ button when querying for section headers.
  return groupBy.value === 'state' ? `mark-all-state-${key}` : `mark-all-host-${key}`
}

async function refresh(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    remote.value = await platform.sessions.listRemoteSessions()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'relay_unauthorized') { emit('tokenInvalid'); return }
    error.value = msg
    remote.value = []
  } finally {
    loading.value = false
  }
}

async function onMarkSeen(p: { ids: string[] } | { all: true }) {
  try {
    await platform.sessions.markSessionsSeen?.(p)
  } catch (e) {
    if (e instanceof Error && e.message === 'relay_unauthorized') {
      emit('tokenInvalid'); return
    }
    console.warn('mark-seen failed', e)
    return
  }
  await refresh()
}

function toggleGroupBy() {
  void groupByState.setGroupBy(groupBy.value === 'host' ? 'state' : 'host')
}

onMounted(async () => {
  try { home.value = await getUserHomeDir() } catch { /* leave empty */ }
  await refresh()
})
</script>

<template>
  <div class="list">
    <header class="bar">
      <span class="title">{{ t('mobile.sessionsTitle') }}<span v-if="remote.length" class="count"> · {{ remote.length }}</span></span>
      <button
        class="group-toggle"
        data-testid="group-toggle"
        :title="t('tasks.settings.groupBy')"
        @click="toggleGroupBy"
      >{{ groupBy === 'state' ? t('tasks.settings.groupByState') : t('tasks.settings.groupByHost') }}</button>
      <button data-testid="refresh" class="icon" :disabled="loading" @click="refresh" :aria-label="t('common.refresh')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36" /><path d="M21 3v6h-6" /></svg>
      </button>
      <button data-testid="gear" class="icon" @click="emit('openSettings')" :aria-label="t('common.settings')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" /></svg>
      </button>
    </header>

    <div class="body">
      <p v-if="error" data-testid="relay-disconnected" class="empty disconnected">
        {{ t('mobile.relayDisconnected') }} · {{ error }}
      </p>

      <section
        v-for="key in groupKeys"
        :key="key"
        class="group"
        :data-testid="groupTestId(key)"
      >
        <header
          class="grouphdr"
          data-testid="group-header"
          role="button"
          tabindex="0"
          :aria-expanded="!isGroupCollapsed(key)"
          @click="toggleGroupCollapsed(key)"
          @keydown.enter.prevent="toggleGroupCollapsed(key)"
          @keydown.space.prevent="toggleGroupCollapsed(key)"
        >
          <span class="caret">{{ isGroupCollapsed(key) ? '▶' : '▼' }}</span>
          <span class="gname">{{ groupHeader(key) }}</span>
          <span class="counts">
            <TaskStateIcon :state="primaryState(key)" :size="10" />
            <span class="count">{{ byGroup(key).length }}</span>
          </span>
          <span v-if="unreadCount(key) > 0" class="unread-badge">{{ t('tasks.unreadBadge', { count: unreadCount(key) }) }}</span>
          <button
            v-if="unreadCount(key) > 0"
            class="group-mark-all"
            :data-testid="groupMarkAllTestId(key)"
            :title="t('tasks.markAllRead')"
            @click.stop="onMarkSeen({ ids: unreadIdsForGroup(key) })"
          >✓</button>
        </header>
        <template v-if="!isGroupCollapsed(key)">
          <MobileSessionCard
            v-for="s in byGroup(key).filter((x) => !inFold(x))"
            :key="s.session_id"
            :session="s"
            :home="home"
            data-testid="task-card"
            @open="emit('open', s)"
            @markSeen="onMarkSeen"
          />
        </template>
      </section>

      <section v-if="sessions.completedSeen.value.length > 0" class="completed-fold">
        <button
          class="fold-toggle"
          data-testid="completed-fold-toggle"
          @click="foldOpen = !foldOpen"
        >{{ foldOpen ? '▼' : '▶' }} {{ t('tasks.completedFold') }} · {{ sessions.completedSeen.value.length }}</button>
        <template v-if="foldOpen">
          <div
            v-for="s in sessions.completedSeen.value"
            :key="s.session_id"
            class="fold-row"
            :data-testid="`completed-fold-row-${s.session_id}`"
            @click="emit('open', s)"
          >
            <TaskStateIcon :state="s.task_state ?? 'idle'" :size="12" />
            <span class="cmd">{{ s.current_command || s.title }}</span>
            <span class="meta">{{ s.host }}·{{ s.user }}</span>
          </div>
        </template>
      </section>

      <p v-if="!loading && !error && groupKeys.length === 0 && sessions.completedSeen.value.length === 0" class="empty">
        {{ t('mobile.noRemoteSessions') }}
      </p>
    </div>

    <footer v-if="sessions.totalUnread.value > 0" class="footer">
      <button
        class="footer-mark-all"
        data-testid="footer-mark-all"
        @click="onMarkSeen({ all: true })"
      >{{ t('tasks.markAllRead') }}</button>
    </footer>
  </div>
</template>

<style scoped>
.list { min-height: 100vh; box-sizing: border-box; padding: env(safe-area-inset-top) 0 0; display: flex; flex-direction: column; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: var(--font-sans); }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.bar .title { flex: 1; font-weight: 600; }
.bar .count { color: #8d93a3; font-weight: 400; margin-left: 4px; font-family: var(--font-mono); font-size: 0.85em; }
.group-toggle { background: none; border: 1px solid rgba(255,255,255,0.12); color: inherit; border-radius: 4px; font-size: 11px; padding: 4px 10px; cursor: pointer; }
.group-toggle:hover { background: rgba(255,255,255,0.05); }
.icon { display: inline-flex; align-items: center; justify-content: center; background: none; border: none; color: #8d93a3; padding: 4px; min-width: 44px; min-height: 44px; }
.body { flex: 1; overflow: auto; padding: 12px; }
.group { margin-bottom: 14px; }
.grouphdr { display: flex; align-items: center; gap: 6px; padding: 4px 2px 8px; font-size: 0.78rem; color: #c6cad5; font-family: var(--font-mono); cursor: pointer; user-select: none; -webkit-user-select: none; min-height: 32px; }
.grouphdr:focus { outline: 1px solid rgba(255,255,255,0.18); outline-offset: 2px; border-radius: 3px; }
.grouphdr .caret { font-size: 9px; color: #8d93a3; }
.grouphdr .gname { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.grouphdr .counts { margin-left: auto; display: inline-flex; align-items: center; gap: 2px; flex-shrink: 0; }
.grouphdr .count { font-size: 0.72rem; color: #8d93a3; }
.unread-badge { font-size: 10px; opacity: 0.9; background: rgba(255,255,255,0.06); border-radius: 3px; padding: 1px 4px; white-space: nowrap; flex-shrink: 0; }
.group-mark-all { background: none; border: none; color: inherit; cursor: pointer; padding: 0 6px; font-size: 14px; min-width: 32px; min-height: 32px; }
.completed-fold { border-top: 1px solid rgba(255,255,255,0.06); margin-top: 6px; padding-top: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 8px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.75; font-family: var(--font-mono); font-size: 0.78rem; }
.fold-row { display: flex; align-items: center; gap: 8px; padding: 6px 10px; opacity: 0.7; font-family: var(--font-mono); font-size: 0.78rem; }
.fold-row .cmd { flex: 1 1 auto; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.fold-row .meta { color: #8d93a3; }
.empty { color: #8d93a3; font-size: 0.85rem; text-align: center; padding: 40px 12px; line-height: 1.6; }
.disconnected { color: #f87171; }
.footer { padding: 8px 12px max(8px, env(safe-area-inset-bottom)); border-top: 1px solid #1e2638; background: #0b1020; }
.footer-mark-all { width: 100%; min-height: 44px; background: none; border: 1px solid rgba(255,255,255,0.12); color: inherit; border-radius: 6px; padding: 8px 12px; cursor: pointer; }
.footer-mark-all:hover { background: rgba(255,255,255,0.05); }
</style>
