<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NCard, NForm, NFormItem, NInputNumber, NButton, NSpace, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'
import type { AdminConfig } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'

const cfg = ref<AdminConfig | null>(null)
const rateInput = ref<number>(0)
const connInput = ref<number>(0)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const message = useMessage()
const { t } = useI18n()

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
    })
    cfg.value = updated
    rateInput.value = updated.rate_limit_per_minute
    connInput.value = updated.max_connections_per_key
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
  <n-card :title="t('admin.config.runtimeLimits')" :bordered="false">
    <p class="hint">
      {{ t('admin.config.hint') }}
    </p>
    <n-form v-if="cfg" label-placement="top" require-mark-placement="right-hanging">
      <n-form-item :label="t('admin.config.rateLimit')" :show-feedback="false">
        <n-space :wrap="false" align="center">
          <n-input-number
            v-model:value="rateInput"
            :show-button="false"
            :input-props="rateInputProps"
          />
          <span class="muted">{{ rateEffective }}</span>
        </n-space>
      </n-form-item>
      <n-form-item :label="t('admin.config.maxConnections')" :show-feedback="false">
        <n-space :wrap="false" align="center">
          <n-input-number
            v-model:value="connInput"
            :show-button="false"
            :input-props="connInputProps"
          />
          <span class="muted">{{ connEffective }}</span>
        </n-space>
      </n-form-item>
      <n-space class="actions" align="center">
        <n-button
          type="primary"
          :loading="saving"
          :disabled="saving"
          data-testid="cfg-save"
          @click="onSave"
        >
          {{ t('common.save') }}
        </n-button>
        <span class="muted version">{{ t('common.version') }}: <code>{{ cfg.version }}</code></span>
      </n-space>
    </n-form>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </n-card>
</template>

<style scoped>
.hint { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.muted { color: var(--fg-dim); font-size: 0.875rem; }
.actions { margin-top: 1rem; }
.version code { color: var(--fg); }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
