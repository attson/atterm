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
    <section class="ai-only" data-test="feishu-ai-only">
      <label class="ai-only__toggle">
        <input type="checkbox" :checked="aiOnlyNotifications" @change="onToggleAIOnly" />
        <span>{{ t('settings.feishu.aiOnlyNotifications') }}</span>
      </label>
    </section>
    <section class="remote-terminal" data-test="feishu-remote-terminal">
      <label class="remote-terminal__toggle">
        <input type="checkbox" :checked="remoteTerminalEnabled" @change="onRemoteTerminalToggleChange" />
        <span>{{ t('settings.feishu.remoteTerminal.enable') }}</span>
      </label>
      <div v-if="remoteTerminalEnabled" class="remote-terminal__attach-label">
        <span>{{ t('settings.feishu.remoteTerminal.autoAttachLabel') }}</span>
        <SelectDropdown
          class="remote-terminal__attach-select"
          :model-value="sessionAutoAttach"
          :options="autoAttachOptions"
          :aria-label="t('settings.feishu.remoteTerminal.autoAttachLabel')"
          data-test="feishu-auto-attach"
          @update:model-value="onAutoAttachChange"
        />
      </div>
    </section>
    <div v-if="status.error" class="hint feishu-status-error" data-test="feishu-load-error">
      <span>{{ t('settings.feishu.load_error', { error: status.error }) }}</span>
      <button type="button" class="hook-install__retry" @click="refresh" data-test="feishu-status-retry">
        {{ t('settings.feishu.retry_status') }}
      </button>
    </div>
    <p v-else-if="status.relay_disabled" class="hint" data-test="feishu-relay-disabled">
      {{ t('settings.feishu.relay_disabled') }}
    </p>
    <p v-else-if="!status.enabled" class="hint">{{ t('settings.feishu.disabled') }}</p>
    <template v-else>
      <div class="feishu-mode">
        <div class="feishu-mode__label">
          <span class="feishu-mode__label-text">{{ t('settings.feishu.mode.label') }}</span>
          <SelectDropdown
            class="feishu-mode__select"
            :model-value="feishuModePref"
            :options="feishuModeOptions"
            :aria-label="t('settings.feishu.mode.label')"
            @update:model-value="onFeishuModeChange"
          />
        </div>
        <p class="feishu-mode__effective">
          <span>{{ t('settings.feishu.mode.effective.label') }}:</span>
          <strong>{{ feishuEffectiveMode || '—' }}</strong>
          <span
            v-if="feishuModePref === 'relay' && feishuEffectiveMode === 'local'"
            class="feishu-mode__warn"
          >
            ⚠ {{ t('settings.feishu.mode.effective.fallbackWarn') }}
          </span>
        </p>
      </div>
      <template v-if="status.bound">
        <p>{{ t('settings.feishu.bound', { open_id: status.open_id }) }}</p>
        <section class="test-send" data-test="feishu-test-send">
          <p class="test-send__title">{{ t('settings.feishu.test_send.title') }}</p>
          <p class="hint">{{ t('settings.feishu.test_send.hint') }}</p>
          <div class="test-send__buttons">
            <button
              v-for="s in testScenarios"
              :key="s"
              type="button"
              class="btn-secondary"
              :disabled="testSending !== ''"
              @click="onTestSend(s)"
              :data-test="'feishu-test-' + s"
            >
              {{ testSending === s ? t('settings.feishu.test_send.sending') : t(`settings.feishu.test_send.${s}`) }}
            </button>
          </div>
          <p v-if="testResult.ok" class="test-send__ok" data-test="feishu-test-ok">{{ t('settings.feishu.test_send.sent') }}</p>
          <p v-else-if="testResult.error" class="error" data-test="feishu-test-error">
            {{ t('settings.feishu.test_send.failed', { error: testResult.error }) }}
          </p>
        </section>
        <button type="button" class="btn-danger" @click="onDelete">{{ t('settings.feishu.delete') }}</button>
      </template>
      <!-- Credentials stored but not yet bound. Show a "configured" view so a
           reopened dialog reflects the saved state (secrets are never echoed
           back, so the form would otherwise look blank) and let the user pair
           or re-enter credentials. -->
      <div v-else-if="status.configured && !editing" class="feishu-configured" data-test="feishu-configured">
        <p class="configured-line">
          <span class="configured-badge">{{ t('settings.feishu.configured') }}</span>
          <span v-if="status.app_id" class="configured-appid">{{ t('settings.feishu.app_id_label') }}: {{ status.app_id }}</span>
        </p>
        <template v-if="status.callback_url">
          <label class="field-label">{{ t('settings.feishu.callback_label') }}
            <span class="callback-row">
              <input :value="status.callback_url" readonly class="field-input" data-test="feishu-callback-url" />
              <button type="button" class="btn-primary" @click="onCopyCallback" data-test="feishu-callback-copy">
                {{ copied ? t('settings.feishu.copied') : t('settings.feishu.copy') }}
              </button>
            </span>
          </label>
          <p class="hint">{{ t('settings.feishu.callback_hint_relay') }}</p>
        </template>
        <p v-else class="hint" data-test="feishu-longconn-hint">{{ t('settings.feishu.longconn_hint_local') }}</p>
        <div v-if="saveError" class="error">{{ saveError }}</div>
        <div class="actions">
          <button type="button" class="btn-primary" @click="onPair" data-test="feishu-begin-pair">{{ t('settings.feishu.begin_pair') }}</button>
          <button type="button" class="btn-secondary" @click="onReconfigure">{{ t('settings.feishu.reconfigure') }}</button>
        </div>
        <p v-if="pairCode" class="pair-hint">{{ t('settings.feishu.pair_hint', { code: pairCode }) }}</p>
      </div>
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
        </div>
      </fieldset>
    </template>
  </div>
</template>

<script setup lang="ts">
import { errText, logError } from "../lib/log";
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from '../i18n/useI18n'
import SelectDropdown, { type SelectOption } from './SelectDropdown.vue'
import {
  getFeishuStatus,
  setFeishuCredentials,
  beginFeishuPair,
  deleteFeishuBinding,
  sendFeishuTestCard,
  getHookInstallState,
  setHookInstallEnabled,
  getAINotificationsOnly,
  setAINotificationsOnly,
  getFeishuRemoteTerminalSettings,
  setFeishuRemoteTerminalSettings,
  getFeishuModePref,
  setFeishuModePref,
  getFeishuEffectiveMode,
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
const saveError = ref('')
// "Notify for AI sessions only" toggle. Defaults to true (on) so the UI matches
// the backend default before onMounted reads the persisted value.
const aiOnlyNotifications = ref(true)

// Feishu mode preference + effective mode display.
const feishuModePref = ref<'auto' | 'local' | 'relay'>('auto')
const feishuEffectiveMode = ref('')

const feishuModeOptions = computed<SelectOption[]>(() => [
  { value: 'auto', label: t('settings.feishu.mode.options.auto') },
  { value: 'local', label: t('settings.feishu.mode.options.local') },
  { value: 'relay', label: t('settings.feishu.mode.options.relay') },
])

async function onToggleAIOnly(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  try {
    await setAINotificationsOnly(on)
    aiOnlyNotifications.value = on
  } catch {
    // Persist failed: re-read so the checkbox reflects the actual backend state.
    try {
      aiOnlyNotifications.value = await getAINotificationsOnly()
    } catch {
      /* leave the optimistic value */
    }
  }
}

async function onFeishuModeChange(next: string) {
  try {
    await setFeishuModePref(next)
    feishuModePref.value = next as 'auto' | 'local' | 'relay'
    // Reconcile may have swapped the running mode synchronously; refresh.
    feishuEffectiveMode.value = await getFeishuEffectiveMode()
  } catch (err) {
    // Rollback the dropdown to the persisted value on failure.
    feishuModePref.value = (await getFeishuModePref()) as 'auto' | 'local' | 'relay'
    logError("feishu", "SetFeishuModePref failed", { error: errText(err) })
  }
}

// Remote terminal toggle + autoAttach dropdown. Default to off/"ai" so the UI
// matches the backend default before onMounted reads the persisted value.
const remoteTerminalEnabled = ref(false)
const sessionAutoAttach = ref('ai')

const autoAttachOptions = computed<SelectOption[]>(() => [
  { value: 'ai', label: t('settings.feishu.remoteTerminal.autoAttach.ai') },
  { value: 'all', label: t('settings.feishu.remoteTerminal.autoAttach.all') },
  { value: 'none', label: t('settings.feishu.remoteTerminal.autoAttach.none') },
])

async function onRemoteTerminalToggleChange(e: Event) {
  const on = (e.target as HTMLInputElement).checked
  try {
    await setFeishuRemoteTerminalSettings(on, sessionAutoAttach.value)
    remoteTerminalEnabled.value = on
  } catch {
    // Persist failed: re-read so the checkbox reflects the actual backend state.
    try {
      const s = await getFeishuRemoteTerminalSettings()
      remoteTerminalEnabled.value = s.enabled
      sessionAutoAttach.value = s.auto_attach
    } catch {
      /* leave the optimistic value */
    }
  }
}

async function onAutoAttachChange(mode: string) {
  try {
    await setFeishuRemoteTerminalSettings(remoteTerminalEnabled.value, mode)
    sessionAutoAttach.value = mode
  } catch {
    try {
      const s = await getFeishuRemoteTerminalSettings()
      remoteTerminalEnabled.value = s.enabled
      sessionAutoAttach.value = s.auto_attach
    } catch {
      /* leave the optimistic value */
    }
  }
}
// editing forces the credentials form even when status.configured, so the user
// can overwrite stored credentials from the "configured" view.
const editing = ref(false)
const copied = ref(false)

// Test-send state. testScenarios mirrors the feishu.TestCard* backend values
// and the settings.feishu.test_send.<scenario> i18n keys. testSending holds the
// scenario currently in flight ('' = idle) so its button shows a spinner label
// and all buttons disable. testResult shows the last outcome.
const testScenarios = ['command_success', 'command_failure', 'command_sealed', 'waiting_input'] as const
const testSending = ref('')
const testResult = ref<{ ok: boolean; error: string }>({ ok: false, error: '' })

async function onTestSend(scenario: string) {
  testSending.value = scenario
  testResult.value = { ok: false, error: '' }
  try {
    await sendFeishuTestCard(scenario)
    testResult.value = { ok: true, error: '' }
  } catch (e) {
    testResult.value = { ok: false, error: e instanceof Error ? e.message : String(e) }
  } finally {
    testSending.value = ''
  }
}

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
    // The Wails call itself failed (rare — the backend normally encodes
    // failures in resp.error). Surface it instead of silently showing
    // "disabled", which conflates "off" with "couldn't load".
    status.value = {
      enabled: false,
      mode: status.value.mode,
      bound: false,
      open_id: '',
      disabled: false,
      error: e instanceof Error ? e.message : String(e),
    }
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
  try {
    aiOnlyNotifications.value = await getAINotificationsOnly()
  } catch {
    // non-fatal; keep the default-on value.
  }
  try {
    const rts = await getFeishuRemoteTerminalSettings()
    remoteTerminalEnabled.value = rts.enabled
    sessionAutoAttach.value = rts.auto_attach
  } catch {
    // non-fatal; keep the defaults (false / "ai").
  }
  try {
    feishuModePref.value = (await getFeishuModePref()) as 'auto' | 'local' | 'relay'
    feishuEffectiveMode.value = await getFeishuEffectiveMode()
  } catch {
    // non-fatal; keep the defaults ('auto' / '').
  }
})

async function onSave() {
  saveError.value = ''
  try {
    await setFeishuCredentials(creds.value)
    editing.value = false
    // refresh() flips status.configured → the configured view takes over.
    await refresh()
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}

function onReconfigure() {
  saveError.value = ''
  editing.value = true
}

async function onCopyCallback() {
  if (!status.value.callback_url) return
  try {
    await navigator.clipboard.writeText(status.value.callback_url)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    // Clipboard unavailable (non-secure context): leave the readonly input so
    // the user can select-and-copy manually.
  }
}

// Binding completes out-of-band: the user sends `/bind <code>` to the bot and
// the relay writes open_id server-side. The dialog has no other signal, so
// after issuing a pair code we poll the status until it flips to bound (or the
// 15-min code window lapses), then the bound view takes over automatically.
const PAIR_POLL_INTERVAL_MS = 2500
const PAIR_POLL_MAX_MS = 15 * 60 * 1000
let pairPollTimer: ReturnType<typeof setInterval> | null = null

function stopPairPolling() {
  if (pairPollTimer) {
    clearInterval(pairPollTimer)
    pairPollTimer = null
  }
}

function startPairPolling() {
  stopPairPolling()
  const startedAt = Date.now()
  pairPollTimer = setInterval(async () => {
    await refresh()
    if (status.value.bound) {
      pairCode.value = ''
      stopPairPolling()
      return
    }
    if (Date.now() - startedAt >= PAIR_POLL_MAX_MS) {
      stopPairPolling()
    }
  }, PAIR_POLL_INTERVAL_MS)
}

onBeforeUnmount(stopPairPolling)

async function onPair() {
  try {
    pairCode.value = await beginFeishuPair()
    startPairPolling()
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}

async function onDelete() {
  try {
    stopPairPolling()
    await deleteFeishuBinding()
    pairCode.value = ''
    editing.value = false
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
.feishu-configured {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.configured-line {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin: 0;
  font-size: 13px;
}
.configured-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--good);
  color: var(--bg);
  font-size: 12px;
}
.configured-appid {
  color: var(--fg-dim);
  font-family: var(--mono, monospace);
}
.callback-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}
.callback-row .field-input {
  flex: 1;
  font-family: var(--mono, monospace);
}
.btn-secondary {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
.error {
  color: #f87171;
  font-size: 13px;
}
.test-send {
  border-top: 1px solid var(--border);
  border-bottom: 1px solid var(--border);
  padding: 12px 0;
  margin: 4px 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.test-send__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
  margin: 0;
}
.test-send .hint {
  margin: 0;
}
.test-send__buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 2px;
}
.test-send__buttons .btn-secondary:disabled {
  opacity: 0.5;
  cursor: default;
}
.test-send__ok {
  color: var(--good);
  font-size: 13px;
  margin: 0;
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
.feishu-status-error {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--warn);
}
.hook-install {
  padding: 8px 0 12px;
  border-bottom: 1px solid var(--border);
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
.hook-install__dot--green { background: var(--good); }
.hook-install__dot--amber { background: var(--warn); }
.hook-install__dot--gray  { background: var(--neutral); }
.hook-install__label { flex: 1; font-size: 13px; }
.hook-install__toggle { display: flex; gap: 4px; align-items: center; font-size: 12px; }
.hook-install__retry { font-size: 12px; padding: 2px 8px; }
.hook-install__error { font-size: 12px; color: var(--warn); margin: 6px 0 0; }
.ai-only {
  padding: 0 0 12px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 12px;
}
.ai-only__toggle { display: flex; gap: 6px; align-items: center; font-size: 13px; }
.remote-terminal {
  padding: 0 0 12px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.remote-terminal__toggle { display: flex; gap: 6px; align-items: center; font-size: 13px; }
.remote-terminal__attach-label {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 13px;
  color: var(--fg-dim);
  padding-left: 22px;
}
.remote-terminal__attach-select {
  width: 220px;
}
.feishu-mode { margin: 8px 0; }
.feishu-mode__label { display: flex; gap: 8px; align-items: center; font-size: 13px; color: var(--fg-dim); }
.feishu-mode__label-text { flex-shrink: 0; }
.feishu-mode__select {
  width: 240px;
}
.feishu-mode__effective { margin: 4px 0 0; font-size: 12px; color: var(--fg-dim, #999); }
.feishu-mode__warn { color: var(--warn, #d89614); margin-left: 8px; }
</style>
