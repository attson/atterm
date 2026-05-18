<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
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

const rows = ref<AdminUserRow[]>([])
const loading = ref(true)
const errorMsg = ref('')
const secrets = ref<{ label: string; plaintext: string }[]>([])
const message = useMessage()

function fmt(iso: string | undefined): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function mapError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'last_admin') return "Can't demote the last admin — promote another user first."
    if (e.code === 'cannot_demote_self') return "You can't demote yourself."
  }
  return 'Action failed.'
}

async function reload() {
  loading.value = true
  errorMsg.value = ''
  try {
    rows.value = await listUsers()
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = 'Failed to load users.'
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
    secrets.value = [{ label: `Temporary password for ${email}`, plaintext }]
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
    return h(NTag, { size: 'small', type: 'error' }, { default: () => `disabled ${fmt(row.disabled_at)}` })
  }
  if (row.is_admin) {
    return h(NTag, { size: 'small', type: 'success' }, { default: () => 'admin' })
  }
  return h(NTag, { size: 'small', type: 'default' }, { default: () => 'active' })
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
              { default: () => 'Demote' },
            ),
          default: () => 'Demote this admin?',
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
              { default: () => 'Promote' },
            ),
          default: () => 'Promote this user to admin?',
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
          { default: () => 'Reset password' },
        ),
      default: () => 'Reset password? A new temporary password is shown once.',
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
          { default: () => 'Disable' },
        ),
      default: () => 'Disable this user? They are signed out and cannot log in.',
    },
  )
  return h(NSpace, {}, { default: () => [adminBtn, resetBtn, disableBtn] })
}

const columns: DataTableColumns<AdminUserRow> = [
  { title: 'Email', key: 'email' },
  { title: 'ID', key: 'id', render: (r) => h('code', {}, r.id) },
  { title: 'Created', key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: 'Status', key: 'status', render: statusCell },
  { title: 'Actions', key: 'actions', render: actionsCell },
]

onMounted(reload)
</script>

<template>
  <n-card title="Users" :bordered="false">
    <n-alert
      v-for="(s, i) in secrets"
      :key="i"
      type="success"
      :show-icon="false"
      class="secret-alert"
    >
      <div class="secret-msg">{{ s.label }} — copy it now, only shown once.</div>
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
