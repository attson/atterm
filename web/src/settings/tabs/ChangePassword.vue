<script setup lang="ts">
import { ref } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton } from 'naive-ui'
import { changePassword } from '@shared/api/me'
import { ApiError } from '@shared/api/client'

const current = ref('')
const next = ref('')
const submitting = ref(false)
const errorMsg = ref('')

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'current_password_wrong') return 'Current password is incorrect.'
    if (e.code === 'password_weak') return 'New password must be at least 12 characters.'
    if (e.code === 'invalid_request') return 'Please check your input.'
  }
  return 'Password change failed. Please try again.'
}

async function onSubmit(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  errorMsg.value = ''
  submitting.value = true
  try {
    await changePassword(current.value, next.value)
    // Server invalidated all sessions including ours; the relay just
    // issued a fresh cookie, but the safest UX is to bounce through
    // /login.html so the new credentials are exercised explicitly.
    location.assign('/login.html')
  } catch (err) {
    errorMsg.value = mapError(err)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <n-card :bordered="false">
    <form @submit="onSubmit" autocomplete="off" novalidate>
      <n-form label-placement="top" require-mark-placement="right-hanging">
        <n-form-item label="Current password" :show-feedback="false">
          <n-input
            v-model:value="current"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'current-password' }"
          />
        </n-form-item>
        <n-form-item label="New password (min 12 characters)" :show-feedback="false">
          <n-input
            v-model:value="next"
            type="password"
            show-password-on="click"
            :input-props="{ required: true, autocomplete: 'new-password', minlength: 12 }"
          />
        </n-form-item>
        <n-button
          type="primary"
          attr-type="submit"
          :loading="submitting"
          :disabled="submitting"
        >
          Update password
        </n-button>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
      </n-form>
    </form>
  </n-card>
</template>

<style scoped>
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
