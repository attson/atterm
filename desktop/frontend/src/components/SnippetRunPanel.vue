<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import { fallbackCopyText } from '../lib/terminalCopy'
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

const hosts = ref<SSHHost[]>([])
const selected = ref<Set<string>>(new Set())
const runId = ref<string | null>(null)
const rows = ref<SnippetHostResult[]>([])
const starting = ref(false)
const cancelling = ref(false)
const errorMsg = ref('')

// Same alias-or-user@host convention SshHostsPanel.vue uses (hostLabel there):
// kept local rather than shared because that file has no exported helper to
// import, and duplicating one line is cheaper than introducing a new shared
// module for it.
function hostLabel(h: SSHHost): string {
  return h.alias?.trim() || `${h.user}@${h.host}`
}

function toggleHost(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

const canStart = computed(() => selected.value.size > 0 && !starting.value)

// isRunActive drives whether Cancel is offered: true while any row is still
// "pending"/"running", false once every host has reached ok/failed/error.
// Cancelling a run with nothing left in flight would just be a confusing
// no-op button.
const isRunActive = computed(() => rows.value.some((r) => r.state === 'pending' || r.state === 'running'))

async function startRun() {
  if (!canStart.value) return
  errorMsg.value = ''
  starting.value = true
  const hostIds = hosts.value.filter((h) => selected.value.has(h.id)).map((h) => h.id)
  rows.value = hostIds.map((id) => {
    const h = hosts.value.find((x) => x.id === id)
    return {
      host_id: id,
      host_label: h ? hostLabel(h) : id,
      state: 'pending',
      exit_code: 0,
      output: '',
      truncated: false,
    }
  })
  // Go can spawn host goroutines and emit "running"/a terminal event before
  // this promise resolves with the run id — runId.value is still null at
  // that point, so onProgress buffers them here instead of dropping them.
  // Discard anything buffered by a previous, superseded start attempt.
  pendingEvents = []
  try {
    const id = await runSnippetOnHosts(props.snippetLabel, props.snippetText, hostIds)
    runId.value = id
    const buffered = pendingEvents
    pendingEvents = []
    for (const p of buffered) {
      if (p.run_id === id) applyProgress(p)
    }
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
    rows.value = []
    pendingEvents = []
  } finally {
    starting.value = false
  }
}

async function cancelRun() {
  if (!runId.value || cancelling.value) return
  cancelling.value = true
  errorMsg.value = ''
  try {
    await cancelSnippetRun(runId.value)
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
  } finally {
    cancelling.value = false
  }
}

// failed = ran, exited non-zero (ExitCode/Output meaningful; exit code is
// rendered once, by the separate .row-exitcode span in the template — not
// composed into this label too).
// error = never ran at all (message only, no exit code). Go's message
// (desktop/snippet_run.go) is rendered verbatim: it is already the most
// diagnosable form (host, fingerprint, remedy where relevant), and every
// other Go-side error in this panel already surfaces in English too, so
// there is nothing to localize here without losing information.
// Conflating these is exactly what desktop/snippet_run.go's separate states
// exist to prevent, so this stays a plain state-keyed switch rather than any
// shared "problem" fallthrough.
function stateLabel(r: SnippetHostResult): string {
  switch (r.state) {
    case 'pending':
      return t('snippets.pending')
    case 'running':
      return t('snippets.running')
    case 'ok':
      return t('snippets.ok')
    case 'failed':
      return t('snippets.failed')
    case 'error':
      return `${t('snippets.error')} · ${r.error ?? ''}`
  }
}

function rowCopyText(r: SnippetHostResult): string {
  if (r.state === 'error') {
    const msg = r.error ?? ''
    // Go preserves partial output before a connection drop even on a
    // never-finished run (snippet_run.go) — keep it in copy-all too.
    return r.output ? `${msg}\n${r.output}` : msg
  }
  return r.output
}
// Exposed for tests to pin the exact format independent of clipboard plumbing.
function buildCopyAllText(): string {
  return rows.value.map((r) => `=== ${r.host_label} ===\n${rowCopyText(r)}`).join('\n\n')
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

let pendingEvents: SnippetRunProgress[] = []

function applyProgress(p: SnippetRunProgress) {
  const idx = rows.value.findIndex((r) => r.host_id === p.result.host_id)
  if (idx >= 0) rows.value[idx] = p.result
}

// snippet:run:progress is pushed once a host enters "running" and again on
// its terminal state; there is no separate "list results" call (see
// SnippetRunProgress in lib/api/_bindings.ts). Go spawns host goroutines and
// can emit before the RunSnippetOnHosts promise resolves with a run id, so
// while runId.value is still null (startRun is awaiting it) events are
// buffered here instead of dropped — startRun replays them, filtered by the
// resolved run_id, once it has one. Once resolved, guard on run_id so a
// stale event from a superseded run (or, in tests, one addressed to a run
// this panel never started) cannot overwrite a live row.
function onProgress(data: unknown) {
  const p = data as SnippetRunProgress
  if (!runId.value) {
    pendingEvents.push(p)
    return
  }
  if (p.run_id !== runId.value) return
  applyProgress(p)
}

let progressOff: (() => void) | null = null
onMounted(async () => {
  progressOff = platform.events.on('snippet:run:progress', onProgress)
  try {
    hosts.value = await listSSHHosts()
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e)
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
    <div class="snippet-run-panel">
      <div v-if="!runId" class="select-phase">
        <p class="hint">{{ t('snippets.selectHosts') }}</p>
        <p v-if="errorMsg" class="error" data-testid="snippet-run-error">{{ errorMsg }}</p>
        <ul class="host-list">
          <li v-for="h in hosts" :key="h.id">
            <label>
              <input
                type="checkbox"
                :data-testid="`snippet-run-host-${h.id}`"
                :checked="selected.has(h.id)"
                @change="toggleHost(h.id)"
              />
              <span>{{ hostLabel(h) }}</span>
            </label>
          </li>
        </ul>
        <div class="footer">
          <button data-testid="snippet-run-close" @click="emit('close')">{{ t('common.close') }}</button>
          <button
            class="primary"
            data-testid="snippet-run-start"
            :disabled="!canStart"
            :title="selected.size === 0 ? t('snippets.noHostsSelected') : ''"
            @click="startRun"
          >{{ t('snippets.runOnHosts') }}</button>
        </div>
      </div>

      <div v-else class="results-phase">
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
        <div class="footer">
          <button data-testid="snippet-run-close" @click="emit('close')">{{ t('common.close') }}</button>
          <button data-testid="snippet-run-copyall" @click="copyAll">{{ t('snippets.copyAll') }}</button>
          <button
            v-if="isRunActive"
            data-testid="snippet-run-cancel"
            :disabled="cancelling"
            @click="cancelRun"
          >{{ t('snippets.cancel') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.snippet-run-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 60; }
.snippet-run-panel { background: #11182b; border: 1px solid #1e2638; border-radius: 11px; padding: 16px 18px; min-width: 360px; max-width: 560px; max-height: 80vh; overflow-y: auto; display: flex; flex-direction: column; gap: 12px; color: #e6e7ea; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; }
.host-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; max-height: 40vh; overflow-y: auto; }
.host-list label { display: flex; align-items: center; gap: 8px; font-size: 0.85rem; }
.rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
.row { border: 1px solid var(--border); border-radius: 6px; padding: 8px 10px; background: var(--bg); display: flex; flex-direction: column; gap: 4px; }
.row-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.row-host { font-weight: 600; font-family: var(--font-mono); font-size: 0.85rem; }
.row-state { font-size: 0.78rem; }
.row.state-ok .row-state { color: var(--good, #3fb950); }
.row.state-failed .row-state { color: var(--bad); }
.row.state-error .row-state { color: var(--bad); }
.row.state-running .row-state { color: var(--accent); }
.row-exitcode { font-size: 0.74rem; color: var(--fg-dim); font-family: var(--font-mono); }
.row-output { font-family: var(--font-mono); font-size: 0.76rem; white-space: pre-wrap; word-break: break-word; background: rgba(255,255,255,0.03); border-radius: 4px; padding: 6px 8px; margin: 0; max-height: 30vh; overflow-y: auto; }
.row-truncated { font-size: 0.72rem; color: var(--fg-dim); margin: 0; font-style: italic; }
.error { color: var(--bad); font-size: 0.75rem; margin: 0; }
.footer { display: flex; gap: 8px; justify-content: flex-end; }
.footer .primary { background: var(--accent); color: #0d1117; border-color: var(--accent); font-weight: 600; }
</style>
