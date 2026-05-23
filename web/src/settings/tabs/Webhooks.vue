<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NList,
  NListItem,
  NThing,
  NInput,
  NSelect,
  NButton,
  NSpace,
  NCheckbox,
} from 'naive-ui'
import { listWebhooks, createWebhook, deleteWebhook } from '@shared/api/webhooks'
import type { WebhookRow } from '@shared/api/types'

const webhooks = ref<WebhookRow[]>([])
const loading = ref(true)

const newName = ref('')
const newUrl = ref('')
const newFormat = ref<'feishu' | 'generic'>('generic')
const newInsecure = ref(false)
const saving = ref(false)
const errorMsg = ref('')

const formatOptions = [
  { label: 'Generic', value: 'generic' },
  { label: 'Feishu', value: 'feishu' },
]

const nameInputProps = { 'data-testid': 'wh-name', autocomplete: 'off' } as Record<string, unknown>
const urlInputProps = { 'data-testid': 'wh-url', autocomplete: 'off' } as Record<string, unknown>

function shortDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

async function reload() {
  loading.value = true
  try {
    webhooks.value = await listWebhooks()
  } finally {
    loading.value = false
  }
}

async function onAdd() {
  const name = newName.value.trim()
  const url = newUrl.value.trim()
  if (!name || !url || saving.value) return
  saving.value = true
  errorMsg.value = ''
  try {
    await createWebhook({ name, url, format: newFormat.value, allow_insecure: newInsecure.value })
    newName.value = ''
    newUrl.value = ''
    newFormat.value = 'generic'
    newInsecure.value = false
    await reload()
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : 'Failed to create webhook.'
  } finally {
    saving.value = false
  }
}

async function onDelete(id: string) {
  try {
    await deleteWebhook(id)
    await reload()
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : 'Failed to delete webhook.'
  }
}

onMounted(reload)
</script>

<template>
  <n-card :bordered="false">
    <n-list v-if="webhooks.length > 0" bordered>
      <n-list-item v-for="wh in webhooks" :key="wh.id">
        <n-thing>
          <template #header>{{ wh.name }}</template>
          <template #description>
            <span>{{ wh.url }}</span> · {{ wh.format }} · created {{ shortDate(wh.created_at) }}
          </template>
        </n-thing>
        <template #suffix>
          <n-button
            size="small"
            type="error"
            :data-testid="`wh-del-${wh.id}`"
            @click="onDelete(wh.id)"
          >
            Delete
          </n-button>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">No webhooks yet.</p>

    <div class="add-form">
      <n-space vertical>
        <n-space :wrap="false">
          <n-input
            v-model:value="newName"
            type="text"
            placeholder="Name"
            :input-props="nameInputProps"
          />
          <n-select
            v-model:value="newFormat"
            :options="formatOptions"
            style="min-width: 120px"
            data-testid="wh-format"
          />
        </n-space>
        <n-input
          v-model:value="newUrl"
          type="text"
          placeholder="https://..."
          :input-props="urlInputProps"
        />
        <n-space align="center">
          <n-checkbox v-model:checked="newInsecure" data-testid="wh-insecure">
            Allow plain http:// (no TLS)
          </n-checkbox>
          <n-button
            type="primary"
            :loading="saving"
            :disabled="saving"
            data-testid="wh-add"
            @click="onAdd"
          >
            Add webhook
          </n-button>
        </n-space>
        <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
      </n-space>
    </div>
  </n-card>
</template>

<style scoped>
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.add-form { margin-top: 1rem; }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0; }
</style>
