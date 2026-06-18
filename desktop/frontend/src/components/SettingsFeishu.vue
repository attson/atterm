<template>
  <div class="tab-pane">
    <p v-if="!status.enabled" class="hint">{{ t('settings.feishu.disabled') }}</p>
    <template v-else>
      <p class="hint">{{ t('settings.feishu.mode') }}: {{ status.mode }}</p>
      <template v-if="status.bound">
        <p>{{ t('settings.feishu.bound', { open_id: status.open_id }) }}</p>
        <button type="button" class="btn-danger" @click="onDelete">{{ t('settings.feishu.delete') }}</button>
      </template>
      <fieldset v-else class="feishu-fieldset">
        <label class="field-label">App ID
          <input v-model="creds.AppID" type="text" class="field-input" />
        </label>
        <label class="field-label">App Secret
          <input v-model="creds.AppSecret" type="password" class="field-input" />
        </label>
        <label class="field-label">Encrypt Key
          <input v-model="creds.EncryptKey" type="password" class="field-input" />
        </label>
        <label class="field-label">Verify Token
          <input v-model="creds.VerifyToken" type="password" class="field-input" />
        </label>
        <div v-if="saveError" class="error">{{ saveError }}</div>
        <div class="actions">
          <button type="button" class="btn-primary" @click="onSave">{{ t('settings.feishu.save') }}</button>
          <button type="button" class="btn-primary" :disabled="!saved" @click="onPair">{{ t('settings.feishu.begin_pair') }}</button>
        </div>
        <p v-if="pairCode" class="pair-hint">{{ t('settings.feishu.pair_hint', { code: pairCode }) }}</p>
      </fieldset>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from '../i18n/useI18n'
import {
  getFeishuStatus,
  setFeishuCredentials,
  beginFeishuPair,
  deleteFeishuBinding,
  type FeishuStatusResp,
  type FeishuCredentials,
} from '../lib/api'

const { t } = useI18n()

const status = ref<FeishuStatusResp>({
  enabled: false,
  mode: 'local',
  bound: false,
  open_id: '',
  disabled: false,
})
const creds = ref<FeishuCredentials>({
  AppID: '',
  AppSecret: '',
  EncryptKey: '',
  VerifyToken: '',
})
const pairCode = ref('')
const saved = ref(false)
const saveError = ref('')

async function refresh() {
  try {
    status.value = await getFeishuStatus()
  } catch (e) {
    // non-fatal; status stays disabled
  }
}

onMounted(refresh)

async function onSave() {
  saveError.value = ''
  try {
    await setFeishuCredentials(creds.value)
    saved.value = true
    await refresh()
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}

async function onPair() {
  try {
    pairCode.value = await beginFeishuPair()
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}

async function onDelete() {
  try {
    await deleteFeishuBinding()
    pairCode.value = ''
    saved.value = false
    await refresh()
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<style scoped>
.feishu-fieldset {
  border: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.field-label {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  font-size: 13px;
  color: var(--fg-dim);
}
.field-input {
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
}
.actions {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.25rem;
}
.error {
  color: #f87171;
  font-size: 13px;
}
.pair-hint {
  font-size: 13px;
  color: var(--fg-dim);
  margin-top: 0.5rem;
}
.hint {
  font-size: 13px;
  color: var(--fg-dim);
  margin: 0 0 1rem;
}
</style>
