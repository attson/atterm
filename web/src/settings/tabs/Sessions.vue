<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NList,
  NListItem,
  NThing,
  NButton,
  NTag,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listSessions, revokeSession, signOutOthers } from '@shared/api/me'
import type { SessionRow } from '@shared/api/types'
import { formatDateTime } from '@shared/i18n'
import { useI18n } from '@shared/i18n/useI18n'

const rows = ref<SessionRow[]>([])
const loading = ref(true)
const signingOut = ref(false)
const message = useMessage()
const { t } = useI18n()

function describeUA(ua: string): string {
  if (!ua) return t('settings.sessionsTab.unknownDevice')
  if (ua.includes('Firefox')) return 'Firefox'
  if (ua.includes('Edg/')) return 'Edge'
  if (ua.includes('Chrome')) return 'Chrome'
  if (ua.includes('Safari')) return 'Safari'
  return ua.length > 40 ? ua.slice(0, 40) + '…' : ua
}

function describeWhen(ms: number): string {
  return formatDateTime(ms)
}

async function reload() {
  loading.value = true
  try {
    rows.value = await listSessions()
  } catch (e) {
    if (e instanceof ApiError) message.error(t('settings.sessionsTab.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function onRevoke(idHash: string) {
  try {
    await revokeSession(idHash)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error(t('settings.sessionsTab.revokeFailed'))
  }
}

async function onSignOutOthers() {
  if (signingOut.value) return
  signingOut.value = true
  try {
    const result = await signOutOthers()
    message.success(t('settings.sessionsTab.signOutOthersSuccess', { count: result.deleted }))
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error(t('settings.sessionsTab.signOutOthersFailed'))
  } finally {
    signingOut.value = false
  }
}

onMounted(reload)
</script>

<template>
  <n-card :bordered="false">
    <p class="muted">{{ t('settings.sessionsTab.hint') }}</p>
    <n-list v-if="rows.length > 0" bordered>
      <n-list-item v-for="row in rows" :key="row.id_hash">
        <n-thing>
          <template #header>
            {{ describeUA(row.user_agent) }}
            <n-tag v-if="row.is_current" type="success" size="small" round style="margin-left: 0.5rem;">
              {{ t('settings.sessionsTab.currentDevice') }}
            </n-tag>
          </template>
          <template #description>
            {{ t('settings.sessionsTab.signedIn') }} {{ describeWhen(row.created_at) }} · {{ row.ip_prefix || t('settings.sessionsTab.ipUnknown') }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm v-if="!row.is_current" @positive-click="onRevoke(row.id_hash)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-session-${row.id_hash}`">
                {{ t('common.revoke') }}
              </n-button>
            </template>
            {{ t('settings.sessionsTab.revokeConfirm') }}
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">{{ t('settings.sessionsTab.empty') }}</p>

    <div class="actions">
      <n-popconfirm @positive-click="onSignOutOthers">
        <template #trigger>
          <n-button
            type="error"
            :loading="signingOut"
            :disabled="signingOut"
            data-testid="sign-out-others"
          >
            {{ t('settings.sessionsTab.signOutOthers') }}
          </n-button>
        </template>
        {{ t('settings.sessionsTab.signOutOthersConfirm') }}
      </n-popconfirm>
    </div>
  </n-card>
</template>

<style scoped>
.muted { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.actions { margin-top: 1rem; }
</style>
