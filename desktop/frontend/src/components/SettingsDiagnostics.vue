<script lang="ts" setup>
import { ref, onMounted } from 'vue'
import { getDiagnostics, exportDiagnostics, type DiagnosticsPayload } from '../lib/api'
import { formatDiagnostics } from '../lib/diagnostics'
import { useI18n } from '../i18n/useI18n'

const { t } = useI18n()

const payload = ref<DiagnosticsPayload | null>(null)
const text = ref('')
const status = ref('')

async function refresh(): Promise<void> {
  status.value = ''
  payload.value = await getDiagnostics(navigator.userAgent)
  text.value = formatDiagnostics(payload.value)
}

async function copy(): Promise<void> {
  if (!text.value) return
  try {
    await navigator.clipboard.writeText(text.value)
    status.value = t('settings.diagnostics.copied')
    setTimeout(() => { status.value = '' }, 1500)
  } catch {
    status.value = t('settings.diagnostics.copyFailed')
  }
}

async function exportToFile(): Promise<void> {
  if (!text.value) return
  try {
    const path = await exportDiagnostics(text.value)
    if (path) {
      status.value = t('settings.diagnostics.exported', { path })
      setTimeout(() => { status.value = '' }, 3000)
    }
  } catch (e) {
    status.value = e instanceof Error ? e.message : String(e)
  }
}

onMounted(refresh)
</script>

<template>
  <div class="diag">
    <p class="hint">{{ t('settings.diagnostics.hint') }}</p>
    <pre class="preview" data-testid="diag-preview">{{ text }}</pre>
    <div class="row">
      <button type="button" data-testid="diag-copy" @click="copy">
        {{ t('settings.diagnostics.copy') }}
      </button>
      <button type="button" data-testid="diag-export" @click="exportToFile">
        {{ t('settings.diagnostics.export') }}
      </button>
      <button type="button" data-testid="diag-refresh" @click="refresh">
        {{ t('settings.diagnostics.refresh') }}
      </button>
      <span v-if="status" class="status">{{ status }}</span>
    </div>
  </div>
</template>

<style scoped>
.diag { display: flex; flex-direction: column; gap: 10px; }
.hint { font-size: 12px; color: var(--fg-dim); margin: 0; line-height: 1.5; }
.preview { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 10px; font-family: ui-monospace, Menlo, monospace; font-size: 11px; line-height: 1.4; max-height: 320px; overflow: auto; white-space: pre; color: var(--fg); margin: 0; }
.row { display: flex; gap: 8px; align-items: center; }
button { height: 30px; border: 1px solid var(--accent); border-radius: 7px; background: var(--accent); color: var(--bg); padding: 0 12px; font-size: 12px; font-weight: 700; cursor: pointer; }
.status { font-size: 12px; color: var(--fg-dim); }
</style>
