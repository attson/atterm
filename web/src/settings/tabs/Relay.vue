<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  NCard,
  NInput,
  NSwitch,
  NButton,
  NAlert,
  NSpace,
  NForm,
  NFormItem,
} from 'naive-ui'
import {
  loadRelayConfig,
  saveRelayConfig,
  clearRelayConfig,
  validateRelayBase,
} from '@shared/api/relay-config'

const _cfg = loadRelayConfig()
const base = ref(_cfg?.base ?? '')
const token = ref(_cfg?.token ?? '')
const allowInsecure = ref(_cfg?.allowInsecure ?? false)
const error = ref<string | null>(null)
const ok = ref<string | null>(null)
const submitting = ref(false)

const baseError = computed(() => {
  if (!base.value) return null
  return validateRelayBase(base.value, allowInsecure.value)
})

async function onSave(): Promise<void> {
  error.value = null
  ok.value = null
  const v = validateRelayBase(base.value, allowInsecure.value)
  if (v) {
    error.value = v
    return
  }
  if (!token.value.trim()) {
    error.value = 'API token is required'
    return
  }
  submitting.value = true
  try {
    const url = base.value.replace(/\/$/, '') + '/api/me'
    const res = await fetch(url, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token.value.trim()}` },
      credentials: 'omit',
    })
    if (res.status === 401) {
      error.value = 'API token is invalid.'
      return
    }
    if (!res.ok) {
      error.value = `Relay returned HTTP ${res.status}.`
      return
    }
    saveRelayConfig({
      base: base.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allowInsecure: allowInsecure.value,
    })
    ok.value = 'Saved.'
  } catch (e) {
    error.value = `Cannot reach relay: ${e instanceof Error ? e.message : String(e)}`
  } finally {
    submitting.value = false
  }
}

function onDisconnect(): void {
  clearRelayConfig()
  location.replace('/setup.html')
}
</script>

<template>
  <n-card title="Relay">
    <n-form @submit.prevent="onSave">
      <n-form-item label="Relay URL" :feedback="baseError ?? ''" v-bind="baseError ? { 'validation-status': 'error' as const } : {}">
        <n-input
          v-model:value="base"
          placeholder="https://relay.example.com"
          :input-props="{ name: 'relay-base', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item label="API token">
        <n-input
          v-model:value="token"
          type="password"
          show-password-on="click"
          placeholder="atk_…"
          :input-props="{ name: 'relay-token', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item label="Allow insecure HTTP/WS (non-loopback)">
        <n-switch v-model:value="allowInsecure" :disabled="submitting" />
      </n-form-item>

      <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 1rem;">{{ error }}</n-alert>
      <n-alert v-if="ok" type="success" :show-icon="true" style="margin-bottom: 1rem;">{{ ok }}</n-alert>

      <n-space justify="space-between">
        <n-button data-testid="disconnect" @click="onDisconnect">Disconnect</n-button>
        <n-button type="primary" :loading="submitting" :disabled="submitting" data-testid="save" @click="onSave">Save</n-button>
      </n-space>
    </n-form>
  </n-card>
</template>
