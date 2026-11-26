<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NButton, NCheckbox, NCheckboxGroup, NDatePicker, NForm, NFormItem, NInput, NInputNumber, NSwitch, useMessage } from 'naive-ui'
import { Save } from 'lucide-vue-next'
import { ApiError } from '@shared/api/client'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'
import type { AdminConfig } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'

const cfg = ref<AdminConfig | null>(null)
const rateInput = ref<number>(0)
const connInput = ref<number>(0)
const debugInput = ref(false)
const debugScopesInput = ref<string[]>([])
const debugUntilInput = ref<number | null>(null)
const originsInput = ref<string>('')
const directSignalInput = ref(false)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const message = useMessage()
const { t } = useI18n()

const debugScopeOptions = [
  ['*', 'all'],
  ['state-sync', 'stateSync'],
  ['connection', 'connection'],
  ['prefs-sync', 'prefsSync'],
  ['terminal-io', 'terminalIO'],
  ['notifications', 'notifications'],
  ['auth', 'auth'],
  ['peer', 'peer'],
  ['payload', 'payload'],
] as const

// Newline-separated list ⇄ string[]; blank lines are dropped so save doesn't
// send stray "" patterns (which nhooyr's filepath.Match would treat as literal).
function parseOrigins(raw: string): string[] {
  return raw.split('\n').map((s) => s.trim()).filter(Boolean)
}

function effectiveLabel(stored: number, fallback: number): string {
  if (stored < 0) return t('admin.config.effectiveDisabled')
  if (stored === 0) return t('admin.config.effective', { value: fallback })
  return t('admin.config.effective', { value: stored })
}

const rateEffective = computed(() =>
  cfg.value ? effectiveLabel(rateInput.value, cfg.value.default_rate_limit_per_minute) : '',
)
const connEffective = computed(() =>
  cfg.value ? effectiveLabel(connInput.value, cfg.value.default_max_connections_per_key) : '',
)

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const c = await getAdminConfig()
    cfg.value = c
    rateInput.value = c.rate_limit_per_minute
    connInput.value = c.max_connections_per_key
    debugInput.value = c.debug
    debugScopesInput.value = [...(c.debug_scopes ?? [])]
    debugUntilInput.value = c.debug_until ? Date.parse(c.debug_until) : null
    originsInput.value = (c.allowed_origins ?? []).join('\n')
    directSignalInput.value = c.direct_signal_enabled
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('admin.config.loadFailed')
  } finally {
    loading.value = false
  }
}

async function onSave() {
  if (!cfg.value || saving.value) return
  errorMsg.value = ''
  saving.value = true
  try {
    const updated = await setAdminConfig({
      rate_limit_per_minute: Math.round(rateInput.value),
      max_connections_per_key: Math.round(connInput.value),
      debug: debugInput.value,
      debug_scopes: debugScopesInput.value,
      debug_until: debugUntilInput.value ? new Date(debugUntilInput.value).toISOString() : '',
      allowed_origins: parseOrigins(originsInput.value),
      direct_signal_enabled: directSignalInput.value,
    })
    cfg.value = updated
    rateInput.value = updated.rate_limit_per_minute
    connInput.value = updated.max_connections_per_key
    debugInput.value = updated.debug
    debugScopesInput.value = [...(updated.debug_scopes ?? [])]
    debugUntilInput.value = updated.debug_until ? Date.parse(updated.debug_until) : null
    originsInput.value = (updated.allowed_origins ?? []).join('\n')
    directSignalInput.value = updated.direct_signal_enabled
    message.success(t('setup.saved'))
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('admin.config.saveFailed')
  } finally {
    saving.value = false
  }
}

const rateInputProps = { 'data-testid': 'cfg-rate' } as Record<string, unknown>
const connInputProps = { 'data-testid': 'cfg-conn' } as Record<string, unknown>

onMounted(load)
</script>

<template>
  <section class="admin-page config-page">
    <header class="admin-page-header">
      <div>
        <h1>{{ t('admin.config.runtimeLimits') }}</h1>
        <p>{{ t('admin.config.hint') }}</p>
      </div>
    </header>

    <n-form v-if="cfg" class="admin-surface config-form" label-placement="top" require-mark-placement="right-hanging">
      <div class="config-section limits-section">
        <div class="limits-grid">
          <n-form-item :label="t('admin.config.rateLimit')" :show-feedback="false">
            <div class="inline-field">
              <n-input-number
                v-model:value="rateInput"
                :show-button="false"
                :input-props="rateInputProps"
              />
              <span class="muted">{{ rateEffective }}</span>
            </div>
          </n-form-item>
          <n-form-item :label="t('admin.config.maxConnections')" :show-feedback="false">
            <div class="inline-field">
              <n-input-number
                v-model:value="connInput"
                :show-button="false"
                :input-props="connInputProps"
              />
              <span class="muted">{{ connEffective }}</span>
            </div>
          </n-form-item>
        </div>
      </div>

      <div class="config-section direct-section">
        <div class="setting-row">
          <div>
            <h2>{{ t('admin.config.directSignal') }}</h2>
            <p class="muted">{{ t('admin.config.directSignalHint') }}</p>
          </div>
          <n-switch v-model:value="directSignalInput" data-testid="cfg-direct-signal" />
        </div>
      </div>

      <div class="config-section debug-section">
        <div class="debug-heading">
          <div>
            <h2>{{ t('admin.config.debug') }}</h2>
            <p class="muted">{{ t('admin.config.debugHint') }}</p>
          </div>
          <n-switch v-model:value="debugInput" data-testid="cfg-debug" />
        </div>

        <div class="debug-controls">
          <n-form-item :label="t('admin.config.debugScopes')" :show-feedback="false">
            <div class="stack-field">
              <n-checkbox-group v-model:value="debugScopesInput" :disabled="!debugInput" class="scope-grid">
                <n-checkbox
                  v-for="([value, label]) in debugScopeOptions"
                  :key="value"
                  :value="value"
                  :data-testid="`cfg-debug-scope-${label}`"
                >
                  {{ t(`admin.config.debugScope.${label}`) }}
                </n-checkbox>
              </n-checkbox-group>
              <p class="warn">{{ t('admin.config.debugPayloadWarn') }}</p>
            </div>
          </n-form-item>
          <n-form-item :label="t('admin.config.debugUntil')" :show-feedback="false">
            <div class="stack-field">
              <n-date-picker
                v-model:value="debugUntilInput"
                type="datetime"
                clearable
                :disabled="!debugInput"
                data-testid="cfg-debug-until"
              />
              <span class="muted">{{ t('admin.config.debugUntilHint') }}</span>
            </div>
          </n-form-item>
        </div>
      </div>

      <div class="config-section origins-section">
        <n-form-item :label="t('admin.config.allowedOrigins')" :show-feedback="false">
          <div class="stack-field">
            <n-input
              v-model:value="originsInput"
              type="textarea"
              :placeholder="t('admin.config.allowedOriginsPlaceholder')"
              :autosize="{ minRows: 2, maxRows: 8 }"
              :input-props="{ 'data-testid': 'cfg-origins' } as unknown as Record<string, unknown>"
            />
            <span class="muted">{{ t('admin.config.allowedOriginsHint') }}</span>
          </div>
        </n-form-item>
      </div>
      <div class="form-footer">
        <n-button
          type="primary"
          :loading="saving"
          :disabled="saving"
          data-testid="cfg-save"
          @click="onSave"
        >
          <template #icon><Save :size="15" /></template>
          {{ t('common.save') }}
        </n-button>
        <span class="muted version">{{ t('common.version') }}: <code>{{ cfg.version }}</code></span>
      </div>
    </n-form>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </section>
</template>

<style scoped>
.config-form { overflow: hidden; }
.config-section { padding: 18px; }
.config-section + .config-section { border-top: 1px solid var(--border); }
.limits-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 24px; }
.config-section :deep(.n-form-item) { margin: 0; }
.inline-field { display: flex; align-items: center; gap: 12px; width: 100%; }
.inline-field :deep(.n-input-number) { width: min(180px, 52%); flex: 0 0 auto; }
.setting-row {
  min-height: 42px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
}
.setting-row h2 { margin: 0; color: var(--fg); font-size: 14px; line-height: 1.4; font-weight: 600; letter-spacing: 0; }
.setting-row p { margin: 4px 0 0; line-height: 1.5; }
.setting-row :deep(.n-switch) { flex: 0 0 auto; margin-top: 2px; }
.debug-heading {
  min-height: 42px;
  margin-bottom: 16px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
}
.debug-heading h2 { margin: 0; color: var(--fg); font-size: 14px; line-height: 1.4; font-weight: 600; letter-spacing: 0; }
.debug-heading p { margin: 4px 0 0; line-height: 1.5; }
.debug-heading :deep(.n-switch) { flex: 0 0 auto; margin-top: 2px; }
.debug-controls { display: grid; grid-template-columns: minmax(0, 1.7fr) minmax(250px, 1fr); gap: 28px; }
.stack-field { display: flex; flex-direction: column; gap: 6px; width: 100%; }
.stack-field :deep(.n-date-picker) { width: 100%; }
.scope-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 9px 20px; width: 100%; }
.muted { color: var(--fg-dim); font-size: 0.875rem; }
.warn { margin: 8px 0 0; color: var(--warn, #d89614); font-size: 0.8125rem; line-height: 1.5; }
.form-footer {
  min-height: 58px;
  padding: 12px 18px;
  border-top: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 14px;
}
.version code { color: var(--fg); }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
@media (max-width: 760px) {
  .limits-grid,
  .debug-controls { grid-template-columns: 1fr; }
  .scope-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 480px) {
  .inline-field { align-items: flex-start; flex-direction: column; }
  .inline-field :deep(.n-input-number) { width: 100%; }
  .scope-grid { grid-template-columns: 1fr; }
}
</style>
