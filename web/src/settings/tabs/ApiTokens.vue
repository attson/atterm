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
import { formatShortDate } from '@shared/i18n'
import { useI18n } from '@shared/i18n/useI18n'

const tokens = ref<ApiTokenRow[]>([])
const newName = ref('')
const creating = ref(false)
const plaintext = ref('')
const loading = ref(true)
const message = useMessage()
const { t } = useI18n()

function isActive(token: ApiTokenRow): boolean {
  return !token.revoked_at
}

async function reload() {
  loading.value = true
  try {
    tokens.value = await listTokens()
  } catch (e) {
    if (e instanceof ApiError) message.error(t('settings.apiTokens.loadFailed'))
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
      if (e.code === 'name_required') message.error(t('settings.apiTokens.nameRequired'))
      else if (e.code === 'invalid_request') message.error(t('settings.apiTokens.invalidName'))
      else message.error(t('settings.apiTokens.createFailed'))
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
    if (e instanceof ApiError) message.error(t('settings.apiTokens.revokeFailed'))
  }
}

async function copyPlaintext() {
  try {
    await navigator.clipboard.writeText(plaintext.value)
    message.success(t('settings.apiTokens.copied'))
  } catch {
    message.warning(t('settings.apiTokens.copyManually'))
  }
}

onMounted(reload)
</script>

<template>
  <n-card :bordered="false">
    <n-alert v-if="plaintext" type="success" :show-icon="false" class="plaintext-alert">
      <div class="plaintext-msg">{{ t('settings.apiTokens.copyNow') }}</div>
      <code class="plaintext-display">{{ plaintext }}</code>
      <n-button size="small" tertiary class="plaintext-copy" @click="copyPlaintext">{{ t('common.copy') }}</n-button>
    </n-alert>

    <n-list v-if="tokens.filter(isActive).length > 0" bordered>
      <n-list-item v-for="token in tokens.filter(isActive)" :key="token.id">
        <n-thing>
          <template #header>{{ token.name }}</template>
          <template #description>
            <code>{{ token.prefix }}…</code> · {{ t('common.created') }} {{ formatShortDate(token.created_at) }}
          </template>
        </n-thing>
        <template #suffix>
          <n-popconfirm @positive-click="onRevoke(token.id)">
            <template #trigger>
              <n-button size="small" type="error" :data-testid="`revoke-${token.id}`">
                {{ t('common.revoke') }}
              </n-button>
            </template>
            {{ t('settings.apiTokens.revokeConfirm') }}
          </n-popconfirm>
        </template>
      </n-list-item>
    </n-list>
    <p v-else-if="!loading" class="empty">{{ t('settings.apiTokens.empty') }}</p>

    <form class="create-form" @submit="onCreate" autocomplete="off">
      <n-space :wrap="false">
        <n-input
          v-model:value="newName"
          type="text"
          :placeholder="t('settings.apiTokens.namePlaceholder')"
          :input-props="{ required: true, autocomplete: 'off' }"
        />
        <n-button type="primary" attr-type="submit" :loading="creating" :disabled="creating">
          {{ t('common.create') }}
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
