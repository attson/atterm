<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NButton, NSpace, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { enablePushFlow, disablePushFlow, type EnableReason } from '@shared/api/push-flow'
import { testPush } from '@shared/api/push'
import { useI18n } from '@shared/i18n/useI18n'

const message = useMessage()
const { t } = useI18n()

const permission = ref<NotificationPermission>('default')
const subscribed = ref(false)
const busy = ref(false)
const supported = ref(true)
const errorMsg = ref('')

const REASON_KEY: Record<EnableReason, string> = {
  denied: 'settings.notificationsTab.errors.denied',
  disabled: 'settings.notificationsTab.errors.disabled',
  'key-failed': 'settings.notificationsTab.errors.keyFailed',
  'subscribe-failed': 'settings.notificationsTab.errors.subscribeFailed',
  'subscribe-rejected': 'settings.notificationsTab.errors.subscribeRejected',
}

async function refreshState() {
  if (typeof Notification === 'undefined' || typeof navigator === 'undefined' || !('serviceWorker' in navigator)) {
    supported.value = false
    return
  }
  permission.value = Notification.permission
  try {
    const reg = await navigator.serviceWorker.ready
    const sub = await reg.pushManager.getSubscription()
    subscribed.value = sub !== null
  } catch {
    subscribed.value = false
  }
}

async function onEnable() {
  busy.value = true
  errorMsg.value = ''
  try {
    const reg = await navigator.serviceWorker.ready
    const result = await enablePushFlow({
      notification: { permission: Notification.permission, requestPermission: () => Notification.requestPermission() },
      registration: reg,
    })
    if (!result.ok) {
      errorMsg.value = t(REASON_KEY[result.reason] ?? 'settings.notificationsTab.errors.fallback')
    } else {
      message.success(t('settings.notificationsTab.enabled'))
    }
  } finally {
    busy.value = false
    await refreshState()
  }
}

async function onDisable() {
  busy.value = true
  errorMsg.value = ''
  try {
    const reg = await navigator.serviceWorker.ready
    await disablePushFlow({ registration: reg })
    message.success(t('settings.notificationsTab.disabled'))
  } catch (e) {
    errorMsg.value = t('settings.notificationsTab.disableFailed', {
      message: e instanceof Error ? e.message : String(e),
    })
  } finally {
    busy.value = false
    await refreshState()
  }
}

async function onTest() {
  busy.value = true
  errorMsg.value = ''
  try {
    const sent = await testPush()
    message.success(t('settings.notificationsTab.testSent', { count: sent }))
  } catch (e) {
    if (e instanceof ApiError) {
      errorMsg.value = t('settings.notificationsTab.testFailed', { message: e.message })
    } else {
      errorMsg.value = t('settings.notificationsTab.testFailed', {
        message: e instanceof Error ? e.message : String(e),
      })
    }
  } finally {
    busy.value = false
  }
}

onMounted(refreshState)
</script>

<template>
  <section class="notifications-tab">
    <p v-if="!supported" class="hint">{{ t('settings.notificationsTab.unsupported') }}</p>

    <p v-else data-testid="push-status" class="status">
      {{ t('settings.notificationsTab.browserPermission') }}:
      <strong>{{ permission }}</strong>
      · {{ t('settings.notificationsTab.subscribed') }}:
      <strong>{{ subscribed ? t('common.yes') : t('common.no') }}</strong>
    </p>

    <n-space v-if="supported" class="actions">
      <n-button
        v-if="!subscribed"
        type="primary"
        :loading="busy"
        data-testid="push-enable"
        @click="onEnable"
      >{{ t('settings.notificationsTab.enableNotifications') }}</n-button>
      <n-button
        v-else
        :loading="busy"
        data-testid="push-disable"
        @click="onDisable"
      >{{ t('settings.notificationsTab.disableNotifications') }}</n-button>
      <n-button
        :disabled="!subscribed || busy"
        tertiary
        data-testid="push-test"
        @click="onTest"
      >{{ t('settings.notificationsTab.sendTest') }}</n-button>
    </n-space>

    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </section>
</template>

<style scoped>
.notifications-tab { display: flex; flex-direction: column; gap: 1rem; color: var(--fg); }
.status { color: var(--fg-dim); margin: 0; }
.status strong { color: var(--fg); }
.actions { margin-top: 0.5rem; }
.hint { color: var(--fg-dim); font-size: 0.875rem; }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0; }
</style>
