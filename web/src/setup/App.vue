<script setup lang="ts">
import { ref, computed } from 'vue'
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
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { useI18n } from '@shared/i18n/useI18n'

const base = ref('https://')
const token = ref('')
const allowInsecure = ref(false)
const error = ref<string | null>(null)
const submitting = ref(false)
const { t } = useI18n()

function getReasonBanner(): string | null {
  const params = new URLSearchParams(location.search)
  if (params.get('reason') === 'token_invalid') {
    return t('setup.reasonTokenInvalid')
  }
  return null
}

const reasonBanner = ref<string | null>(getReasonBanner())

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
      error.value = t('setup.errors.tokenInvalid')
      return
    }
    if (res.status === 403) {
      error.value = t('setup.errors.originRejected')
      return
    }
    if (!res.ok) {
      error.value = t('setup.errors.relayHttpCheckUrl', { status: res.status })
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
    error.value = t('setup.errors.cannotReachRelay', {
      message: e instanceof Error ? e.message : String(e),
    })
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
        <LanguageSelect class="setup-language" />
        <n-card :title="t('setup.connectToRelay')" class="setup-card">
          <n-alert
            v-if="reasonBanner"
            type="warning"
            :show-icon="true"
            style="margin-bottom: 1rem;"
          >{{ reasonBanner }}</n-alert>

          <n-form @submit.prevent="onConnect">
            <n-form-item :label="t('setup.relayUrl')" :feedback="baseError ?? ''" v-bind="baseError ? { 'validation-status': 'error' } : {}">
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
              >{{ t('setup.connect') }}</n-button>
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
.setup-language {
  position: fixed;
  top: 1rem;
  right: 1rem;
}
</style>
