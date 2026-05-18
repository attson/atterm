<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NButton, NSpace, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { enablePushFlow, disablePushFlow, type EnableReason } from '@shared/api/push-flow'
import { testPush } from '@shared/api/push'

const message = useMessage()

const permission = ref<NotificationPermission>('default')
const subscribed = ref(false)
const busy = ref(false)
const supported = ref(true)
const errorMsg = ref('')

const REASON_TEXT: Record<EnableReason, string> = {
  denied: 'Permission denied. Allow notifications in your browser settings and retry.',
  disabled: 'Web push is disabled on this relay.',
  'key-failed': 'Could not fetch the VAPID key from the relay.',
  'subscribe-failed': 'Browser refused to create a subscription.',
  'subscribe-rejected': 'Relay refused the subscription.',
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
      errorMsg.value = REASON_TEXT[result.reason] ?? 'Failed to enable.'
    } else {
      message.success('Notifications enabled.')
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
    message.success('Notifications disabled.')
  } catch (e) {
    errorMsg.value = 'Failed to disable: ' + (e instanceof Error ? e.message : String(e))
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
    message.success(`Test notification sent to ${sent} subscription(s).`)
  } catch (e) {
    if (e instanceof ApiError) {
      errorMsg.value = 'Test failed: ' + e.message
    } else {
      errorMsg.value = 'Test failed: ' + (e instanceof Error ? e.message : String(e))
    }
  } finally {
    busy.value = false
  }
}

onMounted(refreshState)
</script>

<template>
  <section class="notifications-tab">
    <p v-if="!supported" class="hint">This browser does not support service workers; push notifications are unavailable.</p>

    <p v-else data-testid="push-status" class="status">
      Browser permission: <strong>{{ permission }}</strong> · Subscribed: <strong>{{ subscribed ? 'yes' : 'no' }}</strong>
    </p>

    <n-space v-if="supported" class="actions">
      <n-button
        v-if="!subscribed"
        type="primary"
        :loading="busy"
        data-testid="push-enable"
        @click="onEnable"
      >Enable notifications</n-button>
      <n-button
        v-else
        :loading="busy"
        data-testid="push-disable"
        @click="onDisable"
      >Disable notifications</n-button>
      <n-button
        :disabled="!subscribed || busy"
        tertiary
        data-testid="push-test"
        @click="onTest"
      >Send test notification</n-button>
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
