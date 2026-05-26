<script setup lang="ts">
import { ref } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton, NPopconfirm } from 'naive-ui'
import { deleteMe } from '@shared/api/me'
import { ApiError } from '@shared/api/client'
import { useI18n } from '@shared/i18n/useI18n'

const email = ref('')
const password = ref('')
const submitting = ref(false)
const errorMsg = ref('')
const { t } = useI18n()

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'email_mismatch') return t('settings.danger.emailMismatch')
    if (e.code === 'password_incorrect') return t('settings.danger.passwordIncorrect')
    if (e.code === 'last_admin') return t('settings.danger.lastAdmin')
    if (e.code === 'invalid_request') return t('settings.danger.invalidRequest')
  }
  return t('settings.danger.failed')
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
  <n-card :bordered="false" class="danger-card">
    <p>
      {{ t('settings.danger.description') }}
    </p>
    <n-form
      label-placement="top"
      require-mark-placement="right-hanging"
      autocomplete="off"
      novalidate
      @submit="onSubmit"
    >
        <n-form-item :label="t('settings.danger.typeEmail')" :show-feedback="false">
          <n-input
            v-model:value="email"
            type="text"
            :input-props="{ type: 'email', required: true, autocomplete: 'off' }"
          />
        </n-form-item>
        <n-form-item :label="t('settings.danger.currentPassword')" :show-feedback="false">
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
              class="submit-btn"
              type="error"
              attr-type="button"
              :loading="submitting"
              :disabled="submitting"
              data-testid="delete-account-trigger"
            >
              {{ t('settings.danger.deleteAccount') }}
            </n-button>
          </template>
          {{ t('settings.danger.confirmDelete') }}
        </n-popconfirm>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
    </n-form>
  </n-card>
</template>

<style scoped>
.submit-btn { margin-top: 1rem; }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
