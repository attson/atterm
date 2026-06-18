<template>
  <div class="tab-pane">
    <section class="hook-install" data-test="hook-install">
      <header class="hook-install__row">
        <span class="hook-install__dot" :class="dotClass" :title="hookState.last_error || ''"></span>
        <span class="hook-install__label">{{ hookLabel }}</span>
        <label class="hook-install__toggle">
          <input type="checkbox" :checked="hookState.enabled" @change="onToggleHook" />
          <span>{{ t('settings.feishu.hook_install.enable') }}</span>
        </label>
        <button
          v-if="hookState.enabled && (!hookState.binary_ok || !hookState.settings_ok)"
          type="button"
          class="hook-install__retry"
          @click="onRetryHook"
          data-test="hook-install-retry"
        >
          {{ t('settings.feishu.hook_install.retry') }}
        </button>
      </header>
      <p v-if="hookState.enabled && hookState.last_error" class="hook-install__error">
        {{ hookState.last_error }}
      </p>
    </section>
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
import { ref, computed, onMounted } from 'vue'
import { useI18n } from '../i18n/useI18n'
import {
  getFeishuStatus,
  setFeishuCredentials,
  beginFeishuPair,
  deleteFeishuBinding,
  getHookInstallState,
  setHookInstallEnabled,
  type FeishuStatusResp,
  type FeishuCredentials,
  type HookInstallState,
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

const hookState = ref<HookInstallState>({
  enabled: true,
  binary_path: '',
  binary_ok: false,
  binary_version: '',
  settings_path: '',
  settings_ok: false,
  last_error: '',
  last_check: '',
})

async function refresh() {
  try {
    status.value = await getFeishuStatus()
  } catch (e) {
    // non-fatal; status stays disabled
  }
}

async function refreshHook() {
  try {
    hookState.value = await getHookInstallState()
  } catch (e) {
    // non-fatal; UI keeps the last known state.
  }
}

const dotClass = computed(() => {
  if (!hookState.value.enabled) return 'hook-install__dot--gray'
  if (hookState.value.binary_ok && hookState.value.settings_ok) return 'hook-install__dot--green'
  return 'hook-install__dot--amber'
})

const hookLabel = computed(() => {
  if (!hookState.value.enabled) return t('settings.feishu.hook_install.disabled')
  if (hookState.value.binary_ok && hookState.value.settings_ok) return t('settings.feishu.hook_install.healthy')
  return t('settings.feishu.hook_install.needs_attention')
})

async function onToggleHook(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  try {
    await setHookInstallEnabled(on)
  } finally {
    await refreshHook()
  }
}

async function onRetryHook() {
  try {
    await setHookInstallEnabled(true)
  } finally {
    await refreshHook()
  }
}

onMounted(async () => {
  await refresh()
  await refreshHook()
})

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
.hook-install {
  padding: 8px 0 12px;
  border-bottom: 1px solid var(--border-subtle, rgba(127, 127, 127, 0.2));
  margin-bottom: 12px;
}
.hook-install__row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.hook-install__dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.hook-install__dot--green { background: #2ea043; }
.hook-install__dot--amber { background: #d29922; }
.hook-install__dot--gray  { background: #6e7681; }
.hook-install__label { flex: 1; font-size: 13px; }
.hook-install__toggle { display: flex; gap: 4px; align-items: center; font-size: 12px; }
.hook-install__retry { font-size: 12px; padding: 2px 8px; }
.hook-install__error { font-size: 12px; color: #d29922; margin: 6px 0 0; }
</style>
