<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NCard, NForm, NFormItem, NInput, NButton, NSpace, NSwitch, NAlert, useMessage } from 'naive-ui'
import { ApiError } from '@shared/api/client'
import { getFeishuAdminConfig, setFeishuAdminConfig, generateFeishuKey } from '@shared/api/admin'
import type { FeishuAdminConfig, FeishuAdminConfigUpdate } from '@shared/api/types'
import { useI18n } from '@shared/i18n/useI18n'

const cfg = ref<FeishuAdminConfig | null>(null)
const enabled = ref(false)
const baseUrl = ref('')
// newKey holds a freshly generated/typed key. Empty = keep the existing one.
const newKey = ref('')
const force = ref(false)
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const message = useMessage()
const { t } = useI18n()

async function load() {
  loading.value = true
  errorMsg.value = ''
  try {
    const c = await getFeishuAdminConfig()
    applyState(c)
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('admin.feishuConfig.loadFailed')
  } finally {
    loading.value = false
  }
}

function applyState(c: FeishuAdminConfig) {
  cfg.value = c
  enabled.value = c.enabled
  baseUrl.value = c.base_url
  newKey.value = ''
  force.value = false
}

async function onGenerate() {
  errorMsg.value = ''
  try {
    newKey.value = await generateFeishuKey()
    message.info(t('admin.feishuConfig.keyGenerated'))
  } catch (e) {
    if (e instanceof ApiError) errorMsg.value = t('admin.feishuConfig.saveFailed')
  }
}

async function onSave() {
  if (saving.value) return
  errorMsg.value = ''
  saving.value = true
  try {
    // Build the payload conditionally — exactOptionalPropertyTypes forbids
    // assigning explicit `undefined` to optional fields.
    const update: FeishuAdminConfigUpdate = { enabled: enabled.value }
    const k = newKey.value.trim()
    if (k) update.encrypt_key = k
    const b = baseUrl.value.trim()
    if (b) update.base_url = b
    if (force.value) update.force = true
    const updated = await setFeishuAdminConfig(update)
    applyState(updated)
    message.success(t('setup.saved'))
  } catch (e) {
    if (e instanceof ApiError) {
      // 409 = rotating an existing key without force.
      errorMsg.value = e.status === 409
        ? t('admin.feishuConfig.rotateConflict')
        : t('admin.feishuConfig.saveFailed')
    }
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <n-card :title="t('admin.feishuConfig.title')" :bordered="false">
    <p class="hint">{{ t('admin.feishuConfig.description') }}</p>
    <n-form v-if="cfg" label-placement="top">
      <n-form-item :label="t('admin.feishuConfig.enabled')" :show-feedback="false">
        <n-switch v-model:value="enabled" data-testid="feishu-enabled" />
      </n-form-item>

      <n-form-item :label="t('admin.feishuConfig.encryptKey')" :show-feedback="false">
        <n-space vertical :size="6" style="width: 100%">
          <n-space :wrap="false" align="center" style="width: 100%">
            <n-input
              v-model:value="newKey"
              type="password"
              show-password-on="click"
              :placeholder="cfg.key_set ? t('admin.feishuConfig.keyKeepPlaceholder', { last4: cfg.key_last4 ?? '' }) : t('admin.feishuConfig.keyEmptyPlaceholder')"
              data-testid="feishu-key"
            />
            <n-button tertiary data-testid="feishu-generate" @click="onGenerate">
              {{ t('admin.feishuConfig.generate') }}
            </n-button>
          </n-space>
          <span v-if="newKey" class="warn">{{ t('admin.feishuConfig.keyCopyOnce') }}</span>
          <n-space v-if="newKey && cfg.key_set" align="center" :size="8">
            <n-switch v-model:value="force" size="small" data-testid="feishu-force" />
            <span class="muted">{{ t('admin.feishuConfig.forceRotate') }}</span>
          </n-space>
        </n-space>
      </n-form-item>

      <n-form-item :label="t('admin.feishuConfig.baseUrl')" :show-feedback="false">
        <n-input v-model:value="baseUrl" placeholder="https://open.feishu.cn" data-testid="feishu-base" />
      </n-form-item>

      <n-alert v-if="cfg.requires_restart_for_vapid && cfg.vapid_subject" type="info" :show-icon="false" class="note">
        {{ t('admin.feishuConfig.vapidNote') }}
      </n-alert>

      <n-space class="actions" align="center">
        <n-button type="primary" :loading="saving" :disabled="saving" data-testid="feishu-save" @click="onSave">
          {{ t('common.save') }}
        </n-button>
        <span class="muted">{{ cfg.running ? t('admin.feishuConfig.statusRunning') : t('admin.feishuConfig.statusStopped') }}</span>
      </n-space>
    </n-form>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </n-card>
</template>

<style scoped>
.hint { color: var(--fg-dim); font-size: 0.875rem; margin: 0 0 1rem; }
.muted { color: var(--fg-dim); font-size: 0.875rem; }
.warn { color: var(--warn, #d89614); font-size: 0.8125rem; }
.note { margin: 0.75rem 0; }
.actions { margin-top: 1rem; }
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
</style>
