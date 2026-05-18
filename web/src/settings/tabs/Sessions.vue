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

const rows = ref<SessionRow[]>([])
const loading = ref(true)
const signingOut = ref(false)
const message = useMessage()

function describeUA(ua: string): string {
  if (!ua) return 'Unknown device'
  if (ua.includes('Firefox')) return 'Firefox'
  if (ua.includes('Edg/')) return 'Edge'
  if (ua.includes('Chrome')) return 'Chrome'
  if (ua.includes('Safari')) return 'Safari'
  return ua.length > 40 ? ua.slice(0, 40) + '…' : ua
}

function describeWhen(ms: number): string {
  try {
    return new Date(ms).toLocaleString()
  } catch {
    return ''
  }
}

async function reload() {
  loading.value = true
  try {
    rows.value = await listSessions()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load sessions.')
  } finally {
    loading.value = false
  }
}

async function onRevoke(idHash: string) {
  try {
    await revokeSession(idHash)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Revoke failed.')
  }
}

async function onSignOutOthers() {
  if (signingOut.value) return
  signingOut.value = true
  try {
    const result = await signOutOthers()
    message.success(`Signed out ${result.deleted} other device${result.deleted === 1 ? '' : 's'}.`)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Sign-out-others failed.')
  } finally {
    signingOut.value = false
  }
}

onMounted(reload)
</script>

<template>
  <n-card title="Signed-in devices" :bordered="false">
    <p class="muted">Each row is a browser or PWA where this account is signed in.</p>
    <n-list v-if="rows.length > 0" bordered>
      <n-list-item v-for="row in rows" :key="row.id_hash">
        <n-thing>
          <template #header>
            {{ describeUA(row.user_agent) }}
            <n-tag v-if="row.is_current" type="success" size="small" round style="margin-left: 0.5rem;">
              this device
            </n-tag>
          </template>
          <template #description>
            signed in {{ describeWhen(row.created_at) }} · {{ row.ip_prefix || 'ip unknown' }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm v-if="!row.is_current" @positive-click="onRevoke(row.id_hash)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-session-${row.id_hash}`">
                Revoke
              </n-button>
            </template>
            Revoke this device? You'll need to sign in again on it.
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">No active sessions.</p>

    <div class="actions">
      <n-popconfirm @positive-click="onSignOutOthers">
        <template #trigger>
          <n-button
            type="error"
            :loading="signingOut"
            :disabled="signingOut"
            data-testid="sign-out-others"
          >
            Sign out everywhere except this device
          </n-button>
        </template>
        Sign out every other device? They'll all need to sign in again.
      </n-popconfirm>
    </div>
  </n-card>
</template>

<style scoped>
.muted { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.actions { margin-top: 1rem; }
</style>
