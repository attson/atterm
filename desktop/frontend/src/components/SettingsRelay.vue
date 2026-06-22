<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { getRelayConfig, setRelayConfig, setRelayDisableE2EE, setUplinkPaused, fetchRelayMe, loginRemoteRelay, registerRemoteRelay, probeRelayVersion, loadSavedRelayPassword } from "../lib/api";
import { usePlatform } from '../platform'
const platform = usePlatform()
import PairingPanel from "./PairingPanel.vue";
import { useI18n } from "../i18n/useI18n";

const emit = defineEmits<{
  (e: "relay-config-changed"): void;
  (e: "dirty", value: boolean): void;
}>();

// `host` holds the bare relay host the user types (no scheme). Relay
// connections are always HTTPS/WSS; the scheme is rendered as a fixed
// `https://` prefix. `fullUrl` reconstructs the URL sent to the backend.
const host = ref("");
// `token` mirrors the persisted session token (issued by /api/auth/login).
// It is no longer user-editable — see the email/password login form below.
const token = ref("");
// allowInsecureRelay = "trust self-signed certificate": when checked the
// desktop skips TLS verification so it can reach a relay serving a
// self-signed cert (atterm-relay's quick-start default). It no longer
// downgrades the scheme to http:// — connections stay HTTPS either way.
const allowInsecureRelay = ref(false);
// disableE2EE = true means agent will NOT seal outbound session content.
// Persisted in appConfig.DisableE2EE and applied immediately via the
// dedicated setRelayDisableE2EE binding (no Save needed). Used for
// testing the plaintext fallback path; the TitleBar shows a ⚠ chip
// while this is on so the user doesn't forget.
const disableE2EE = ref(false);
const paused = ref(false);
const loading = ref(true);
const saving = ref(false);
const togglingPause = ref(false);
const error = ref("");
const { t } = useI18n();

// Login form state. Password is mirrored to the safekeyring slot by LoginRemoteRelay so it can be prefilled on subsequent launches.
const email = ref("");
const password = ref("");
const showPassword = ref(false);

// Mode toggle: "login" against an existing account, or "register" a new
// one via the OPAQUE flow. claimToken is shown only in register mode and
// is optional (operators supply it from `atterm-relay` bootstrap output
// to promote the new user to admin).
const authMode = ref<"login" | "register">("login");
const claimToken = ref("");

// In-memory only (SEC-1): never persisted or logged.
const connectedUserID = ref("");
const connectedEmail = ref("");

const persistedHost = ref("");
const persistedToken = ref("");
const persistedAllowInsecure = ref(false);

// Strip any scheme the user might paste so `host` always stays bare; the
// canonical scheme is owned by the insecure-mode toggle.
function stripScheme(s: string): string {
  return s.trim().replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, "");
}

// Always HTTPS — WebCrypto/OPAQUE require a secure context and the relay
// serves TLS by default. The self-signed-trust checkbox is orthogonal to the
// scheme (it only relaxes certificate verification on the desktop side).
const urlScheme = computed(() => "https://");

// If a full URL is pasted into the host field, drop the scheme so the bare
// host doesn't render doubled against the fixed prefix. Only fires when a
// `scheme://` is actually present, so normal host typing is untouched.
watch(host, (value) => {
  if (/:\/\//.test(value)) host.value = stripScheme(value);
});

// Full URL handed to the backend (probe / login / setRelayConfig). Empty when
// no host is entered so callers can treat it like the old `url` value.
const fullUrl = computed(() => {
  const h = stripScheme(host.value);
  return h ? urlScheme.value + h : "";
});

const dirty = computed(
  () =>
    stripScheme(host.value) !== persistedHost.value ||
    token.value !== persistedToken.value ||
    allowInsecureRelay.value !== persistedAllowInsecure.value,
);

watch(dirty, (value) => emit("dirty", value));

// Login is the default; register is reached through the muted link below the
// password field. Switching modes clears the claim token so a stale value
// can't leak into the next attempt.
function toggleAuthMode() {
  authMode.value = authMode.value === "login" ? "register" : "login";
  if (authMode.value === "login") claimToken.value = "";
}

function isValidRelayUrl(s: string): boolean {
  try {
    const u = new URL(s.trim());
    if (!u.host) return false;
    return ["http:", "https:", "ws:", "wss:"].includes(u.protocol);
  } catch {
    return false;
  }
}

// Status pill: 4 states. Order matters — connected wins over everything.
const statusPill = computed(() => {
  if (connectedEmail.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedEmail.value }), cls: "on" };
  }
  if (connectedUserID.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedUserID.value.slice(0, 8) }), cls: "on" };
  }
  if (!fullUrl.value) {
    return { text: t("settings.relay.notConfigured"), cls: "off" };
  }
  if (paused.value) {
    return { text: t("settings.relay.paused"), cls: "off" };
  }
  if (!isValidRelayUrl(fullUrl.value)) {
    return { text: t("settings.relay.relayInvalid"), cls: "error" };
  }
  return { text: t("settings.relay.connecting"), cls: "warn" };
});

onMounted(async () => {
  try {
    const cfg = await getRelayConfig();
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    host.value = stripScheme(cfg.url);
    token.value = cfg.token;
    disableE2EE.value = (cfg as any).disable_e2ee ?? false;
    paused.value = (cfg as any).paused ?? false;
    snapshotPersisted();

    // Prefill email from persisted config (set by LoginRemoteRelay on
    // last successful login).
    email.value = cfg.last_email ?? "";

    // Prefill the password from the safekeyring slot the most recent
    // successful login wrote. Empty when nothing is stored or when no
    // email was cached.
    if (email.value) {
      try {
        password.value = await loadSavedRelayPassword();
      } catch {
        // Treat any binding failure as "no stored password" — the user
        // can still type one in.
      }
    }

    // Show the "logged in as X" pill immediately without waiting for the
    // uplink's relay:auth-info event. The event listener below stays
    // registered as a fallback (covers identity changes during the dialog
    // session — e.g., login from another device, admin promotion).
    if (cfg.token) {
      try {
        const me = await fetchRelayMe();
        connectedEmail.value = me.email || "";
        connectedUserID.value = me.user_id || "";
      } catch {
        // Token rejected (401) or network error — pill stays empty; the
        // uplink event stream and apiFetch's 401 interceptor will produce
        // an accurate state if/when the session is actually invalid.
      }
    }
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

  // Backend-side flips (e.g. from another window) feed this event so
  // the checkbox stays in sync with the persisted state without polling.
  platform.events.on('e2ee-mode-changed', (data) => {
    const next = (data as { disabled?: boolean })?.disabled;
    if (typeof next === 'boolean') {
      disableE2EE.value = next;
    }
  });
});

onBeforeUnmount(() => {
  // platform.events handlers are cleaned up at the platform layer when
  // the component unmounts; this hook is kept as the documented place
  // to revisit if the event API ever returns explicit dispose handles.
});

// E2EE toggle is applied immediately (no Save required) — testing the
// unsealed fallback otherwise means typing email/password again every
// flip. setRelayDisableE2EE is idempotent and emits the same event we
// listen for above, so the optimistic UI update reconciles with the
// authoritative state on the next round-trip.
async function onDisableE2EEChange(e: Event) {
  const target = e.target as HTMLInputElement | null;
  const want = !!target?.checked;
  disableE2EE.value = want;
  try {
    await setRelayDisableE2EE(want);
  } catch (err: any) {
    // Revert the optimistic flip on failure so the UI matches the
    // backend.
    disableE2EE.value = !want;
    error.value = err?.message ?? String(err);
  }
}

function snapshotPersisted() {
  persistedHost.value = stripScheme(host.value);
  persistedToken.value = token.value;
  persistedAllowInsecure.value = allowInsecureRelay.value;
}

// Persist the URL / insecure flag / email even when the connect fails, so
// reopening the dialog doesn't lose them. The token is kept as-is; with no
// token the backend skips the uplink (no failing reconnect loop). Best-effort —
// never masks the real connect error.
async function rememberInputs() {
  try {
    await setRelayConfig({
      url: fullUrl.value,
      token: token.value,
      session_expires_at: 0,
      allow_insecure_relay: allowInsecureRelay.value,
      disable_e2ee: disableE2EE.value,
      remote_permission: "full",
      last_email: email.value.trim(),
    });
  } catch {
    /* ignore — the real failure is already surfaced */
  }
}

async function save() {
  saving.value = true;
  error.value = "";

  // 1. URL format check (cheap, local). fullUrl already carries the scheme
  // derived from insecure mode, so we validate the reconstructed value.
  if (!isValidRelayUrl(fullUrl.value)) {
    error.value = t("settings.relay.relayInvalid");
    saving.value = false;
    return;
  }

  // 2. /api/version probe via Wails Go method
  try {
    await probeRelayVersion(fullUrl.value, allowInsecureRelay.value);
  } catch (e: any) {
    await rememberInputs();
    error.value = t("settings.relay.versionProbeFailed", { reason: e?.message ?? String(e) });
    saving.value = false;
    return;
  }

  // 3. Login vs reuse-token vs error
  const hasCreds = !!(email.value.trim() && password.value);
  const hasExistingToken = !!token.value;

  if (hasCreds) {
    try {
      if (authMode.value === "register") {
        await registerRemoteRelay(fullUrl.value, email.value.trim(), password.value, claimToken.value.trim(), allowInsecureRelay.value);
      } else {
        await loginRemoteRelay(fullUrl.value, email.value.trim(), password.value, allowInsecureRelay.value);
      }
    } catch (e: any) {
      await rememberInputs();
      error.value = t("settings.relay.loginFailedInline", { reason: e?.message ?? String(e) });
      saving.value = false;
      return;
    }
    claimToken.value = "";
  } else if (hasExistingToken) {
    try {
      await setRelayConfig({
        url: fullUrl.value,
        token: token.value,
        session_expires_at: 0,
        allow_insecure_relay: allowInsecureRelay.value,
        disable_e2ee: disableE2EE.value,
        // Single-user tool: sessions are always published with full
        // permission (the view/control selector was removed — there is no
        // sharing scenario). The wire field is kept for protocol compat.
        remote_permission: "full",
      });
    } catch (e: any) {
      error.value = e?.message ?? String(e);
      saving.value = false;
      return;
    }
  } else {
    await rememberInputs();
    error.value = t("settings.relay.credentialsRequired");
    saving.value = false;
    return;
  }

  // 4. Refresh from persisted config
  try {
    const cfg = await getRelayConfig();
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    host.value = stripScheme(cfg.url);
    token.value = cfg.token;
    disableE2EE.value = (cfg as any).disable_e2ee ?? false;
    paused.value = (cfg as any).paused ?? false;
    email.value = cfg.last_email ?? "";
    snapshotPersisted();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
    saving.value = false;
    return;
  }

  saving.value = false;
  emit("relay-config-changed");
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

const canSave = computed(() => !saving.value && !!stripScheme(host.value));
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
            :disabled="togglingPause || !host"
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

      <div class="url-label-row">
        <label class="field-label" for="relay-host">{{ t("settings.relay.relayUrl") }}</label>
        <label class="insecure-inline">
          <input
            v-model="allowInsecureRelay"
            type="checkbox"
            :disabled="saving"
          />
          {{ t("settings.relay.insecureMode") }}
        </label>
      </div>
      <div class="url-input" :class="{ insecure: allowInsecureRelay }">
        <span class="url-scheme" aria-hidden="true">{{ urlScheme }}</span>
        <input
          id="relay-host"
          v-model="host"
          type="text"
          placeholder="relay.example.com"
          autocomplete="off"
          spellcheck="false"
          :disabled="saving"
          @keyup.enter="save"
        />
      </div>
      <p v-if="allowInsecureRelay" class="warning">
        {{ t("settings.relay.insecureWarning") }}
      </p>

      <label class="field-label" for="relay-email">{{ t("settings.relay.email") }}</label>
      <input
        id="relay-email"
        v-model="email"
        type="email"
        :autocomplete="authMode === 'register' ? 'email' : 'username'"
        :disabled="saving"
        @keyup.enter="save"
      />

      <label class="field-label" for="relay-password">{{ t("settings.relay.password") }}</label>
      <div class="password-field">
        <input
          id="relay-password"
          v-model="password"
          :type="showPassword ? 'text' : 'password'"
          autocomplete="current-password"
          :disabled="saving"
          @keyup.enter="save"
        />
        <button
          type="button"
          class="password-toggle"
          :aria-label="showPassword ? t('settings.relay.passwordHide') : t('settings.relay.passwordShow')"
          :aria-pressed="showPassword"
          :disabled="saving"
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

      <template v-if="authMode === 'register'">
        <label class="field-label" for="relay-claim-token">{{ t("settings.relay.claimToken") }}</label>
        <input
          id="relay-claim-token"
          v-model="claimToken"
          type="text"
          autocomplete="off"
          spellcheck="false"
          :placeholder="t('settings.relay.claimTokenPlaceholder')"
          :disabled="saving"
          @keyup.enter="save"
        />
        <p class="hint">{{ t("settings.relay.claimTokenHint") }}</p>
      </template>

      <button
        type="button"
        class="auth-switch"
        :disabled="saving"
        @click="toggleAuthMode"
      >
        {{ authMode === 'login' ? t('settings.relay.noAccountPrompt') : t('settings.relay.haveAccountPrompt') }}
        <span class="auth-switch-action">{{ authMode === 'login' ? t('settings.relay.modeRegister') : t('settings.relay.modeLogin') }}</span>
      </button>

      <label class="checkbox">
        <input
          :checked="disableE2EE"
          type="checkbox"
          :disabled="saving"
          @change="onDisableE2EEChange"
        />
        {{ t("settings.relay.disableE2EE") }}
      </label>
      <p v-if="disableE2EE" class="warning">
        {{ t("settings.relay.disableE2EEWarning") }}
      </p>

      <p v-if="error" class="error">{{ error }}</p>

      <PairingPanel />
    </template>
  </div>
</template>

<style scoped>
/* relay url label row: label on the left, insecure-mode toggle on the right */
.url-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.insecure-inline {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: none;
  letter-spacing: normal;
  cursor: pointer;
  user-select: none;
}
.insecure-inline input[type="checkbox"] {
  margin: 0;
}

/* url input with a fixed, non-editable scheme prefix glued to its left */
.url-input {
  display: flex;
  align-items: stretch;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
  background: var(--bg);
}
.url-input:focus-within {
  box-shadow: 0 0 0 2px var(--accent);
}
.url-scheme {
  display: inline-flex;
  align-items: center;
  padding: 0 8px;
  font-size: 13px;
  color: var(--fg-dim);
  background: color-mix(in srgb, var(--fg-dim) 12%, transparent 88%);
  border-right: 1px solid var(--border);
  user-select: none;
  white-space: nowrap;
}
.url-input.insecure .url-scheme {
  color: var(--warn, #d97706);
}
.url-input input {
  flex: 1;
  border: 0;
  border-radius: 0;
  background: transparent;
  min-width: 0;
}
.url-input input:focus {
  box-shadow: none;
}

/* muted register/login switch link below the password field */
.auth-switch {
  appearance: none;
  align-self: flex-start;
  background: transparent;
  border: 0;
  padding: 0;
  margin: -4px 0 0;
  font: inherit;
  font-size: 12px;
  color: var(--fg-dim);
  cursor: pointer;
}
.auth-switch:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.auth-switch-action {
  color: var(--accent);
  text-decoration: underline;
  text-underline-offset: 2px;
}
.auth-switch:hover:not(:disabled) .auth-switch-action {
  text-decoration: none;
}

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
.status-pill.warn {
  color: var(--warn, #d97706);
}
.status-pill.warn .dot {
  color: var(--warn, #d97706);
}
.status-pill.error {
  color: var(--bad);
}
.status-pill.error .dot {
  color: var(--bad);
}
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
