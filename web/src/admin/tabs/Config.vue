<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NCard, NForm, NFormItem, NInputNumber, NButton, NSpace, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { getAdminConfig, setAdminConfig } from '@shared/api/admin'
import type { AdminConfig } from '@shared/api/types'

const cfg = ref<AdminConfig | null>(null)
const rateInput = ref<number>(0)
const connInput = ref<number>(0)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const message = useMessage()

function effectiveLabel(stored: number, fallback: number): string {
  if (stored < 0) return 'effective: disabled'
  if (stored === 0) return `effective: ${fallback}`
  return `effective: ${stored}`
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
    if (e instanceof ApiError) errorMsg.value = 'Failed to load config.'
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
    message.success('Saved.')
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Save failed.'
  } finally {
    saving.value = false
  }
}

const rateInputProps = { 'data-testid': 'cfg-rate' } as Record<string, unknown>
const connInputProps = { 'data-testid': 'cfg-conn' } as Record<string, unknown>

onMounted(load)
</script>

<template>
  <n-card title="Runtime limits" :bordered="false">
    <p class="hint">
      <strong>0</strong> means "use the built-in default"; <strong>negative</strong> disables
      the limit entirely. Changes apply immediately and persist to the admin config file.
    </p>
    <n-form v-if="cfg" label-placement="top" require-mark-placement="right-hanging">
      <n-form-item label="Rate limit (requests/min per IP+token)" :show-feedback="false">
        <n-space :wrap="false" align="center">
          <n-input-number
            v-model:value="rateInput"
            :show-button="false"
            :input-props="rateInputProps"
          />
          <span class="muted">{{ rateEffective }}</span>
        </n-space>
      </n-form-item>
      <n-form-item label="Max WS connections (per IP+token)" :show-feedback="false">
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
          Save
        </n-button>
        <span class="muted version">Version: <code>{{ cfg.version }}</code></span>
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
