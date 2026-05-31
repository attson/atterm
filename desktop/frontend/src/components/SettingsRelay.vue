<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { getRelayConfig, setRelayConfig, setUplinkPaused, fetchRelayMe } from "../lib/api";
import { usePlatform } from '../platform'
const platform = usePlatform()
import SelectDropdown from "./SelectDropdown.vue";
import PairingPanel from "./PairingPanel.vue";
import { useI18n } from "../i18n/useI18n";
import type { MessageKey } from "../i18n";
import { classifyRelaySetupError, relayHttpToWsURL, validateRelaySetupInputs, type RelaySetupIssue } from "../lib/relaySetupWizard";

const emit = defineEmits<{
  (e: "relay-config-changed"): void;
  (e: "dirty", value: boolean): void;
}>();

const url = ref("");
const token = ref("");
const allowInsecureRelay = ref(false);
const remotePermission = ref("full");
const paused = ref(false);
const loading = ref(true);
const saving = ref(false);
const togglingPause = ref(false);
const wizardRunning = ref(false);
const wizardDone = ref(false);
const wizardIssue = ref<RelaySetupIssue | null>(null);
const wizardIdentity = ref("");
const error = ref("");
const { t } = useI18n();

// In-memory only (SEC-1): never persisted or logged.
const connectedUserID = ref("");
const connectedEmail = ref("");

const persistedUrl = ref("");
const persistedToken = ref("");
const persistedAllowInsecure = ref(false);
const persistedPermission = ref("full");

const permissionOptions = computed(() => [
  { value: "view", label: t("settings.relay.viewOnly"), description: t("settings.relay.viewOnlyDesc") },
  { value: "control", label: t("settings.relay.control"), description: t("settings.relay.controlDesc") },
  { value: "full", label: t("settings.relay.full"), description: t("settings.relay.fullDesc") },
]);

const dirty = computed(
  () =>
    url.value !== persistedUrl.value ||
    token.value !== persistedToken.value ||
    allowInsecureRelay.value !== persistedAllowInsecure.value ||
    remotePermission.value !== persistedPermission.value,
);

watch(dirty, (value) => emit("dirty", value));

// Token format warning: tokens must start with atk_
const tokenWarning = computed(() =>
  token.value && !token.value.startsWith("atk_")
    ? t("settings.relay.tokenWarning")
    : "",
);

// Status pill matrix per spec §8.2
const statusPill = computed(() => {
  if (connectedEmail.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedEmail.value }), cls: "on" };
  }
  if (connectedUserID.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedUserID.value.slice(0, 8) }), cls: "on" };
  }
  if (!url.value) {
    return { text: t("settings.relay.notConfigured"), cls: "off" };
  }
  if (paused.value) {
    return { text: t("settings.relay.paused"), cls: "off" };
  }
  return { text: t("settings.relay.uplinkRunning"), cls: "on" };
});

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    url.value = cfg.url;
    token.value = cfg.token;
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    remotePermission.value = cfg.remote_permission || "full";
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }

  platform.events.on('relay:auth-info', async (data) => {
    const { user_id } = data as { user_id: string };
    connectedUserID.value = user_id || "";
    try {
      const me = await fetchRelayMe();
      connectedEmail.value = me.email || "";
    } catch {
      // Ignore; status row falls back to showing the short user_id.
    }
  });
});

function snapshotPersisted() {
  persistedUrl.value = url.value;
  persistedToken.value = token.value;
  persistedAllowInsecure.value = allowInsecureRelay.value;
  persistedPermission.value = remotePermission.value;
}

async function save() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({
      url: url.value.trim(),
      token: token.value.trim(),
      allow_insecure_relay: allowInsecureRelay.value,
      remote_permission: remotePermission.value,
    });
    const cfg = await getRelayConfig();
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

async function handleTogglePaused() {
  togglingPause.value = true;
  error.value = "";
  try {
    await setUplinkPaused(paused.value);
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
    // Revert toggle on failure
    paused.value = !paused.value;
  } finally {
    togglingPause.value = false;
  }
}

function openInBrowser() {
  if (!url.value) return;
  void platform.system.openExternalURL(`${url.value}/settings.html`);
}

const canSave = computed(() => !saving.value && !!url.value.trim());
const saveLabel = computed(() => (saving.value ? t("settings.relay.saving") : t("settings.relay.saveConnect")));
const wizardWsUrl = computed(() => {
  try {
    return url.value.trim() ? relayHttpToWsURL(url.value.trim()) : "";
  } catch {
    return "";
  }
});
const wizardRecovery = computed(() => wizardIssue.value ? t(wizardIssue.value.recoveryKey as MessageKey) : "");
const wizardChecks = computed(() => [
  { key: "reachability", label: t("settings.relay.wizard.steps.reachability") },
  { key: "urlCompatibility", label: t("settings.relay.wizard.steps.urlCompatibility") },
  { key: "apiToken", label: t("settings.relay.wizard.steps.apiToken") },
  { key: "identity", label: t("settings.relay.wizard.steps.identity") },
  { key: "uplink", label: t("settings.relay.wizard.steps.uplink") },
]);

async function runWizardValidation() {
  wizardRunning.value = true;
  wizardDone.value = false;
  wizardIssue.value = null;
  wizardIdentity.value = "";
  error.value = "";
  const localIssue = validateRelaySetupInputs(url.value, token.value, allowInsecureRelay.value);
  if (localIssue) {
    wizardIssue.value = localIssue;
    wizardRunning.value = false;
    return;
  }
  try {
    await setRelayConfig({
      url: url.value.trim(),
      token: token.value.trim(),
      allow_insecure_relay: allowInsecureRelay.value,
      remote_permission: remotePermission.value,
    });
    const me = await fetchRelayMe();
    wizardIdentity.value = me.email || me.user_id || "";
    const cfg = await getRelayConfig();
    paused.value = (cfg as any).paused ?? false;
    if (!(cfg as any).connected) {
      throw new Error("uplink not connected");
    }
    snapshotPersisted();
    wizardDone.value = true;
    emit("relay-config-changed");
  } catch (e: any) {
    wizardIssue.value = classifyRelaySetupError(e);
  } finally {
    wizardRunning.value = false;
  }
}

defineExpose({
  save,
  canSave,
  saveLabel,
  paused,
  saving,
});
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="uplink-toggle-row">
        <span class="field-label">{{ t("settings.relay.uplink") }}</span>
        <label class="toggle-switch" :class="{ disabled: togglingPause }">
          <input
            v-model="paused"
            type="checkbox"
            :true-value="false"
            :false-value="true"
            :disabled="togglingPause || !url"
            @change="handleTogglePaused"
          />
          <span class="toggle-track">
            <span class="toggle-thumb" />
          </span>
          <span class="toggle-label">{{ paused ? t("settings.relay.off") : t("settings.relay.on") }}</span>
        </label>
      </div>

      <div class="status-pill" :class="statusPill.cls">
        <span class="dot">●</span>
        {{ statusPill.text }}
      </div>

      <p class="hint">
        {{ t("settings.relay.hint") }}
      </p>

      <section class="relay-wizard" data-testid="relay-setup-wizard">
        <div class="wizard-head">
          <div>
            <div class="wizard-title">{{ t("settings.relay.wizard.title") }}</div>
            <div class="wizard-sub">{{ wizardWsUrl || t("settings.relay.wizard.noWsUrl") }}</div>
          </div>
          <button type="button" :disabled="wizardRunning || saving" @click="runWizardValidation">
            {{ wizardRunning ? t("settings.relay.wizard.checking") : t("settings.relay.wizard.run") }}
          </button>
        </div>
        <div class="wizard-steps">
          <span v-for="step in wizardChecks" :key="step.key" class="wizard-step">{{ step.label }}</span>
        </div>
        <p v-if="wizardDone" class="wizard-ok">
          {{ t("settings.relay.wizard.ok", { identity: wizardIdentity }) }}
        </p>
        <p v-if="wizardIssue" class="wizard-error">
          {{ t(`settings.relay.wizard.errors.${wizardIssue.code}` as MessageKey) }}
          <span class="wizard-recovery">{{ wizardRecovery }}</span>
        </p>
      </section>

      <label class="field-label">{{ t("settings.relay.relayUrl") }}</label>
      <input
        v-model="url"
        type="text"
        placeholder="wss://relay.example.com"
        :disabled="saving"
        @keyup.enter="save"
      />

      <label class="field-label">{{ t("settings.relay.apiToken") }}</label>
      <input
        v-model="token"
        type="password"
        placeholder="atk_xxxxxxxx…"
        :disabled="saving"
        @keyup.enter="save"
      />
      <p v-if="tokenWarning" class="token-warning">{{ tokenWarning }}</p>
      <div class="token-actions">
        <button
          v-if="url"
          class="btn-link"
          type="button"
          @click="openInBrowser"
        >{{ t("settings.relay.openInBrowser") }}</button>
      </div>

      <label class="field-label">{{ t("settings.relay.remotePermissions") }}</label>
      <SelectDropdown
        v-model="remotePermission"
        :options="permissionOptions"
        :disabled="saving"
        :aria-label="t('settings.relay.remotePermissions')"
      />
      <p class="hint">
        {{ t("settings.relay.permissionsHint") }}
      </p>

      <label class="checkbox">
        <input
          v-model="allowInsecureRelay"
          type="checkbox"
          :disabled="saving"
        />
        {{ t("settings.relay.insecureMode") }}
      </label>
      <p v-if="allowInsecureRelay" class="warning">
        {{ t("settings.relay.insecureWarning") }}
      </p>

      <p v-if="error" class="error">{{ error }}</p>

      <PairingPanel />
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dim {
  color: var(--fg-dim);
  font-size: 13px;
}
.uplink-toggle-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.toggle-switch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}
.toggle-switch.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.toggle-switch input[type="checkbox"] {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}
.toggle-track {
  position: relative;
  display: inline-block;
  width: 32px;
  height: 18px;
  background: var(--fg-dim);
  border-radius: 9px;
  transition: background 0.15s;
}
.toggle-switch input:checked + .toggle-track {
  background: var(--good);
}
.toggle-thumb {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  background: var(--fg);
  border-radius: 50%;
  transition: transform 0.15s;
}
.toggle-switch input:checked + .toggle-track .toggle-thumb {
  transform: translateX(14px);
}
.toggle-label {
  font-size: 12px;
  color: var(--fg-dim);
  min-width: 2.2em;
}
.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: 12px;
  width: max-content;
}
.status-pill .dot {
  font-size: 10px;
  line-height: 1;
}
.status-pill.on .dot { color: var(--good); }
.status-pill.off .dot { color: var(--fg-dim); }
.status-pill.on { color: var(--good); }
.status-pill.off { color: var(--fg-dim); }
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0;
  line-height: 1.5;
}
.field-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.token-warning {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.token-actions {
  display: flex;
  gap: 8px;
}
.btn-link {
  background: none;
  border: none;
  color: var(--accent);
  font-size: 12px;
  padding: 0;
  cursor: pointer;
  text-decoration: underline;
}
.btn-link:hover {
  opacity: 0.8;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.warning {
  color: var(--bad);
  font-size: 12px;
  line-height: 1.45;
  margin: 0;
}
.relay-wizard {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: color-mix(in srgb, var(--panel) 88%, var(--accent) 12%);
}
.wizard-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}
.wizard-head button {
  height: 30px;
  border: 1px solid var(--accent);
  border-radius: 7px;
  background: var(--accent);
  color: var(--bg);
  padding: 0 12px;
  font-size: 12px;
  font-weight: 700;
}
.wizard-head button:disabled { opacity: .55; }
.wizard-title { font-size: 13px; font-weight: 700; color: var(--fg); }
.wizard-sub { font-size: 11px; color: var(--fg-dim); margin-top: 2px; }
.wizard-steps { display: flex; flex-wrap: wrap; gap: 6px; }
.wizard-step {
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 999px;
  color: var(--fg-dim);
  font-size: 11px;
}
.wizard-ok, .wizard-error { margin: 0; font-size: 12px; line-height: 1.45; }
.wizard-ok { color: var(--good); }
.wizard-error { color: var(--bad); }
.wizard-recovery { display: block; color: var(--fg-dim); margin-top: 3px; }
input[type="text"],
input[type="password"] {
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
}
input:focus {
  outline: none;
  box-shadow: 0 0 0 2px var(--accent);
}
</style>
