<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NCard,
  NInput,
  NSwitch,
  NButton,
  NAlert,
  NSpace,
  NForm,
  NFormItem,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import {
  saveRelayConfig,
  validateRelayBase,
  type RelayConfig,
} from '@shared/api/relay-config'

const base = ref('https://')
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)

function getReasonBanner(): string | null {
  const params = new URLSearchParams(location.search)
  if (params.get('reason') === 'token_invalid') {
    return 'Your API token is no longer valid. Please paste a fresh token to sign in again.'
  }
  return null
}

const reasonBanner = ref<string | null>(getReasonBanner())

onMounted(() => {
  reasonBanner.value = getReasonBanner()
})

const baseError = computed(() => {
  if (!base.value || base.value === 'https://') return null
  return validateRelayBase(base.value, allowInsecure.value)
})

async function onConnect(): Promise<void> {
  error.value = null
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
      error.value = 'API token is invalid. Generate a new one from Settings → API Tokens on the relay web UI.'
      return
    }
    if (res.status === 403) {
      error.value =
        'Relay rejected the origin. Make sure the relay was started with ATTERM_ORIGINS containing capacitor://localhost.'
      return
    }
    if (!res.ok) {
      error.value = `Relay returned HTTP ${res.status}. Check the URL and try again.`
      return
    }
    const cfg: RelayConfig = {
      base: base.value.replace(/\/$/, ''),
      token: token.value.trim(),
      allowInsecure: allowInsecure.value,
    }
    saveRelayConfig(cfg)
    location.replace('/')
  } catch (e) {
    error.value = `Cannot reach relay: ${e instanceof Error ? e.message : String(e)}`
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <main class="setup-page">
        <n-card title="Connect to relay" class="setup-card">
          <n-alert
            v-if="reasonBanner"
            type="warning"
            :show-icon="true"
            style="margin-bottom: 1rem;"
          >{{ reasonBanner }}</n-alert>

          <n-form @submit.prevent="onConnect">
            <n-form-item label="Relay URL" :feedback="baseError ?? ''" v-bind="baseError ? { 'validation-status': 'error' } : {}">
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
              <n-switch
                v-model:value="allowInsecure"
                :disabled="submitting"
              />
            </n-form-item>

            <n-alert v-if="error" type="error" :show-icon="true" style="margin-bottom: 1rem;">
              {{ error }}
            </n-alert>

            <n-space justify="end">
              <n-button
                type="primary"
                :loading="submitting"
                :disabled="submitting"
                data-testid="connect"
                @click="onConnect"
              >Connect</n-button>
            </n-space>
          </n-form>
        </n-card>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.setup-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem 1rem;
  background: var(--bg);
  color: var(--fg);
}
.setup-card {
  width: 100%;
  max-width: 480px;
}
</style>
