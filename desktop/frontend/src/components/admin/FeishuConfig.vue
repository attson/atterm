<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NForm, NFormItem, NInput, NButton, NSwitch, useMessage } from 'naive-ui'
import { KeyRound, Save } from 'lucide-vue-next'
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
  <section class="admin-page feishu-page">
    <header class="admin-page-header">
      <div>
        <h1>{{ t('admin.feishuConfig.title') }}</h1>
        <p>{{ t('admin.feishuConfig.description') }}</p>
      </div>
      <span v-if="cfg" class="status-badge" :class="{ running: cfg.running }">
        <i aria-hidden="true"></i>
        {{ cfg.running ? t('admin.feishuConfig.statusRunning') : t('admin.feishuConfig.statusStopped') }}
      </span>
    </header>

    <n-form v-if="cfg" class="admin-surface feishu-form" label-placement="top">
      <div class="enable-row">
        <strong>{{ t('admin.feishuConfig.enabled') }}</strong>
        <n-switch v-model:value="enabled" :aria-label="t('admin.feishuConfig.enabled')" data-testid="feishu-enabled" />
      </div>

      <div class="feishu-fields">
        <n-form-item class="key-field" :label="t('admin.feishuConfig.encryptKey')" :show-feedback="false">
          <div class="stack-field">
            <div class="key-control">
              <n-input
                v-model:value="newKey"
                type="password"
                show-password-on="click"
                :placeholder="cfg.key_set ? t('admin.feishuConfig.keyKeepPlaceholder', { last4: cfg.key_last4 ?? '' }) : t('admin.feishuConfig.keyEmptyPlaceholder')"
                data-testid="feishu-key"
              />
              <n-button tertiary data-testid="feishu-generate" @click="onGenerate">
                <template #icon><KeyRound :size="15" /></template>
                {{ t('admin.feishuConfig.generate') }}
              </n-button>
            </div>
            <span v-if="newKey" class="warn">{{ t('admin.feishuConfig.keyCopyOnce') }}</span>
            <div v-if="newKey && cfg.key_set" class="force-row">
              <n-switch v-model:value="force" size="small" :aria-label="t('admin.feishuConfig.forceRotate')" data-testid="feishu-force" />
              <span class="muted">{{ t('admin.feishuConfig.forceRotate') }}</span>
            </div>
          </div>
        </n-form-item>

        <n-form-item class="base-field" :label="t('admin.feishuConfig.baseUrl')" :show-feedback="false">
          <n-input v-model:value="baseUrl" placeholder="https://open.feishu.cn" data-testid="feishu-base" />
        </n-form-item>
      </div>

      <div class="form-footer">
        <n-button type="primary" :loading="saving" :disabled="saving" data-testid="feishu-save" @click="onSave">
          <template #icon><Save :size="15" /></template>
          {{ t('common.save') }}
        </n-button>
      </div>
    </n-form>
    <p v-if="errorMsg" class="form-error" role="alert">{{ errorMsg }}</p>
  </section>
</template>

<style scoped>
.status-badge {
  min-height: 28px;
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--fg-dim);
  background: var(--panel);
  font-size: 11px;
  white-space: nowrap;
}
.status-badge i { width: 6px; height: 6px; border-radius: 50%; background: var(--fg-dim); }
.status-badge.running { color: var(--good); border-color: color-mix(in srgb, var(--good) 35%, var(--border)); }
.status-badge.running i { background: var(--good); }
.feishu-form { overflow: hidden; }
.enable-row {
  min-height: 64px;
  padding: 0 18px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
}
.enable-row strong { font-size: 13px; font-weight: 600; }
.feishu-fields {
  display: grid;
  grid-template-columns: minmax(320px, 1.4fr) minmax(240px, .8fr);
  gap: 24px;
  padding: 18px;
}
.stack-field { width: 100%; display: flex; flex-direction: column; gap: 7px; }
.key-control { display: flex; align-items: center; gap: 10px; }
.key-control :deep(.n-input) { flex: 1 1 auto; width: auto; min-width: 0; }
.force-row { display: flex; align-items: center; gap: 8px; }
.muted { color: var(--fg-dim); font-size: 0.875rem; }
.warn { color: var(--warn, #d89614); font-size: 0.8125rem; }
.form-footer {
  min-height: 62px;
  padding: 12px 18px;
  border-top: 1px solid var(--border);
  display: flex;
  align-items: center;
}
.form-error { color: var(--bad); font-size: 0.875rem; margin: 0.5rem 0 0; }
@media (max-width: 760px) {
  .feishu-fields { grid-template-columns: 1fr; gap: 4px; }
}
@media (max-width: 520px) {
  .key-control { align-items: stretch; flex-direction: column; }
  .key-control :deep(.n-button) { width: 100%; }
}
</style>
