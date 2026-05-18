<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
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

const rows = ref<InvitationRow[]>([])
const loading = ref(true)
const submitting = ref(false)
const noteInput = ref('')
const countInput = ref<number>(1)
const expiresInput = ref<number | null>(null)
const newSecrets = ref<InvitationCreated[]>([])
const message = useMessage()

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

const columns: DataTableColumns<InvitationRow> = [
  { title: 'Prefix', key: 'code_prefix', render: (r) => h('code', {}, r.code_prefix) },
  { title: 'Note', key: 'note' },
  { title: 'Created', key: 'created_at', render: (r) => fmt(r.created_at) },
  { title: 'Expires', key: 'expires_at', render: (r) => fmt(r.expires_at) },
  {
    title: 'Consumed',
    key: 'consumed_at',
    render: (r) =>
      r.consumed_at
        ? h('span', {}, `${fmt(r.consumed_at)}${r.consumed_by ? ' · ' + r.consumed_by : ''}`)
        : h(NTag, { size: 'small', type: 'default' }, { default: () => 'unused' }),
  },
]

async function reload() {
  loading.value = true
  try {
    rows.value = await listInvitations()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load invitations.')
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
    if (err instanceof ApiError) message.error('Create failed: ' + err.code)
  } finally {
    submitting.value = false
  }
}

onMounted(reload)
</script>

<template>
  <n-card title="Invitations" :bordered="false">
    <form @submit="onCreate" autocomplete="off" class="create-form">
      <n-space :wrap="false" align="end">
        <div class="field">
          <label class="field-label">Note</label>
          <n-input
            v-model:value="noteInput"
            type="text"
            placeholder="optional"
            :input-props="noteInputProps"
          />
        </div>
        <div class="field">
          <label class="field-label">Count</label>
          <n-input-number
            v-model:value="countInput"
            :min="1"
            :max="50"
            :input-props="countInputProps"
          />
        </div>
        <div class="field">
          <label class="field-label">Expires</label>
          <n-date-picker
            v-model:value="expiresInput"
            type="datetime"
            clearable
            placeholder="optional"
            :input-props="expiresPickerProps"
          />
        </div>
        <n-button type="primary" attr-type="submit" :loading="submitting" :disabled="submitting">
          Create
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
        Copy this invitation now{{ s.note ? ` (${s.note})` : '' }} — it will not be shown again.
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
    <p v-if="!loading && rows.length === 0" class="empty">No invitations yet.</p>
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
