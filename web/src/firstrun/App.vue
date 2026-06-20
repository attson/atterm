<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
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
import { ApiError } from '@shared/api/client'
import { register, getBootstrapStatus } from '@shared/api/auth'
import { fetchVersion, formatVersionLabel } from '@shared/api/version'
import LanguageSelect from '@shared/components/LanguageSelect.vue'
import { useI18n } from '@shared/i18n/useI18n'
import { naiveLocale } from '@shared/i18n/naive-locale'

const email = ref('')
const password = ref('')
const confirm = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const ready = ref(false)
const version = ref('dev')
const { t } = useI18n()
const versionLabel = computed(() => formatVersionLabel(version.value, t))

onMounted(async () => {
  // If an admin already exists, this page is done — send to login.
  try {
    const { admin_exists } = await getBootstrapStatus()
    if (admin_exists) {
      location.replace('/login.html')
      return
    }
  } catch {
    /* ignore — still show the form; the relay may be momentarily unreachable */
  }
  ready.value = true
  version.value = await fetchVersion()
})

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_taken') return t('auth.errors.emailTaken')
    if (e.code === 'password_weak') return t('auth.errors.passwordWeak')
    if (e.code === 'invalid_email') return t('auth.errors.invalidEmail')
    if (e.code === 'rate_limited') return t('auth.errors.rateLimitedShort')
    if (e.code === 'invalid_request') return t('auth.errors.invalidRequest')
  }
  return t('auth.setup.failed')
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  if (password.value !== confirm.value) {
    errorMsg.value = t('auth.setup.passwordMismatch')
    return
  }
  submitting.value = true
  try {
    const res = await register(email.value.trim(), password.value)
    if (res.is_admin) {
      location.assign('/')
    } else {
      // Account created but the email didn't match ATTERM_BOOTSTRAP_ADMIN_EMAIL,
      // so no admin role was granted.
      errorMsg.value = t('auth.setup.notAdmin')
    }
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

const overrides = getNaiveOverrides()
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :theme-overrides="overrides"
    :locale="naiveLocale.locale"
    :date-locale="naiveLocale.dateLocale"
  >
    <n-message-provider>
      <main class="auth-page">
        <LanguageSelect class="auth-language" />
        <n-card v-if="ready" class="auth-card" :bordered="false">
          <header class="auth-title">
            <h1>{{ t('common.appName') }}</h1>
            <p class="auth-subtitle">{{ t('auth.setup.title') }}</p>
          </header>
          <p class="auth-hint">{{ t('auth.setup.hint') }}</p>
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
                :placeholder="t('auth.setup.emailPlaceholder')"
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
            <n-form-item :label="t('auth.setup.confirmPassword')" :show-feedback="false">
              <n-input
                v-model:value="confirm"
                type="password"
                show-password-on="click"
                :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
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
              {{ t('auth.setup.createAdmin') }}
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
  margin-bottom: 0.5rem;
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
.auth-hint {
  color: var(--fg-dim);
  font-size: 0.8125rem;
  text-align: center;
  margin: 0 0 1rem;
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

<style>
/* Autofill background override — unscoped (see web/src/login/App.vue). */
.auth-page input:-webkit-autofill,
.auth-page input:-webkit-autofill:hover,
.auth-page input:-webkit-autofill:focus,
.auth-page input:-webkit-autofill:active {
  -webkit-text-fill-color: var(--fg) !important;
  caret-color: var(--fg);
  -webkit-box-shadow: 0 0 0 1000px var(--panel) inset !important;
  box-shadow: 0 0 0 1000px var(--panel) inset !important;
  transition: background-color 9999s ease-out;
}
</style>
