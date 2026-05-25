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

const email = ref('')
const password = ref('')
const inviteCode = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const versionLabel = ref('version dev')

onMounted(async () => {
  versionLabel.value = await fetchVersionLabel()
})

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_taken') return 'An account with that email already exists.'
    if (e.code === 'invite_invalid') return 'Invite code is invalid or already used.'
    if (e.code === 'password_weak') return 'Password must be at least 12 characters.'
    if (e.code === 'invalid_email') return 'Please enter a valid email.'
    if (e.code === 'rate_limited') return 'Too many attempts. Please wait.'
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Sign-up failed. Check your invite code and try again.'
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
            <h1>AT Term</h1>
            <p class="auth-subtitle">sign up</p>
          </header>
          <n-form
            label-placement="top"
            require-mark-placement="right-hanging"
            autocomplete="on"
            novalidate
            @submit="onSubmit"
          >
            <n-form-item label="Email" :show-feedback="false">
                <n-input
                  v-model:value="email"
                  type="text"
                  placeholder="you@example.com"
                  :input-props="{ type: 'email', required: true, autocomplete: 'username' }"
                />
              </n-form-item>
              <n-form-item label="Password" :show-feedback="false">
                <n-input
                  v-model:value="password"
                  type="password"
                  show-password-on="click"
                  :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
                />
              </n-form-item>
              <n-form-item label="Invite code" :show-feedback="false">
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
                Create account
              </n-button>
              <p v-if="errorMsg" class="auth-error" role="alert">{{ errorMsg }}</p>
              <p class="auth-alt">
                Already have an account?
                <a href="/login.html">Sign in</a>.
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
