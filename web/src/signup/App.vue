<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NConfigProvider,
  NMessageProvider,
  NCard,
  NForm,
  NFormItem,
  NInput,
  NButton,
  darkTheme,
} from 'naive-ui'
import { getNaiveOverrides } from '@shared/theme/naive-theme'
import { safeNext, ApiError } from '@shared/api/client'
import { signup } from '@shared/api/auth'
import { fetchVersionLabel } from '@shared/api/version'
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { useI18n } from '@shared/i18n/useI18n'

const email = ref('')
const password = ref('')
const inviteCode = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const versionLabel = ref('version dev')
const { t } = useI18n()

onMounted(async () => {
  versionLabel.value = await fetchVersionLabel()
})

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_taken') return t('auth.errors.emailTaken')
    if (e.code === 'invite_invalid') return t('auth.errors.inviteInvalid')
    if (e.code === 'password_weak') return t('auth.errors.passwordWeak')
    if (e.code === 'invalid_email') return t('auth.errors.invalidEmail')
    if (e.code === 'rate_limited') return t('auth.errors.rateLimitedShort')
    if (e.code === 'invalid_request') return t('auth.errors.invalidRequest')
  }
  return t('auth.errors.signUpFailed')
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await signup(email.value, password.value, inviteCode.value)
    const nextParam = new URLSearchParams(location.search).get('next')
    location.assign(safeNext(nextParam))
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider :theme="darkTheme" :theme-overrides="overrides">
    <n-message-provider>
      <main class="auth-page">
        <LanguageSelect class="auth-language" />
        <n-card class="auth-card" :bordered="false">
          <header class="auth-title">
            <h1>{{ t('common.appName') }}</h1>
            <p class="auth-subtitle">{{ t('auth.signUp') }}</p>
          </header>
          <n-form
            label-placement="top"
            require-mark-placement="right-hanging"
            autocomplete="on"
            novalidate
            @submit="onSubmit"
          >
            <n-form-item :label="t('auth.email')" :show-feedback="false">
                <n-input
                  v-model:value="email"
                  type="text"
                  :placeholder="t('auth.emailPlaceholder')"
                  :input-props="{ type: 'email', required: true, autocomplete: 'username' }"
                />
              </n-form-item>
              <n-form-item :label="t('auth.password')" :show-feedback="false">
                <n-input
                  v-model:value="password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
                />
              </n-form-item>
              <n-form-item :label="t('auth.inviteCode')" :show-feedback="false">
                <n-input
                  v-model:value="inviteCode"
                  type="text"
                  :input-props="{ required: true, autocomplete: 'off' }"
                />
              </n-form-item>
              <n-button
                class="submit-btn"
                type="primary"
                attr-type="submit"
                :loading="submitting"
                :disabled="submitting"
                block
              >
                {{ t('auth.createAccount') }}
              </n-button>
              <p v-if="errorMsg" class="auth-error" role="alert">{{ errorMsg }}</p>
              <p class="auth-alt">
                {{ t('auth.alreadyHaveAccount') }}
                <a href="/login.html">{{ t('auth.signIn') }}</a>.
              </p>
          </n-form>
        </n-card>
        <p class="auth-version">{{ versionLabel }}</p>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  background: var(--bg);
  color: var(--fg);
  display: grid;
  grid-template-rows: 1fr auto;
  place-items: center;
  padding: 2rem 1rem;
  gap: 1rem;
}
.auth-card {
  max-width: 420px;
  width: 100%;
  background: var(--panel);
}
.auth-language {
  position: fixed;
  top: 1rem;
  right: 1rem;
}
.auth-title {
  text-align: center;
  margin-bottom: 1rem;
}
.auth-title h1 {
  margin: 0;
  font-size: 1.5rem;
  letter-spacing: 0.08em;
}
.auth-subtitle {
  margin: 0.25rem 0 0;
  color: var(--fg-dim);
  font-size: 0.875rem;
  text-transform: lowercase;
  letter-spacing: 0.1em;
}
.submit-btn { margin-top: 1rem; }
.auth-error {
  color: var(--bad);
  margin: 0.75rem 0 0;
  font-size: 0.875rem;
}
.auth-alt {
  margin: 1rem 0 0;
  font-size: 0.875rem;
  color: var(--fg-dim);
  text-align: center;
}
.auth-alt a {
  color: var(--accent);
}
.auth-version {
  color: var(--fg-dim);
  font-size: 0.75rem;
  margin: 0;
}
</style>
