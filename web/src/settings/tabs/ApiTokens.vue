<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  NCard,
  NList,
  NListItem,
  NThing,
  NInput,
  NButton,
  NSpace,
  NAlert,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { listTokens, createToken, revokeToken } from '@shared/api/me'
import type { ApiTokenRow } from '@shared/api/types'

const tokens = ref<ApiTokenRow[]>([])
const newName = ref('')
const creating = ref(false)
const plaintext = ref('')
const loading = ref(true)
const message = useMessage()

function shortDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString()
  } catch {
    return iso
  }
}

function isActive(t: ApiTokenRow): boolean {
  return !t.revoked_at
}

async function reload() {
  loading.value = true
  try {
    tokens.value = await listTokens()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to load tokens.')
  } finally {
    loading.value = false
  }
}

async function onCreate(e: Event) {
  e.preventDefault()
  const name = newName.value.trim()
  if (!name || creating.value) return
  creating.value = true
  try {
    const created = await createToken(name)
    plaintext.value = created.plaintext
    newName.value = ''
    await reload()
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'name_required') message.error('Token name is required.')
      else if (e.code === 'invalid_request') message.error('Please enter a valid name.')
      else message.error('Failed to create token.')
    }
  } finally {
    creating.value = false
  }
}

async function onRevoke(id: string) {
  try {
    await revokeToken(id)
    await reload()
  } catch (e) {
    if (e instanceof ApiError) message.error('Failed to revoke token.')
  }
}

async function copyPlaintext() {
  try {
    await navigator.clipboard.writeText(plaintext.value)
    message.success('Token copied to clipboard.')
  } catch {
    message.warning('Clipboard not available — select and copy manually.')
  }
}

onMounted(reload)
</script>

<template>
  <n-card title="API Tokens" :bordered="false">
    <n-alert v-if="plaintext" type="success" :show-icon="false" class="plaintext-alert">
      <div class="plaintext-msg">Copy this token now — it will not be shown again.</div>
      <code class="plaintext-display">{{ plaintext }}</code>
      <n-button size="small" tertiary class="plaintext-copy" @click="copyPlaintext">Copy</n-button>
    </n-alert>

    <n-list v-if="tokens.filter(isActive).length > 0" bordered>
      <n-list-item v-for="t in tokens.filter(isActive)" :key="t.id">
        <n-thing>
          <template #header>{{ t.name }}</template>
          <template #description>
            <code>{{ t.prefix }}…</code> · created {{ shortDate(t.created_at) }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm @positive-click="onRevoke(t.id)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-${t.id}`">
                Revoke
              </n-button>
            </template>
            Revoke this token? This cannot be undone.
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">No tokens yet.</p>

    <form class="create-form" @submit="onCreate" autocomplete="off">
      <n-space :wrap="false">
        <n-input
          v-model:value="newName"
          type="text"
          placeholder="e.g. my-laptop"
          :input-props="{ required: true, autocomplete: 'off' }"
        />
        <n-button type="primary" attr-type="submit" :loading="creating" :disabled="creating">
          Create
        </n-button>
      </n-space>
    </form>
  </n-card>
</template>

<style scoped>
.plaintext-alert { margin-bottom: 1rem; }
.plaintext-msg { color: var(--fg-dim); font-size: 0.85rem; margin-bottom: 0.5rem; }
.plaintext-display {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  color: var(--good);
  word-break: break-all;
  display: block;
  padding: 0.25rem 0;
}
.plaintext-copy { margin-top: 0.5rem; }
.empty { color: var(--fg-dim); font-size: 0.875rem; margin: 0.5rem 0; }
.create-form { margin-top: 1rem; }
</style>
