<script setup lang="ts">
import { computed, ref, onMounted, h } from 'vue'
import {
  NDataTable,
  NInput,
  NInputNumber,
  NDatePicker,
  NButton,
  NAlert,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { Plus } from 'lucide-vue-next'
import { ApiError } from '@shared/api/client'
import { listInvitations, createInvitation } from '@shared/api/admin'
import type { InvitationCreated, InvitationRow } from '@shared/api/types'
import { formatDateTime } from '@shared/i18n'
import { useI18n } from '@shared/i18n/useI18n'

const rows = ref<InvitationRow[]>([])
const loading = ref(true)
const submitting = ref(false)
const noteInput = ref('')
const countInput = ref<number>(1)
const expiresInput = ref<number | null>(null)
const newSecrets = ref<InvitationCreated[]>([])
const message = useMessage()
const { t } = useI18n()

// data-testid is not in Vue's stock InputHTMLAttributes, so cast through Record<string, unknown>
// for the test-only hooks. Keeps the markup identical at runtime.
const noteInputProps = { 'data-testid': 'invite-note', autocomplete: 'off' } as Record<string, unknown>
const countInputProps = { 'data-testid': 'invite-count' } as Record<string, unknown>
const expiresPickerProps = { 'data-testid': 'invite-expires' } as Record<string, unknown>

function fmt(iso: string | undefined): string {
  return formatDateTime(iso)
}

const columns = computed<DataTableColumns<InvitationRow>>(() => [
  { title: t('admin.invitationsTab.prefix'), key: 'code_prefix', render: (r) => h('code', {}, r.code_prefix) },
  { title: t('admin.invitationsTab.note'), key: 'note' },
  { title: t('admin.created'), key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: t('admin.invitationsTab.expires'), key: 'expires_at', render: (r) => fmt(r.expires_at) },
  {
    title: t('admin.invitationsTab.consumed'),
    key: 'consumed_at',
    render: (r) =>
      r.consumed_at
        ? h('span', {}, `${fmt(r.consumed_at)}${r.consumed_by ? ' · ' + r.consumed_by : ''}`)
        : h(NTag, { size: 'small', type: 'default' }, { default: () => t('admin.invitationsTab.unused') }),
  },
])

async function reload() {
  loading.value = true
  try {
    rows.value = await listInvitations()
  } catch (e) {
    if (e instanceof ApiError) message.error(t('admin.invitationsTab.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function onCreate(e: Event) {
  e.preventDefault()
  if (submitting.value) return
  submitting.value = true
  try {
    const count = Math.max(1, Math.min(50, Math.round(countInput.value || 1)))
    const note = noteInput.value.trim()
    const req: { count: number; note?: string; expires_at?: string } = { count }
    if (note) req.note = note
    if (expiresInput.value != null) {
      // n-date-picker (type=datetime) emits a unix ms timestamp.
      req.expires_at = new Date(expiresInput.value).toISOString()
    }
    const created = await createInvitation(req)
    newSecrets.value = created
    noteInput.value = ''
    expiresInput.value = null
    countInput.value = 1
    await reload()
  } catch (err) {
    if (err instanceof ApiError) message.error(t('admin.invitationsTab.createFailed', { code: err.code }))
  } finally {
    submitting.value = false
  }
}

onMounted(reload)
</script>

<template>
  <section class="admin-page invitations-page">
    <header class="admin-page-header">
      <div><h1>{{ t('admin.invitations') }}</h1></div>
    </header>

    <form @submit="onCreate" autocomplete="off" class="admin-surface create-form">
      <div class="field note-field">
        <label class="field-label">{{ t('admin.invitationsTab.note') }}</label>
        <n-input
          v-model:value="noteInput"
          type="text"
          :placeholder="t('common.optional')"
          :input-props="noteInputProps"
        />
      </div>
      <div class="field">
        <label class="field-label">{{ t('admin.invitationsTab.count') }}</label>
        <n-input-number
          v-model:value="countInput"
          :min="1"
          :max="50"
          :input-props="countInputProps"
        />
      </div>
      <div class="field expires-field">
        <label class="field-label">{{ t('admin.invitationsTab.expires') }}</label>
        <n-date-picker
          v-model:value="expiresInput"
          type="datetime"
          clearable
          :placeholder="t('common.optional')"
          :input-props="expiresPickerProps"
        />
      </div>
      <n-button class="create-button" type="primary" attr-type="submit" :loading="submitting" :disabled="submitting">
        <template #icon><Plus :size="15" /></template>
        {{ t('common.create') }}
      </n-button>
    </form>

    <n-alert
      v-for="(s, i) in newSecrets"
      :key="i"
      type="success"
      :show-icon="false"
      class="secret-alert"
    >
      <div class="secret-msg">
        {{ t('admin.invitationsTab.copyNow', { note: s.note ? ` (${s.note})` : '' }) }}
      </div>
      <code class="secret-display">{{ s.plaintext }}</code>
    </n-alert>

    <div class="admin-surface admin-table-surface">
      <n-data-table
        :columns="columns"
        :data="rows"
        :loading="loading"
        size="small"
        :bordered="false"
        :pagination="false"
      >
        <template #empty><p class="empty">{{ t('admin.invitationsTab.empty') }}</p></template>
      </n-data-table>
    </div>
  </section>
</template>

<style scoped>
.create-form {
  display: grid;
  grid-template-columns: minmax(220px, 1.4fr) 110px minmax(220px, .9fr) auto;
  align-items: end;
  gap: 14px;
  margin-bottom: 14px;
  padding: 16px;
}
.field { display: flex; flex-direction: column; gap: 0.25rem; }
.field :deep(.n-input-number), .field :deep(.n-date-picker) { width: 100%; }
.field-label {
  color: var(--fg-dim);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0;
}
.create-button { min-width: 88px; }
.secret-alert { margin: 0 0 14px; }
.secret-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.secret-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0; padding: 20px; text-align: center; }
@media (max-width: 900px) {
  .create-form { grid-template-columns: minmax(180px, 1fr) 110px; }
  .expires-field { grid-column: 1; }
  .create-button { grid-column: 2; }
}
@media (max-width: 560px) {
  .create-form { grid-template-columns: 1fr; }
  .expires-field, .create-button { grid-column: auto; }
  .create-button { width: 100%; }
}
</style>
