<script setup lang="ts">
import { computed, ref, onMounted, h } from 'vue'
import {
  NCard,
  NDataTable,
  NButton,
  NPopconfirm,
  NTag,
  NAlert,
  NSpace,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import {
  listUsers,
  promoteUser,
  demoteUser,
  resetUserPassword,
  disableUser,
} from '@shared/api/admin'
import type { AdminUserRow } from '@shared/api/types'
import { formatDateTime } from '@shared/i18n'
import { useI18n } from '@shared/i18n/useI18n'

const rows = ref<AdminUserRow[]>([])
const loading = ref(true)
const errorMsg = ref('')
const secrets = ref<{ label: string; plaintext: string }[]>([])
const message = useMessage()
const { t } = useI18n()

function fmt(iso: string | undefined): string {
  return formatDateTime(iso)
}

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'last_admin') return t('admin.errors.lastAdmin')
    if (e.code === 'cannot_demote_self') return t('admin.errors.cannotDemoteSelf')
  }
  return t('admin.errors.actionFailed')
}

async function reload() {
  loading.value = true
  errorMsg.value = ''
  try {
    rows.value = await listUsers()
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('admin.loadUsersFailed')
  } finally {
    loading.value = false
  }
}

async function onPromote(id: string) {
  errorMsg.value = ''
  try {
    await promoteUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onDemote(id: string) {
  errorMsg.value = ''
  try {
    await demoteUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onResetPassword(id: string, email: string) {
  errorMsg.value = ''
  secrets.value = []
  try {
    const { plaintext } = await resetUserPassword(id)
    secrets.value = [{ label: t('admin.temporaryPasswordFor', { email }), plaintext }]
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

async function onDisable(id: string) {
  errorMsg.value = ''
  try {
    await disableUser(id)
    await reload()
  } catch (e) {
    errorMsg.value = mapError(e)
  }
}

function statusCell(row: AdminUserRow) {
  if (row.disabled_at) {
    return h(NTag, { size: 'small', type: 'error' }, { default: () => t('admin.disabled', { when: fmt(row.disabled_at) }) })
  }
  if (row.is_admin) {
    return h(NTag, { size: 'small', type: 'success' }, { default: () => t('admin.userStatus.admin') })
  }
  return h(NTag, { size: 'small', type: 'default' }, { default: () => t('admin.userStatus.active') })
}

function actionsCell(row: AdminUserRow) {
  if (row.disabled_at) return null
  const adminBtn = row.is_admin
    ? h(
        NPopconfirm,
        { onPositiveClick: () => onDemote(row.id) },
        {
          trigger: () =>
            h(
              NButton,
              { size: 'small', 'data-testid': `demote-${row.id}` } as Record<string, unknown>,
              { default: () => t('admin.demote') },
            ),
          default: () => t('admin.demoteConfirm'),
        },
      )
    : h(
        NPopconfirm,
        { onPositiveClick: () => onPromote(row.id) },
        {
          trigger: () =>
            h(
              NButton,
              { size: 'small', 'data-testid': `promote-${row.id}` } as Record<string, unknown>,
              { default: () => t('admin.promote') },
            ),
          default: () => t('admin.promoteConfirm'),
        },
      )
  const resetBtn = h(
    NPopconfirm,
    { onPositiveClick: () => onResetPassword(row.id, row.email) },
    {
      trigger: () =>
        h(
          NButton,
          { size: 'small', 'data-testid': `reset-${row.id}` } as Record<string, unknown>,
          { default: () => t('admin.resetPassword') },
        ),
      default: () => t('admin.resetPasswordConfirm'),
    },
  )
  const disableBtn = h(
    NPopconfirm,
    { onPositiveClick: () => onDisable(row.id) },
    {
      trigger: () =>
        h(
          NButton,
          { size: 'small', type: 'error', 'data-testid': `disable-${row.id}` } as Record<string, unknown>,
          { default: () => t('admin.disable') },
        ),
      default: () => t('admin.disableConfirm'),
    },
  )
  return h(NSpace, {}, { default: () => [adminBtn, resetBtn, disableBtn] })
}

const columns = computed<DataTableColumns<AdminUserRow>>(() => [
  { title: t('admin.email'), key: 'email' },
  { title: t('admin.id'), key: 'id', render: (r) => h('code', {}, r.id) },
  { title: t('admin.created'), key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: t('admin.status'), key: 'status', render: statusCell },
  { title: t('admin.actions'), key: 'actions', render: actionsCell },
])

onMounted(reload)
</script>

<template>
  <n-card :title="t('admin.users')" :bordered="false">
    <n-alert
      v-for="(s, i) in secrets"
      :key="i"
      type="success"
      :show-icon="false"
      class="secret-alert"
    >
      <div class="secret-msg">{{ t('admin.secretCopyOnce', { label: s.label }) }}</div>
      <code class="secret-display">{{ s.plaintext }}</code>
    </n-alert>
    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="loading"
      size="small"
      :bordered="false"
      :pagination="false"
    />
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </n-card>
</template>

<style scoped>
.secret-alert { margin-bottom: 0.5rem; }
.secret-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.secret-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
