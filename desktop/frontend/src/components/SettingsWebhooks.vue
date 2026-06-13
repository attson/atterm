<script lang="ts" setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from '../i18n/useI18n'
import { usePlatform } from '../platform'
import SelectDropdown from './SelectDropdown.vue'
import type { Webhook, WebhookFormat } from '../platform/types'

const { t } = useI18n()
const platform = usePlatform()

const items = ref<Webhook[]>([])
const loading = ref(true)
const listError = ref('')

const newName = ref('')
const newURL = ref('')
const newFormat = ref<WebhookFormat>('generic')
const newAllowInsecure = ref(false)
const creating = ref(false)
const createError = ref('')

const confirmDeleteFor = ref<Webhook | null>(null)
const deleting = ref(false)
const deleteError = ref('')

const bridge = computed(() => platform.relay.webhooks)

const formatOptions = computed(() => [
  { value: 'generic', label: t('settings.webhooks.formats.generic'), description: '' },
  { value: 'feishu', label: t('settings.webhooks.formats.feishu'), description: '' },
])

function urlLooksValid(s: string): boolean {
  try {
    const u = new URL(s.trim())
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

const canCreate = computed(
  () =>
    !creating.value &&
    !!newName.value.trim() &&
    urlLooksValid(newURL.value),
)

async function refresh() {
  if (!bridge.value) {
    loading.value = false
    listError.value = t('settings.webhooks.errors.not_configured' as const)
    return
  }
  loading.value = true
  listError.value = ''
  try {
    const list = await bridge.value.list()
    items.value = list ?? []
  } catch (e) {
    listError.value = mapError(e, 'settings.webhooks.errors.network' as const)
  } finally {
    loading.value = false
  }
}

onMounted(refresh)

type WebhookErrorCode =
  | 'invalid_url'
  | 'invalid_format'
  | 'insecure_url'
  | 'name_required'
  | 'too_many'
  | 'unauthorized'
  | 'network'
  | 'not_configured'
  | 'not_found'
  | 'generic'

const KNOWN_ERROR_CODES: ReadonlySet<WebhookErrorCode> = new Set<WebhookErrorCode>([
  'invalid_url',
  'invalid_format',
  'insecure_url',
  'name_required',
  'too_many',
  'unauthorized',
  'network',
  'not_configured',
  'not_found',
])

type FallbackKey = 'settings.webhooks.errors.generic' | 'settings.webhooks.errors.network'

function normalizeErrorCode(raw: string): WebhookErrorCode | undefined {
  if (raw === 'relay_unauthorized') return 'unauthorized'
  if (raw === 'webhook_not_found') return 'not_found'
  if (raw === 'relay_not_configured') return 'not_configured'
  return (KNOWN_ERROR_CODES as ReadonlySet<string>).has(raw)
    ? (raw as WebhookErrorCode)
    : undefined
}

function mapError(e: unknown, fallback: FallbackKey): string {
  const raw = e instanceof Error ? e.message : String(e)
  const code = normalizeErrorCode(raw)
  if (!code) return t(fallback)
  switch (code) {
    case 'invalid_url': return t('settings.webhooks.errors.invalid_url')
    case 'invalid_format': return t('settings.webhooks.errors.invalid_format')
    case 'insecure_url': return t('settings.webhooks.errors.insecure_url')
    case 'name_required': return t('settings.webhooks.errors.name_required')
    case 'too_many': return t('settings.webhooks.errors.too_many')
    case 'unauthorized': return t('settings.webhooks.errors.unauthorized')
    case 'network': return t('settings.webhooks.errors.network')
    case 'not_configured': return t('settings.webhooks.errors.not_configured')
    case 'not_found': return t('settings.webhooks.errors.not_found')
    case 'generic': return t('settings.webhooks.errors.generic')
  }
}

async function onCreate() {
  if (!bridge.value || !canCreate.value) return
  createError.value = ''
  creating.value = true
  try {
    const row = await bridge.value.create({
      name: newName.value.trim(),
      url: newURL.value.trim(),
      format: newFormat.value,
      allow_insecure: newAllowInsecure.value,
    })
    items.value = [row, ...items.value]
    newName.value = ''
    newURL.value = ''
    newFormat.value = 'generic'
    newAllowInsecure.value = false
  } catch (e) {
    createError.value = mapError(e, 'settings.webhooks.errors.generic' as const)
  } finally {
    creating.value = false
  }
}

function askDelete(wh: Webhook) {
  confirmDeleteFor.value = wh
  deleteError.value = ''
}

function cancelDelete() {
  confirmDeleteFor.value = null
}

async function confirmDelete() {
  const wh = confirmDeleteFor.value
  if (!wh || !bridge.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await bridge.value.delete(wh.id)
    items.value = items.value.filter((x) => x.id !== wh.id)
    confirmDeleteFor.value = null
  } catch (e) {
    deleteError.value = mapError(e, 'settings.webhooks.errors.generic' as const)
  } finally {
    deleting.value = false
  }
}

function formatDate(s: string): string {
  try {
    const d = new Date(s)
    if (Number.isNaN(d.getTime())) return s
    return d.toLocaleString()
  } catch {
    return s
  }
}
</script>

<template>
  <div class="tab-pane">
    <p class="hint">{{ t('settings.webhooks.hint') }}</p>

    <section class="webhooks-list">
      <h3 class="section-title">{{ t('settings.webhooks.listTitle') }}</h3>

      <div v-if="loading" class="dim">{{ t('common.loading') }}</div>
      <div v-else-if="listError" class="error">{{ listError }}</div>
      <div v-else-if="items.length === 0" class="empty">{{ t('settings.webhooks.empty') }}</div>
      <ul v-else class="list">
        <li v-for="wh in items" :key="wh.id" class="row" :data-testid="`wh-row-${wh.id}`">
          <div class="row-main">
            <div class="row-name">{{ wh.name }}</div>
            <div class="row-meta">
              <span class="badge">{{
                wh.format === 'feishu'
                  ? t('settings.webhooks.formats.feishu')
                  : t('settings.webhooks.formats.generic')
              }}</span>
              <span class="row-url">{{ wh.url }}</span>
              <span class="row-date">{{ t('settings.webhooks.createdAt', { date: formatDate(wh.created_at) }) }}</span>
            </div>
          </div>
          <button
            type="button"
            class="btn-danger"
            :data-testid="`wh-delete-${wh.id}`"
            @click="askDelete(wh)"
          >
            {{ t('settings.webhooks.actions.delete') }}
          </button>
        </li>
      </ul>
    </section>

    <section v-if="bridge" class="webhooks-add">
      <h3 class="section-title">{{ t('settings.webhooks.addTitle') }}</h3>

      <label class="field-label" for="wh-name">{{ t('settings.webhooks.fields.name') }}</label>
      <input
        id="wh-name"
        v-model="newName"
        type="text"
        data-testid="wh-name"
        :placeholder="t('settings.webhooks.fields.namePlaceholder')"
        :disabled="creating"
      />

      <label class="field-label" for="wh-url">{{ t('settings.webhooks.fields.url') }}</label>
      <input
        id="wh-url"
        v-model="newURL"
        type="text"
        data-testid="wh-url"
        :placeholder="t('settings.webhooks.fields.urlPlaceholder')"
        :disabled="creating"
      />

      <label class="field-label">{{ t('settings.webhooks.fields.format') }}</label>
      <SelectDropdown
        v-model="newFormat"
        :options="formatOptions"
        :aria-label="t('settings.webhooks.fields.format')"
      />

      <label class="checkbox-row">
        <input
          v-model="newAllowInsecure"
          type="checkbox"
          data-testid="wh-allow-insecure"
          :disabled="creating"
        />
        <span>{{ t('settings.webhooks.fields.allowInsecure') }}</span>
      </label>

      <div v-if="createError" class="error">{{ createError }}</div>

      <button
        type="button"
        class="btn-primary"
        data-testid="wh-submit"
        :disabled="!canCreate"
        @click="onCreate"
      >
        {{ creating ? t('settings.webhooks.actions.creating') : t('settings.webhooks.actions.create') }}
      </button>
    </section>

    <div
      v-if="confirmDeleteFor"
      class="modal-backdrop"
      role="dialog"
      aria-modal="true"
      data-testid="wh-confirm-delete"
    >
      <div class="modal">
        <h4>{{ t('settings.webhooks.actions.confirmDeleteTitle') }}</h4>
        <p>{{ t('settings.webhooks.actions.confirmDeleteBody') }}</p>
        <p class="dim">{{ confirmDeleteFor.name }} — {{ confirmDeleteFor.url }}</p>
        <div v-if="deleteError" class="error">{{ deleteError }}</div>
        <div class="modal-actions">
          <button
            type="button"
            class="btn-ghost"
            :disabled="deleting"
            @click="cancelDelete"
          >
            {{ t('settings.webhooks.actions.cancel') }}
          </button>
          <button
            type="button"
            class="btn-danger"
            data-testid="wh-confirm-delete-button"
            :disabled="deleting"
            @click="confirmDelete"
          >
            {{ t('settings.webhooks.actions.delete') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.section-title {
  font-size: 0.9rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  opacity: 0.8;
  margin: 1rem 0 0.5rem;
}
.list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 1rem;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.02);
}
.row-name {
  font-weight: 600;
  margin-bottom: 0.25rem;
}
.row-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1rem;
  font-size: 0.85rem;
  opacity: 0.75;
}
.row-url {
  font-family: ui-monospace, monospace;
  word-break: break-all;
}
.row-date {
  opacity: 0.6;
}
.badge {
  display: inline-block;
  padding: 0 0.5rem;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.08);
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.empty {
  padding: 1rem;
  text-align: center;
  border: 1px dashed rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  opacity: 0.7;
}
.checkbox-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0.5rem 0;
}
.error {
  color: #f87171;
  margin: 0.5rem 0;
}
.btn-primary {
  margin-top: 0.5rem;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.modal {
  background: #1a1f2b;
  padding: 1.5rem;
  border-radius: 8px;
  max-width: 420px;
  width: 90vw;
  border: 1px solid rgba(255, 255, 255, 0.1);
}
.modal h4 {
  margin: 0 0 0.5rem;
}
.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  margin-top: 1rem;
}
</style>
