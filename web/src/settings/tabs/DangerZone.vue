<script setup lang="ts">
import { ref } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton, NPopconfirm } from 'naive-ui'
import { deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

const email = ref('')
const password = ref('')
const submitting = ref(false)
const errorMsg = ref('')

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_mismatch') return "Email doesn't match — type your exact email."
    if (e.code === 'password_incorrect') return 'Password is incorrect.'
    if (e.code === 'last_admin') return "You're the last admin — promote another user first."
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Delete failed. Please try again.'
}

async function performDelete() {
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await deleteMe(email.value.trim(), password.value)
    location.assign('/login.html')
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}

function onSubmit(e: Event) {
  // The submit button is wrapped in <n-popconfirm>; the actual deletion
  // fires from the popconfirm's positive-click handler. The native
  // submit event is intercepted here so the browser does not reload.
  e.preventDefault()
}
</script>

<template>
  <n-card title="Danger zone" :bordered="false" class="danger-card">
    <p>
      Permanently delete this account. This cannot be undone. API tokens, web
      sessions, and account data are removed. Invitations you've consumed
      stay (their "consumed by" field is cleared).
    </p>
    <form @submit="onSubmit" autocomplete="off" novalidate>
      <n-form label-placement="top" require-mark-placement="right-hanging">
        <n-form-item label="Confirm by typing your full email" :show-feedback="false">
          <n-input
            v-model:value="email"
            type="text"
            :input-props="{ type: 'email', required: true, autocomplete: 'off' }"
          />
        </n-form-item>
        <n-form-item label="Current password" :show-feedback="false">
          <n-input
            v-model:value="password"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'current-password' }"
          />
        </n-form-item>
        <n-popconfirm @positive-click="performDelete">
          <template #trigger>
            <n-button
              type="error"
              attr-type="button"
              :loading="submitting"
              :disabled="submitting"
              data-testid="delete-account-trigger"
            >
              Delete my account
            </n-button>
          </template>
          Permanently delete this account? This cannot be undone.
        </n-popconfirm>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
      </n-form>
    </form>
  </n-card>
</template>

<style scoped>
.danger-card :deep(.n-card-header__main) { color: var(--bad); }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
