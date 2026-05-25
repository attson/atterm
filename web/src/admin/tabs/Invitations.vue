<script setup lang="ts">
import { computed, ref, onMounted, h } from 'vue'
import {
  NCard,
  NDataTable,
  NSpace,
  NInput,
  NInputNumber,
  NDatePicker,
  NButton,
  NAlert,
  NTag,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listInvitations, createInvitation } from '@shared/api/admin'
import type { InvitationCreated, InvitationRow } from '@shared/api/types'
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

// data-testid is not in Vue's stock InputHTMLAttributes, so cast through any
// for the test-only hooks. Keeps the markup identical at runtime.
const noteInputProps = { 'data-testid': 'invite-note', autocomplete: 'off' } as any
const countInputProps = { 'data-testid': 'invite-count' } as any
const expiresPickerProps = { 'data-testid': 'invite-expires' } as any

function fmt(iso: string | undefined): string {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
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
  <n-card :title="t('admin.invitations')" :bordered="false">
    <form @submit="onCreate" autocomplete="off" class="create-form">
      <n-space :wrap="false" align="end">
        <div class="field">
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
        <div class="field">
          <label class="field-label">{{ t('admin.invitationsTab.expires') }}</label>
          <n-date-picker
            v-model:value="expiresInput"
            type="datetime"
            clearable
            :placeholder="t('common.optional')"
            :input-props="expiresPickerProps"
          />
        </div>
        <n-button type="primary" attr-type="submit" :loading="submitting" :disabled="submitting">
          {{ t('common.create') }}
        </n-button>
      </n-space>
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

    <n-data-table
      :columns="columns"
      :data="rows"
      :loading="loading"
      size="small"
      :bordered="false"
      :pagination="false"
    />
    <p v-if="!loading && rows.length === 0" class="empty">{{ t('admin.invitationsTab.empty') }}</p>
  </n-card>
</template>

<style scoped>
.create-form { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.25rem; }
.field-label {
  font-size: 0.75rem;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.secret-alert { margin: 0.5rem 0; }
.secret-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.secret-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
