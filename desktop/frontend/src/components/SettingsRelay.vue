<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import { getRelayConfig, setRelayConfig, setUplinkPaused, fetchRelayMe, loginRemoteRelay } from "../lib/api";
import { usePlatform } from '../platform'
const platform = usePlatform()
import SelectDropdown from "./SelectDropdown.vue";
import PairingPanel from "./PairingPanel.vue";
import { useI18n } from "../i18n/useI18n";

const emit = defineEmits<{
  (e: "relay-config-changed"): void;
  (e: "dirty", value: boolean): void;
}>();

const url = ref("");
// `token` mirrors the persisted session token (issued by /api/auth/login).
// It is no longer user-editable — see the email/password login form below.
const token = ref("");
const allowInsecureRelay = ref(false);
const remotePermission = ref("full");
const paused = ref(false);
const loading = ref(true);
const saving = ref(false);
const togglingPause = ref(false);
const error = ref("");
const { t } = useI18n();

// Login form state. Password lives only in memory and is cleared on success.
const email = ref("");
const password = ref("");
const showPassword = ref(false);
const loginInProgress = ref(false);
const loginError = ref("");
const loginSuccess = ref(false);

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

const canLogin = computed(
  () => !loginInProgress.value && !!url.value.trim() && !!email.value.trim() && !!password.value,
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

async function login() {
  loginError.value = "";
  loginSuccess.value = false;
  if (!url.value.trim() || !email.value.trim() || !password.value) {
    return;
  }
  loginInProgress.value = true;
  try {
    await loginRemoteRelay(url.value.trim(), email.value.trim(), password.value);
    // LoginRemoteRelay persisted the new session token via SetRelayConfig.
    // Re-read so `token` reflects the new value (and dirty/save logic sees it).
    const cfg = await getRelayConfig();
    url.value = cfg.url;
    token.value = cfg.token;
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    remotePermission.value = cfg.remote_permission || "full";
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();
    password.value = "";
    loginSuccess.value = true;
    emit("relay-config-changed");
  } catch (e: any) {
    loginError.value = e?.message ?? String(e);
  } finally {
    loginInProgress.value = false;
  }
}

const canSave = computed(() => !saving.value && !!url.value.trim());
const saveLabel = computed(() => (saving.value ? t("settings.relay.saving") : t("settings.relay.saveConnect")));

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

      <label class="field-label">{{ t("settings.relay.relayUrl") }}</label>
      <input
        v-model="url"
        type="text"
        placeholder="https://relay.example.com"
        :disabled="saving || loginInProgress"
        @keyup.enter="save"
      />

      <section class="relay-login" data-testid="relay-login-form">
        <div class="login-title">{{ t("settings.relay.loginTitle") }}</div>
        <p class="hint">{{ t("settings.relay.loginHint") }}</p>

        <label class="field-label" for="relay-login-email">{{ t("settings.relay.email") }}</label>
        <input
          id="relay-login-email"
          v-model="email"
          type="email"
          autocomplete="username"
          :disabled="loginInProgress || saving"
          @keyup.enter="login"
        />

        <label class="field-label" for="relay-login-password">{{ t("settings.relay.password") }}</label>
        <div class="password-field">
          <input
            id="relay-login-password"
            v-model="password"
            :type="showPassword ? 'text' : 'password'"
            autocomplete="current-password"
            :disabled="loginInProgress || saving"
            @keyup.enter="login"
          />
          <button
            type="button"
            class="password-toggle"
            :aria-label="showPassword ? t('settings.relay.passwordHide') : t('settings.relay.passwordShow')"
            :aria-pressed="showPassword"
            :disabled="loginInProgress || saving"
            @click="showPassword = !showPassword"
          >
            <svg
              v-if="!showPassword"
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
              <circle cx="12" cy="12" r="3" />
            </svg>
            <svg
              v-else
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" />
              <line x1="1" y1="1" x2="23" y2="23" />
            </svg>
          </button>
        </div>

        <div class="login-actions">
          <button
            type="button"
            class="login-btn"
            :disabled="!canLogin"
            @click="login"
          >
            {{ loginInProgress ? t("settings.relay.loginInProgress") : t("settings.relay.login") }}
          </button>
        </div>

        <p v-if="loginSuccess" class="login-ok">{{ t("settings.relay.loggedIn") }}</p>
        <p v-if="loginError" class="login-error">
          <span class="login-error-label">{{ t("settings.relay.loginFailed") }}:</span>
          {{ loginError }}
        </p>
      </section>

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
.relay-login {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: color-mix(in srgb, var(--panel) 88%, var(--accent) 12%);
}
.login-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--fg);
}
.login-actions {
  display: flex;
  justify-content: flex-end;
}
.login-btn {
  height: 30px;
  padding: 0 14px;
  border: 1px solid var(--accent);
  border-radius: 7px;
  background: var(--accent);
  color: var(--bg);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}
.login-btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.login-ok {
  color: var(--good);
  font-size: 12px;
  margin: 0;
}
.login-error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
  line-height: 1.45;
}
.login-error-label {
  font-weight: 700;
  margin-right: 4px;
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
input[type="text"],
input[type="email"],
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

.password-field {
  position: relative;
  display: block;
}

.password-field input {
  width: 100%;
  padding-right: 36px;
}

.password-toggle {
  position: absolute;
  top: 50%;
  right: 8px;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--fg-dim);
  border-radius: 4px;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}

.password-toggle:hover:not(:disabled) {
  color: var(--fg);
  background: color-mix(in srgb, var(--accent) 12%, transparent 88%);
}

.password-toggle:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}

.password-toggle:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
