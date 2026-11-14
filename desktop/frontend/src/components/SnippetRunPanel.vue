<script setup lang="ts">
import { computed, inject, onBeforeUnmount, onMounted, ref, type ComputedRef } from 'vue'
import { Copy, RotateCcw, Search, ServerCog, Square, SquareTerminal, X } from 'lucide-vue-next'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { fallbackCopyText } from '../lib/terminalCopy'
import { allHostTags, hostHasAllTags } from '../lib/hostTags'
import type { SnippetSessionTarget, SnippetSessionTargetRegistry } from '../lib/snippetSessionTargets'
import type { RemoteSession } from '../platform/types'
import {
  listSSHHosts,
  runSnippetOnHosts,
  cancelSnippetRun,
  type SSHHost,
  type SnippetHostResult,
  type SnippetRunProgress,
} from '../lib/api'

const props = defineProps<{ snippetLabel: string; snippetText: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useI18n()
const platform = usePlatform()
const sessionCatalog = inject<ComputedRef<RemoteSession[]> | null>('atterm:snippetSessions', null)
const sessionTargets = inject<SnippetSessionTargetRegistry | null>('atterm:snippetSessionTargets', null)

type TargetKind = 'sessions' | 'hosts'
type SessionSendState = 'pending' | 'sent' | 'error'
interface SessionSendResult {
  session_id: string
  label: string
  detail: string
  state: SessionSendState
  error?: string
}

const hosts = ref<SSHHost[]>([])
const selected = ref<Set<string>>(new Set())
const selectedSessions = ref<Set<string>>(new Set())
const targetKind = ref<TargetKind>('sessions')
const resultTarget = ref<TargetKind | null>(null)
const hostQuery = ref('')
const sessionQuery = ref('')
const selectedTags = ref<string[]>([])
const runId = ref<string | null>(null)
const rows = ref<SnippetHostResult[]>([])
const sessionRows = ref<SessionSendResult[]>([])
const starting = ref(false)
const cancelling = ref(false)
const errorMsg = ref('')

function hostLabel(h: SSHHost): string {
  return h.alias?.trim() || h.host.trim()
}

function hostAddress(h: SSHHost): string {
  const port = h.port && h.port !== '22' ? `:${h.port}` : ''
  return `${h.user}@${h.host}${port}`
}

const availableTags = computed(() => allHostTags(hosts.value))
const filteredHosts = computed(() => {
  const q = hostQuery.value.trim().toLocaleLowerCase()
  return hosts.value.filter((host) => {
    if (!hostHasAllTags(host.tags, selectedTags.value)) return false
    if (!q) return true
    return [host.alias, host.host, host.user, ...(host.tags ?? []), ...(host.group_path ?? [])]
      .filter(Boolean)
      .join(' ')
      .toLocaleLowerCase()
      .includes(q)
  })
})

function sessionLabel(session: RemoteSession): string {
  const title = session.title?.trim()
  if (title) return title
  const command = session.current_command?.trim()
  if (command) return command
  const cwd = session.cwd?.replace(/\/+$/, '')
  if (cwd) return cwd.split('/').pop() || cwd
  return session.session_id.slice(0, 8)
}

function sessionDetail(session: RemoteSession): string {
  const location = [session.user, session.host].filter(Boolean).join('@')
  const context = session.current_command?.trim() || session.cwd?.trim() || session.session_id
  return [location, context].filter(Boolean).join(' · ')
}

function sessionUnavailableReason(sessionID: string): string {
  return sessionTargets?.has(sessionID)
    ? t('snippets.sessionNotWritable')
    : t('snippets.sessionNotOpen')
}

const sessionItems = computed(() => (sessionCatalog?.value ?? []).map((session) => {
  const target = sessionTargets?.get(session.session_id)
  return {
    session,
    writable: Boolean(target?.canSend()),
    unavailableReason: target?.canSend() ? '' : sessionUnavailableReason(session.session_id),
  }
}))

const filteredSessionItems = computed(() => {
  const q = sessionQuery.value.trim().toLocaleLowerCase()
  if (!q) return sessionItems.value
  return sessionItems.value.filter(({ session }) =>
    [session.title, session.cwd, session.host, session.user, session.current_command, session.session_id]
      .filter(Boolean)
      .join(' ')
      .toLocaleLowerCase()
      .includes(q),
  )
})

const groupedSessions = computed(() => {
  const groups = new Map<string, { key: string; label: string; items: typeof sessionItems.value }>()
  for (const item of filteredSessionItems.value) {
    const key = item.session.host_id || item.session.host || ''
    let group = groups.get(key)
    if (!group) {
      group = {
        key,
        label: item.session.host?.trim() || t('snippets.unknownHost'),
        items: [],
      }
      groups.set(key, group)
    }
    group.items.push(item)
  }
  return [...groups.values()].sort((a, b) => a.label.localeCompare(b.label))
})

const groupedHosts = computed(() => {
  const groups = new Map<string, { key: string; label: string; hosts: SSHHost[] }>()
  for (const host of filteredHosts.value) {
    const path = host.group_path?.filter(Boolean) ?? []
    const key = JSON.stringify(path)
    let group = groups.get(key)
    if (!group) {
      group = {
        key,
        label: path.length ? path.join(' / ') : t('snippets.ungrouped'),
        hosts: [],
      }
      groups.set(key, group)
    }
    group.hosts.push(host)
  }
  return [...groups.values()].sort((a, b) => {
    if (a.key === '[]') return 1
    if (b.key === '[]') return -1
    return a.label.localeCompare(b.label)
  })
})

const visibleSelectedCount = computed(() =>
  filteredHosts.value.reduce((count, host) => count + Number(selected.value.has(host.id)), 0),
)
const allVisibleSelected = computed(() =>
  filteredHosts.value.length > 0 && visibleSelectedCount.value === filteredHosts.value.length,
)
const writableVisibleSessions = computed(() => filteredSessionItems.value.filter((item) => item.writable))
const visibleSelectedSessionCount = computed(() =>
  writableVisibleSessions.value.reduce(
    (count, item) => count + Number(selectedSessions.value.has(item.session.session_id)),
    0,
  ),
)
const allVisibleSessionsSelected = computed(() =>
  writableVisibleSessions.value.length > 0 &&
  visibleSelectedSessionCount.value === writableVisibleSessions.value.length,
)

function toggleHost(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function toggleSession(id: string) {
  if (!sessionTargets?.get(id)?.canSend()) return
  const next = new Set(selectedSessions.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedSessions.value = next
}

function toggleTag(tag: string) {
  selectedTags.value = selectedTags.value.includes(tag)
    ? selectedTags.value.filter((item) => item !== tag)
    : [...selectedTags.value, tag]
}

function toggleVisible() {
  const next = new Set(selected.value)
  if (allVisibleSelected.value) {
    for (const host of filteredHosts.value) next.delete(host.id)
  } else {
    for (const host of filteredHosts.value) next.add(host.id)
  }
  selected.value = next
}

function toggleVisibleSessions() {
  const next = new Set(selectedSessions.value)
  if (allVisibleSessionsSelected.value) {
    for (const item of writableVisibleSessions.value) next.delete(item.session.session_id)
  } else {
    for (const item of writableVisibleSessions.value) next.add(item.session.session_id)
  }
  selectedSessions.value = next
}

const selectedCount = computed(() =>
  targetKind.value === 'sessions' ? selectedSessions.value.size : selected.value.size,
)
const canStart = computed(() => selectedCount.value > 0 && !starting.value)
const isRunActive = computed(() => rows.value.some((row) => row.state === 'pending' || row.state === 'running'))
const failedHostIDs = computed(() => rows.value
  .filter((row) => row.state === 'failed' || row.state === 'error')
  .map((row) => row.host_id))
const completedOKCount = computed(() => rows.value.filter((row) => row.state === 'ok').length)
const failedSessionIDs = computed(() => sessionRows.value
  .filter((row) => row.state === 'error')
  .map((row) => row.session_id))
const sentSessionCount = computed(() => sessionRows.value.filter((row) => row.state === 'sent').length)

let pendingEvents: SnippetRunProgress[] = []

async function startHostIDs(hostIds: string[]) {
  if (hostIds.length === 0 || starting.value) return
  errorMsg.value = ''
  starting.value = true
  runId.value = null
  rows.value = hostIds.map((id) => {
    const host = hosts.value.find((item) => item.id === id)
    return {
      host_id: id,
      host_label: host ? hostLabel(host) : id,
      state: 'pending',
      exit_code: 0,
      output: '',
      truncated: false,
    }
  })
  // Host workers can report progress before the Wails call returns its run
  // id. Buffer that narrow window, then replay only events for this run.
  pendingEvents = []
  try {
    const id = await runSnippetOnHosts(props.snippetLabel, props.snippetText, hostIds)
    runId.value = id
    resultTarget.value = 'hosts'
    const buffered = pendingEvents
    pendingEvents = []
    for (const progress of buffered) {
      if (progress.run_id === id) applyProgress(progress)
    }
  } catch (error) {
    errorMsg.value = error instanceof Error ? error.message : String(error)
    rows.value = []
    resultTarget.value = null
    pendingEvents = []
  } finally {
    starting.value = false
  }
}

function updateSessionRow(sessionID: string, update: Partial<SessionSendResult>) {
  const index = sessionRows.value.findIndex((row) => row.session_id === sessionID)
  if (index >= 0) sessionRows.value[index] = { ...sessionRows.value[index], ...update }
}

async function startSessionIDs(sessionIDs: string[]) {
  if (sessionIDs.length === 0 || starting.value) return
  errorMsg.value = ''
  starting.value = true
  resultTarget.value = 'sessions'
  runId.value = null
  sessionRows.value = sessionIDs.map((id) => {
    const session = sessionCatalog?.value.find((item) => item.session_id === id)
    return {
      session_id: id,
      label: session ? sessionLabel(session) : id,
      detail: session ? sessionDetail(session) : '',
      state: 'pending',
    }
  })

  const ready = new Map<string, SnippetSessionTarget>()
  for (const id of sessionIDs) {
    const target = sessionTargets?.get(id)
    if (!target || !target.canSend()) {
      updateSessionRow(id, { state: 'error', error: t('snippets.sessionNotWritable') })
      continue
    }
    try {
      target.send(props.snippetText)
      ready.set(id, target)
    } catch (error) {
      updateSessionRow(id, {
        state: 'error',
        error: error instanceof Error ? error.message : String(error),
      })
    }
  }

  if (ready.size > 0) {
    await new Promise<void>((resolve) => window.setTimeout(resolve, 16))
  }
  for (const [id, originalTarget] of ready) {
    const target = sessionTargets?.get(id)
    if (target !== originalTarget || !target.canSend()) {
      updateSessionRow(id, { state: 'error', error: t('snippets.sessionNotWritable') })
      continue
    }
    try {
      target.send('\r')
      updateSessionRow(id, { state: 'sent', error: undefined })
    } catch (error) {
      updateSessionRow(id, {
        state: 'error',
        error: error instanceof Error ? error.message : String(error),
      })
    }
  }
  starting.value = false
}

async function startRun() {
  if (!canStart.value) return
  if (targetKind.value === 'sessions') {
    const sessionIDs = (sessionCatalog?.value ?? [])
      .filter((session) => selectedSessions.value.has(session.session_id))
      .map((session) => session.session_id)
    await startSessionIDs(sessionIDs)
    return
  }
  const hostIds = hosts.value.filter((host) => selected.value.has(host.id)).map((host) => host.id)
  await startHostIDs(hostIds)
}

async function retryFailed() {
  if (isRunActive.value || failedHostIDs.value.length === 0) return
  await startHostIDs([...failedHostIDs.value])
}

async function retryFailedSessions() {
  if (starting.value || failedSessionIDs.value.length === 0) return
  await startSessionIDs([...failedSessionIDs.value])
}

async function cancelRun() {
  if (!runId.value || cancelling.value) return
  cancelling.value = true
  errorMsg.value = ''
  try {
    await cancelSnippetRun(runId.value)
  } catch (error) {
    errorMsg.value = error instanceof Error ? error.message : String(error)
  } finally {
    cancelling.value = false
  }
}

function stateLabel(row: SnippetHostResult): string {
  switch (row.state) {
    case 'pending':
      return t('snippets.pending')
    case 'running':
      return t('snippets.running')
    case 'ok':
      return t('snippets.ok')
    case 'failed':
      return t('snippets.failed')
    case 'error':
      return `${t('snippets.error')} · ${row.error ?? ''}`
  }
}

function sessionStateLabel(row: SessionSendResult): string {
  if (row.state === 'pending') return t('snippets.sending')
  if (row.state === 'sent') return t('snippets.sent')
  return `${t('snippets.sendFailed')} · ${row.error ?? ''}`
}

function rowCopyText(row: SnippetHostResult): string {
  if (row.state === 'error') {
    const message = row.error ?? ''
    return row.output ? `${message}\n${row.output}` : message
  }
  return row.output
}

function buildCopyAllText(): string {
  return rows.value.map((row) => `=== ${row.host_label} ===\n${rowCopyText(row)}`).join('\n\n')
}

async function copyAll() {
  const text = buildCopyAllText()
  const clipboard = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  if (clipboard?.writeText) {
    await clipboard.writeText(text)
    return
  }
  fallbackCopyText(text)
}

function applyProgress(progress: SnippetRunProgress) {
  const index = rows.value.findIndex((row) => row.host_id === progress.result.host_id)
  if (index >= 0) rows.value[index] = progress.result
}

function onProgress(data: unknown) {
  const progress = data as SnippetRunProgress
  if (!runId.value) {
    pendingEvents.push(progress)
    return
  }
  if (progress.run_id !== runId.value) return
  applyProgress(progress)
}

let progressOff: (() => void) | null = null
onMounted(async () => {
  progressOff = platform.events.on('snippet:run:progress', onProgress)
  try {
    hosts.value = await listSSHHosts()
  } catch (error) {
    errorMsg.value = error instanceof Error ? error.message : String(error)
  }
})
onBeforeUnmount(() => {
  progressOff?.()
  progressOff = null
})

defineExpose({ buildCopyAllText })
</script>

<template>
  <div class="snippet-run-overlay" @click.self="emit('close')">
    <section class="snippet-run-modal" role="dialog" aria-modal="true" :aria-label="t('snippets.drawerTitle')">
      <header class="modal-header">
        <div class="modal-heading">
          <span class="modal-kicker"><ServerCog :size="15" />{{ t('snippets.drawerTitle') }}</span>
          <h2>{{ snippetLabel }}</h2>
        </div>
        <button
          class="icon-btn"
          type="button"
          data-testid="snippet-run-close"
          :aria-label="t('common.close')"
          v-tooltip="t('common.close')"
          @click="emit('close')"
        ><X :size="18" /></button>
      </header>

      <section class="command-preview">
        <span>{{ t('snippets.commandPreview') }}</span>
        <code>{{ snippetText }}</code>
      </section>

      <nav v-if="!resultTarget" class="target-tabs" :aria-label="t('snippets.targetType')">
        <button
          type="button"
          data-testid="snippet-target-sessions"
          :class="{ active: targetKind === 'sessions' }"
          :aria-pressed="targetKind === 'sessions'"
          @click="targetKind = 'sessions'"
        ><SquareTerminal :size="15" />{{ t('snippets.targetSessions') }}</button>
        <button
          type="button"
          data-testid="snippet-target-hosts"
          :class="{ active: targetKind === 'hosts' }"
          :aria-pressed="targetKind === 'hosts'"
          @click="targetKind = 'hosts'"
        ><ServerCog :size="15" />{{ t('snippets.targetHosts') }}</button>
      </nav>

      <div v-if="!resultTarget" class="modal-body select-phase">
        <template v-if="targetKind === 'sessions'">
          <label class="search-box">
            <Search :size="16" />
            <input
              v-model="sessionQuery"
              data-testid="snippet-run-session-search"
              type="search"
              :placeholder="t('snippets.searchSessions')"
            />
          </label>
          <div class="selection-tools">
            <label>
              <input
                type="checkbox"
                data-testid="snippet-run-select-visible-sessions"
                :checked="allVisibleSessionsSelected"
                :disabled="writableVisibleSessions.length === 0"
                @change="toggleVisibleSessions"
              />
              <span>{{ t('snippets.selectVisible', { selected: visibleSelectedSessionCount, total: writableVisibleSessions.length }) }}</span>
            </label>
          </div>
          <div v-if="groupedSessions.length" class="host-groups session-groups">
            <section v-for="group in groupedSessions" :key="group.key" class="host-group">
              <h3>{{ group.label }} <span>{{ group.items.length }}</span></h3>
              <ul class="host-list session-list">
                <li v-for="item in group.items" :key="item.session.session_id">
                  <label class="host-option" :class="{ disabled: !item.writable }">
                    <input
                      type="checkbox"
                      :data-testid="`snippet-run-session-${item.session.session_id}`"
                      :checked="selectedSessions.has(item.session.session_id)"
                      :disabled="!item.writable"
                      @change="toggleSession(item.session.session_id)"
                    />
                    <span class="host-copy">
                      <strong>{{ sessionLabel(item.session) }}</strong>
                      <small>{{ sessionDetail(item.session) }}</small>
                    </span>
                    <span v-if="!item.writable" class="session-unavailable">{{ item.unavailableReason }}</span>
                  </label>
                </li>
              </ul>
            </section>
          </div>
          <p v-else class="empty">{{ t('snippets.noMatchingSessions') }}</p>
        </template>

        <template v-else>
          <label class="search-box">
            <Search :size="16" />
            <input
              v-model="hostQuery"
              data-testid="snippet-run-search"
              type="search"
              :placeholder="t('snippets.searchHosts')"
            />
          </label>
          <div v-if="availableTags.length" class="tag-filter" :aria-label="t('snippets.filterByTag')">
            <button
              v-for="tag in availableTags"
              :key="tag"
              type="button"
              class="tag-chip"
              :class="{ active: selectedTags.includes(tag) }"
              :aria-pressed="selectedTags.includes(tag)"
              :data-testid="`snippet-run-tag-${tag}`"
              @click="toggleTag(tag)"
            >{{ tag }}</button>
          </div>
          <div class="selection-tools">
            <label>
              <input
                type="checkbox"
                data-testid="snippet-run-select-visible"
                :checked="allVisibleSelected"
                :disabled="filteredHosts.length === 0"
                @change="toggleVisible"
              />
              <span>{{ t('snippets.selectVisible', { selected: visibleSelectedCount, total: filteredHosts.length }) }}</span>
            </label>
          </div>
          <p v-if="errorMsg" class="error" data-testid="snippet-run-error">{{ errorMsg }}</p>
          <div v-if="groupedHosts.length" class="host-groups">
            <section v-for="group in groupedHosts" :key="group.key" class="host-group">
              <h3>{{ group.label }} <span>{{ group.hosts.length }}</span></h3>
              <ul class="host-list">
                <li v-for="host in group.hosts" :key="host.id">
                  <label class="host-option">
                    <input
                      type="checkbox"
                      :data-testid="`snippet-run-host-${host.id}`"
                      :checked="selected.has(host.id)"
                      @change="toggleHost(host.id)"
                    />
                    <span class="host-copy">
                      <strong>{{ hostLabel(host) }}</strong>
                      <small>{{ hostAddress(host) }}</small>
                    </span>
                    <span v-if="host.tags?.length" class="host-tags">{{ host.tags.join(' · ') }}</span>
                  </label>
                </li>
              </ul>
            </section>
          </div>
          <p v-else-if="!errorMsg" class="empty">{{ t('snippets.noMatchingHosts') }}</p>
        </template>
      </div>

      <div v-else-if="resultTarget === 'hosts'" class="modal-body results-phase">
        <ul class="rows">
          <li
            v-for="row in rows"
            :key="row.host_id"
            class="row"
            :class="`state-${row.state}`"
            :data-testid="`snippet-run-row-${row.host_id}`"
            :data-state="row.state"
          >
            <div class="row-head">
              <span class="row-host">{{ row.host_label }}</span>
              <span
                class="row-state"
                :data-testid="`snippet-run-state-${row.host_id}`"
                :data-state="row.state"
              >{{ stateLabel(row) }}</span>
            </div>
            <span
              v-if="row.state === 'failed'"
              class="row-exitcode"
              :data-testid="`snippet-run-exitcode-${row.host_id}`"
            >{{ t('snippets.exitCode', { code: row.exit_code }) }}</span>
            <pre
              v-if="row.state === 'ok' || row.state === 'failed' || (row.state === 'error' && row.output)"
              class="row-output"
              :data-testid="`snippet-run-output-${row.host_id}`"
            >{{ row.output }}</pre>
            <p
              v-if="row.truncated"
              class="row-truncated"
              :data-testid="`snippet-run-truncated-${row.host_id}`"
            >{{ t('snippets.truncated') }}</p>
          </li>
        </ul>
        <p v-if="errorMsg" class="error" data-testid="snippet-run-error">{{ errorMsg }}</p>
      </div>

      <div v-else class="modal-body results-phase">
        <ul class="rows">
          <li
            v-for="row in sessionRows"
            :key="row.session_id"
            class="row"
            :class="`state-${row.state}`"
            :data-testid="`snippet-run-session-row-${row.session_id}`"
            :data-state="row.state"
          >
            <div class="row-head">
              <span class="row-host">{{ row.label }}</span>
              <span
                class="row-state"
                :data-testid="`snippet-run-session-state-${row.session_id}`"
                :data-state="row.state"
              >{{ sessionStateLabel(row) }}</span>
            </div>
            <small class="session-result-detail">{{ row.detail }}</small>
          </li>
        </ul>
      </div>

      <footer v-if="!resultTarget" class="modal-footer">
        <span>{{ targetKind === 'sessions'
          ? t('snippets.selectedSessions', { count: selectedSessions.size })
          : t('snippets.selectedCount', { count: selected.size }) }}</span>
        <button
          class="primary"
          data-testid="snippet-run-start"
          :disabled="!canStart"
          v-tooltip="selectedCount === 0
            ? (targetKind === 'sessions' ? t('snippets.noSessionsSelected') : t('snippets.noHostsSelected'))
            : ''"
          @click="startRun"
        >{{ targetKind === 'sessions'
          ? t('snippets.sendSelected', { count: selectedSessions.size })
          : t('snippets.runSelected', { count: selected.size }) }}</button>
      </footer>
      <footer v-else-if="resultTarget === 'hosts'" class="modal-footer result-footer">
        <span v-if="!isRunActive">{{ t('snippets.resultSummary', { ok: completedOKCount, failed: failedHostIDs.length }) }}</span>
        <span v-else>{{ t('snippets.runningCount', { count: rows.length }) }}</span>
        <div class="footer-actions">
          <button data-testid="snippet-run-copyall" @click="copyAll"><Copy :size="15" />{{ t('snippets.copyAll') }}</button>
          <button
            v-if="!isRunActive && failedHostIDs.length"
            data-testid="snippet-run-retry"
            :disabled="starting"
            @click="retryFailed"
          ><RotateCcw :size="15" />{{ t('snippets.retryFailed') }}</button>
          <button
            v-if="isRunActive"
            data-testid="snippet-run-cancel"
            :disabled="cancelling"
            @click="cancelRun"
          ><Square :size="14" />{{ t('snippets.cancel') }}</button>
        </div>
      </footer>
      <footer v-else class="modal-footer result-footer">
        <span>{{ t('snippets.sessionResultSummary', { sent: sentSessionCount, failed: failedSessionIDs.length }) }}</span>
        <div class="footer-actions">
          <button
            v-if="failedSessionIDs.length"
            data-testid="snippet-run-session-retry"
            :disabled="starting"
            @click="retryFailedSessions"
          ><RotateCcw :size="15" />{{ t('snippets.retryUnsent') }}</button>
        </div>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.snippet-run-overlay {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  background: rgba(0, 0, 0, 0.48);
}
.snippet-run-modal {
  width: min(960px, calc(100vw - 40px));
  height: min(760px, calc(100vh - 40px));
  min-height: 420px;
  display: flex;
  flex-direction: column;
  color: var(--fg);
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 20px 56px rgba(0, 0, 0, 0.42);
  overflow: hidden;
}
.modal-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 20px 14px;
  border-bottom: 1px solid var(--border);
}
.modal-heading { min-width: 0; }
.modal-kicker { display: flex; align-items: center; gap: 7px; color: var(--fg-dim); font-size: 0.75rem; }
.modal-heading h2 { margin: 7px 0 0; overflow: hidden; color: var(--fg); font-size: 1rem; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.icon-btn { width: 32px; height: 32px; flex: none; display: inline-flex; align-items: center; justify-content: center; padding: 0; color: var(--fg-dim); background: transparent; border: 1px solid transparent; border-radius: 6px; cursor: pointer; }
.icon-btn:hover { color: var(--fg); background: var(--bg); border-color: var(--border); }
.command-preview { display: flex; flex-direction: column; gap: 7px; padding: 12px 20px; border-bottom: 1px solid var(--border); }
.command-preview > span { color: var(--fg-dim); font-size: 0.72rem; }
.command-preview code { max-height: 96px; overflow: auto; padding: 9px 10px; color: var(--fg); background: var(--bg); border: 1px solid var(--border); border-radius: 6px; font: 0.76rem/1.5 var(--font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.target-tabs { display: flex; gap: 2px; padding: 8px 20px 0; border-bottom: 1px solid var(--border); }
.target-tabs button { min-height: 34px; display: inline-flex; align-items: center; gap: 7px; padding: 0 12px; color: var(--fg-dim); background: transparent; border: 0; border-bottom: 2px solid transparent; cursor: pointer; }
.target-tabs button:hover { color: var(--fg); }
.target-tabs button.active { color: var(--fg); border-bottom-color: var(--accent); }
.modal-body { min-height: 0; flex: 1; overflow-y: auto; padding: 14px 20px; }
.search-box { height: 36px; display: flex; align-items: center; gap: 8px; padding: 0 10px; color: var(--fg-dim); background: var(--bg); border: 1px solid var(--border); border-radius: 6px; }
.search-box:focus-within { border-color: var(--accent); }
.search-box input { min-width: 0; flex: 1; color: var(--fg); background: transparent; border: 0; outline: 0; font: 0.8rem var(--font-mono); }
.tag-filter { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.tag-chip { min-height: 26px; padding: 3px 9px; color: var(--fg-dim); background: transparent; border: 1px solid var(--border); border-radius: 5px; font-size: 0.72rem; cursor: pointer; }
.tag-chip.active { color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); border-color: var(--accent); }
.selection-tools { display: flex; align-items: center; min-height: 38px; margin-top: 10px; color: var(--fg-dim); border-bottom: 1px solid var(--border); font-size: 0.75rem; }
.selection-tools label { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.host-groups { display: flex; flex-direction: column; }
.host-group { padding: 12px 0 4px; }
.host-group + .host-group { border-top: 1px solid var(--border); }
.host-group h3 { display: flex; align-items: center; gap: 7px; margin: 0 0 5px; color: var(--fg-dim); font-size: 0.72rem; font-weight: 600; }
.host-group h3 span { font-weight: 400; }
.host-list { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 2px 10px; }
.host-option { min-height: 48px; display: flex; align-items: center; gap: 10px; padding: 6px 7px; border-radius: 5px; cursor: pointer; }
.host-option:hover { background: color-mix(in srgb, var(--accent) 8%, var(--bg)); }
.host-option.disabled { opacity: 0.58; cursor: default; }
.host-option.disabled:hover { background: transparent; }
.host-copy { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 3px; }
.host-copy strong { overflow: hidden; font-size: 0.8rem; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.host-copy small { overflow: hidden; color: var(--fg-dim); font: 0.7rem var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }
.host-tags { max-width: 38%; overflow: hidden; color: var(--fg-dim); font-size: 0.68rem; text-overflow: ellipsis; white-space: nowrap; }
.session-unavailable { flex: none; color: var(--fg-dim); font-size: 0.68rem; }
.empty { margin: 36px 0; color: var(--fg-dim); font-size: 0.8rem; text-align: center; }
.rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.row { padding: 9px 10px; background: var(--bg); border: 1px solid var(--border); border-radius: 6px; display: flex; flex-direction: column; gap: 5px; }
.row-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 10px; }
.row-host { min-width: 0; overflow: hidden; font: 600 0.8rem var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }
.row-state { max-width: 65%; font-size: 0.72rem; text-align: right; overflow-wrap: anywhere; }
.row.state-ok .row-state { color: var(--good, #3fb950); }
.row.state-sent .row-state { color: var(--good, #3fb950); }
.row.state-failed .row-state, .row.state-error .row-state { color: var(--bad); }
.row.state-running .row-state { color: var(--accent); }
.session-result-detail { overflow: hidden; color: var(--fg-dim); font: 0.7rem var(--font-mono); text-overflow: ellipsis; white-space: nowrap; }
.row-exitcode { color: var(--fg-dim); font: 0.7rem var(--font-mono); }
.row-output { max-height: 30vh; overflow-y: auto; margin: 0; padding: 7px 8px; color: var(--fg); background: color-mix(in srgb, var(--panel) 72%, transparent); border-radius: 4px; font: 0.73rem/1.45 var(--font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.row-truncated { margin: 0; color: var(--fg-dim); font-size: 0.7rem; font-style: italic; }
.error { margin: 8px 0; color: var(--bad); font-size: 0.75rem; overflow-wrap: anywhere; }
.modal-footer { min-height: 66px; flex: none; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 20px; color: var(--fg-dim); background: var(--panel); border-top: 1px solid var(--border); font-size: 0.75rem; }
.modal-footer button { min-height: 34px; display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 0 12px; color: var(--fg); background: var(--bg); border: 1px solid var(--border); border-radius: 6px; cursor: pointer; }
.modal-footer button:disabled { opacity: 0.45; cursor: default; }
.modal-footer .primary { color: #0d1117; background: var(--accent); border-color: var(--accent); font-weight: 650; }
.footer-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 7px; }
@media (max-width: 520px) {
  .snippet-run-overlay { padding: 8px; }
  .snippet-run-modal { width: calc(100vw - 16px); height: calc(100vh - 16px); min-height: 0; }
  .modal-header { padding: 14px 16px 12px; }
  .command-preview, .target-tabs, .modal-body { padding-left: 16px; padding-right: 16px; }
  .host-list { grid-template-columns: minmax(0, 1fr); }
  .modal-footer { align-items: stretch; flex-direction: column; padding: 10px 16px; }
  .modal-footer .primary { width: 100%; }
  .result-footer > span { text-align: center; }
  .footer-actions { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); }
}
</style>
