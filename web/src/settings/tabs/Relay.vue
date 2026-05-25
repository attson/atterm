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
import { useI18n } from '@shared/i18n/useI18n'

const _cfg = loadRelayConfig()
const base = ref(_cfg?.base ?? '')
const token = ref(_cfg?.token ?? '')
const allowInsecure = ref(_cfg?.allowInsecure ?? false)
const error = ref<string | null>(null)
const ok = ref<string | null>(null)
const submitting = ref(false)
const { t } = useI18n()

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
    error.value = t('setup.errors.tokenRequired')
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
      error.value = t('setup.errors.tokenInvalidSettings')
      return
    }
    if (res.status === 403) {
      error.value = t('setup.errors.originRejected')
      return
    }
    if (!res.ok) {
      error.value = t('setup.errors.relayHttp', { status: res.status })
      return
    }
    saveRelayConfig({
      base: base.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allowInsecure: allowInsecure.value,
    })
    ok.value = t('setup.saved')
  } catch (e) {
    error.value = t('setup.errors.cannotReachRelay', {
      message: e instanceof Error ? e.message : String(e),
    })
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
  <n-card :title="t('settings.relay')">
    <n-form @submit.prevent="onSave">
      <n-form-item :label="t('setup.relayUrl')" :feedback="baseError ?? ''" v-bind="baseError ? { 'validation-status': 'error' as const } : {}">
        <n-input
          v-model:value="base"
          :placeholder="t('setup.relayUrlPlaceholder')"
          :input-props="{ name: 'relay-base', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item :label="t('setup.token')">
        <n-input
          v-model:value="token"
          type="password"
          show-password-on="click"
          :placeholder="t('setup.tokenPlaceholder')"
          :input-props="{ name: 'relay-token', autocomplete: 'off' }"
          :disabled="submitting"
        />
      </n-form-item>

      <n-form-item :label="t('setup.allowInsecure')">
        <n-switch v-model:value="allowInsecure" :disabled="submitting" />
      </n-form-item>

      <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 1rem;">{{ error }}</n-alert>
      <n-alert v-if="ok" type="success" :show-icon="true" style="margin-bottom: 1rem;">{{ ok }}</n-alert>

      <n-space justify="space-between">
        <n-button data-testid="disconnect" @click="onDisconnect">{{ t('setup.disconnect') }}</n-button>
        <n-button type="primary" :loading="submitting" :disabled="submitting" data-testid="save" @click="onSave">{{ t('common.save') }}</n-button>
      </n-space>
    </n-form>
  </n-card>
</template>
