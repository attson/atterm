<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import { changePassword, deleteMe } from "@shared/api/me";
import { ApiError } from "@shared/api/client";
import { useI18n } from "../i18n/useI18n";
import { usePlatform } from "../platform";
import { validateRelayBase } from "../lib/relayUrl";
import { QRScanner } from "../platform/qrScanner";
import SettingsPairingConsume from "./SettingsPairingConsume.vue";

const { t } = useI18n();
const platform = usePlatform();

// ----- Email (read-only) -----
const email = ref("");
const emailLoading = ref(true);
const logoutSubmitting = ref(false);
const authenticated = ref(false);
const loginScheme = ref<"https://" | "http://">("https://");
const loginHost = ref("");
const loginEmail = ref("");
const loginPassword = ref("");
const loginAllowInsecure = ref(false);
const loginSubmitting = ref(false);
const loginError = ref("");
const scannedPairingUrl = ref("");

const canInlineLogin = computed(() => !!platform.relay.login);
const canPairByQR = computed(() => !!platform.relay.consumePairing);
const loginUrl = computed(() => loginScheme.value + loginHost.value.trim());

function applyLoginUrl(raw: string): void {
  const m = /^\s*(https?:\/\/)(.*)$/i.exec(raw);
  if (m) {
    loginScheme.value = m[1].toLowerCase() as "https://" | "http://";
    loginHost.value = m[2].trim();
  } else {
    loginHost.value = raw.trim();
  }
  if (loginScheme.value === "https://") loginAllowInsecure.value = false;
}

function normalizeLoginHost(): void {
  applyLoginUrl(loginHost.value);
}

onMounted(async () => {
  try {
    const cfg = await platform.relay.load();
    if (cfg) {
      applyLoginUrl(cfg.url || "");
      loginEmail.value = cfg.last_email || "";
      loginAllowInsecure.value = !!cfg.allow_insecure_relay;
    }
  } catch {
    /* keep empty login form */
  }
  try {
    loginPassword.value = await platform.relay.loadSavedPassword?.() ?? "";
  } catch {
    /* no saved password */
  }
  try {
    const me = await platform.relay.fetchMe();
    email.value = me.email;
    if (!loginEmail.value) loginEmail.value = me.email;
    authenticated.value = true;
  } catch {
    // Best-effort — leave email blank; the danger-zone email match will
    // simply never satisfy, so delete stays disabled. No separate error UI
    // for this since every other tab in SettingsDialog fails the same way
    // when unauthenticated.
    authenticated.value = false;
  } finally {
    emailLoading.value = false;
  }
});

async function onLogoutClick() {
  if (logoutSubmitting.value) return;
  logoutSubmitting.value = true;
  try {
    await platform.relay.logout?.();
  } catch {
    // The shared web logout helper clears local state in finally. Do not keep
    // the user on an authenticated surface just because server revoke failed.
  } finally {
    if (canInlineLogin.value) {
      authenticated.value = false;
      email.value = "";
      logoutSubmitting.value = false;
      platform.events.emit("relay:auth-restored", undefined);
      return;
    }
    location.assign("/login.html");
  }
}

async function onLoginSubmit() {
  if (!platform.relay.login || loginSubmitting.value) return;
  loginError.value = "";
  const validation = validateRelayBase(loginUrl.value, loginAllowInsecure.value);
  if (validation) {
    loginError.value = validation;
    return;
  }
  if (!loginEmail.value.trim()) {
    loginError.value = t("mobile.emailRequired");
    return;
  }
  if (!loginPassword.value) {
    loginError.value = t("mobile.passwordRequired");
    return;
  }
  loginSubmitting.value = true;
  try {
    await platform.relay.login(
      loginUrl.value.replace(/\/$/, ""),
      loginEmail.value.trim(),
      loginPassword.value,
      loginAllowInsecure.value,
    );
    const me = await platform.relay.fetchMe();
    email.value = me.email;
    loginEmail.value = me.email;
    authenticated.value = true;
    platform.events.emit("relay:auth-restored", undefined);
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    if (msg === "invalid_credentials") loginError.value = t("mobile.invalidCredentials");
    else if (msg === "rate_limited") loginError.value = t("mobile.rateLimited");
    else if (/403|origin/i.test(msg)) loginError.value = t("mobile.originRejected");
    else loginError.value = t("mobile.cannotReachRelay", { message: msg });
  } finally {
    loginSubmitting.value = false;
  }
}

async function onScanQR(): Promise<void> {
  loginError.value = "";
  try {
    const { camera } = await QRScanner.requestPermissions();
    if (camera !== "granted") {
      loginError.value = t("mobile.pairing.cameraDenied");
      return;
    }
    const result = await QRScanner.scan();
    if (result.cancelled) return;
    if (!result.rawValue) {
      loginError.value = t("mobile.pairing.noQrDetected");
      return;
    }
    scannedPairingUrl.value = result.rawValue;
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e);
    if (/PLUGIN_NOT_AVAILABLE|not implemented/i.test(msg)) {
      loginError.value = t("mobile.pairing.scanNotAvailable");
    } else {
      loginError.value = msg;
    }
  }
}

async function onPairingConnected(): Promise<void> {
  scannedPairingUrl.value = "";
  try {
    const me = await platform.relay.fetchMe();
    email.value = me.email;
    loginEmail.value = me.email;
    authenticated.value = true;
    platform.events.emit("relay:auth-restored", undefined);
  } catch (e) {
    loginError.value = e instanceof Error ? e.message : String(e);
  }
}

// describeAccountError maps the errors the OPAQUE step-up + password/delete
// endpoints can throw onto the toast copy from spec §4.4's error table.
// - requestStepUpToken (driven internally by deleteMe) throws a plain
//   Error('invalid credentials') when the client-side KE2 MAC check fails,
//   i.e. wrong current password.
// - DELETE /api/me responds 401 with a JSON {error: 'step_up_required' |
//   'step_up_invalid'} when the step-up token is missing/expired — that's
//   the "session expired" case, distinct from a wrong password.
// - Any other 401 (stepup init/finalize server-side reject, or the
//   password-change endpoint) reads as "wrong current password".
//
// Note: the step_up_required / step_up_invalid branches below are unreachable
// today for the change-password path — the restored `changePassword`
// (web/src/shared/api/me.ts) sends the historic pre-stepup shape and never
// attaches a step-up token, so the relay can't respond with those codes for
// that endpoint. They stay wired for `deleteMe` (which does drive step-up)
// and for the follow-up that reintroduces step-up on change-password.
function describeAccountError(e: unknown): string {
  if (e instanceof Error && e.message === "invalid credentials") {
    return t("settings.account.errors.wrongPassword");
  }
  if (e instanceof ApiError) {
    if (e.code === "step_up_required" || e.code === "step_up_invalid") {
      return t("settings.account.errors.sessionExpired");
    }
    if (e.status === 401) return t("settings.account.errors.wrongPassword");
    if (e.status === 429) return t("settings.account.errors.rateLimited");
    if (e.status === 0) return t("settings.account.errors.network");
    return t("settings.account.errors.generic");
  }
  return e instanceof Error ? e.message : String(e);
}

// ----- Change password -----
const oldPassword = ref("");
const newPassword = ref("");
const confirmPassword = ref("");
const changePasswordSubmitting = ref(false);
const changePasswordError = ref("");
const changePasswordSuccess = ref("");

const newPasswordLengthValid = computed(() => newPassword.value.length >= 8);
const newPasswordMatches = computed(
  () => confirmPassword.value !== "" && newPassword.value === confirmPassword.value,
);
const canSubmitChangePassword = computed(
  () =>
    oldPassword.value !== "" &&
    newPasswordLengthValid.value &&
    newPasswordMatches.value &&
    !changePasswordSubmitting.value,
);

async function onChangePasswordSubmit() {
  if (!canSubmitChangePassword.value) return;
  changePasswordError.value = "";
  changePasswordSuccess.value = "";
  changePasswordSubmitting.value = true;
  try {
    // Restored pre-stepup shape (see comment on `changePassword` in
    // web/src/shared/api/me.ts). Endpoint currently returns 410 on the live
    // relay; tracked as follow-up to re-introduce OPAQUE step-up here.
    await changePassword(oldPassword.value, newPassword.value);
    changePasswordSuccess.value = t("settings.account.changePassword.successToast");
    oldPassword.value = "";
    newPassword.value = "";
    confirmPassword.value = "";
  } catch (e) {
    changePasswordError.value = describeAccountError(e);
  } finally {
    changePasswordSubmitting.value = false;
  }
}

// ----- Danger zone -----
const dangerZoneExpanded = ref(false);
const dangerPassword = ref("");
const dangerEmailConfirm = ref("");
const deleteSubmitting = ref(false);
const deleteError = ref("");

const canDelete = computed(
  () =>
    dangerPassword.value !== "" &&
    email.value !== "" &&
    dangerEmailConfirm.value === email.value &&
    !deleteSubmitting.value,
);

function toggleDangerZone() {
  dangerZoneExpanded.value = !dangerZoneExpanded.value;
  if (!dangerZoneExpanded.value) {
    dangerPassword.value = "";
    dangerEmailConfirm.value = "";
    deleteError.value = "";
  }
}

async function onDeleteClick() {
  if (!canDelete.value) return;
  if (!window.confirm(t("settings.account.dangerZone.confirmPrompt"))) return;
  deleteError.value = "";
  deleteSubmitting.value = true;
  try {
    await deleteMe(email.value, dangerPassword.value);
    // Hard-navigate to the login page. /login.html only exists in the web
    // bundle (Wails doesn't serve it), but this tab is gated on
    // !caps.wailsBindings in SettingsDialog.vue — so the surrounding
    // SettingsDialog can only render this button in the browser build,
    // where the URL resolves. Full-page navigation is the intended reset:
    // it clears every in-memory ref + closes any WS to the (now-deleted)
    // account.
    location.assign("/login.html");
  } catch (e) {
    deleteError.value = describeAccountError(e);
  } finally {
    deleteSubmitting.value = false;
  }
}
</script>

<template>
  <div class="tab-pane account-tab">
    <form
      v-if="!authenticated && canInlineLogin"
      class="account-section inline-login"
      data-testid="account-inline-login"
      @submit.prevent="onLoginSubmit"
    >
      <h3>{{ t("mobile.loginButton") }}</h3>
      <button
        v-if="canPairByQR"
        type="button"
        class="secondary primary-action"
        data-testid="account-scan-qr"
        :disabled="loginSubmitting || !!scannedPairingUrl"
        @click="onScanQR"
      >
        {{ t("mobile.pairing.scan") }}
      </button>
      <SettingsPairingConsume
        v-if="scannedPairingUrl"
        :scanned-url="scannedPairingUrl"
        :allow-insecure="loginAllowInsecure"
        @connected="onPairingConnected"
        @cancel="scannedPairingUrl = ''"
      />
      <label class="field-label" for="account-relay-url">{{ t("mobile.relayUrl") }}</label>
      <div class="relay-url-row">
        <select
          v-model="loginScheme"
          data-testid="account-login-scheme"
          :disabled="loginSubmitting"
          @change="loginScheme === 'https://' ? loginAllowInsecure = false : undefined"
        >
          <option value="https://">https://</option>
          <option value="http://">http://</option>
        </select>
        <input
          id="account-relay-url"
          v-model="loginHost"
          data-testid="account-login-url"
          :disabled="loginSubmitting"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          placeholder="relay.example.com"
          @input="normalizeLoginHost"
        />
      </div>
      <label v-if="loginScheme === 'http://'" class="checkbox-row">
        <span>{{ t("mobile.allowInsecure") }}</span>
        <input
          v-model="loginAllowInsecure"
          data-testid="account-login-allow-insecure"
          :disabled="loginSubmitting"
          type="checkbox"
        />
      </label>
      <label class="field-label" for="account-login-email">{{ t("mobile.email") }}</label>
      <input
        id="account-login-email"
        v-model="loginEmail"
        data-testid="account-login-email"
        :disabled="loginSubmitting"
        type="email"
        autocomplete="username"
        autocapitalize="off"
        spellcheck="false"
      />
      <label class="field-label" for="account-login-password">{{ t("mobile.password") }}</label>
      <input
        id="account-login-password"
        v-model="loginPassword"
        data-testid="account-login-password"
        :disabled="loginSubmitting"
        type="password"
        autocomplete="current-password"
      />
      <p v-if="loginError" class="inline-error" role="alert">{{ loginError }}</p>
      <button
        type="submit"
        class="secondary primary-action"
        data-testid="account-login-submit"
        :disabled="loginSubmitting"
      >
        {{ loginSubmitting ? t("common.loading") : t("mobile.loginButton") }}
      </button>
    </form>

    <template v-else>
    <section class="account-section">
      <label class="field-label">{{ t("settings.account.email") }}</label>
      <p class="email-value" data-testid="account-email">
        {{ emailLoading ? t("common.loading") : email }}
      </p>
      <button
        type="button"
        class="secondary"
        data-testid="account-logout-button"
        :disabled="logoutSubmitting"
        @click="onLogoutClick"
      >
        {{
          logoutSubmitting
            ? t("settings.account.logout.submitting")
            : t("settings.account.logout.button")
        }}
      </button>
    </section>

    <section class="account-section">
      <h3>{{ t("settings.account.changePassword.title") }}</h3>

      <label class="field-label" for="account-old-password">{{
        t("settings.account.changePassword.oldPassword")
      }}</label>
      <input
        id="account-old-password"
        v-model="oldPassword"
        data-testid="old-password-input"
        type="password"
        autocomplete="current-password"
        :disabled="changePasswordSubmitting"
      />

      <label class="field-label" for="account-new-password">{{
        t("settings.account.changePassword.newPassword")
      }}</label>
      <input
        id="account-new-password"
        v-model="newPassword"
        data-testid="new-password-input"
        type="password"
        autocomplete="new-password"
        :disabled="changePasswordSubmitting"
      />
      <p v-if="newPassword && !newPasswordLengthValid" class="hint">
        {{ t("settings.account.changePassword.hintLength") }}
      </p>

      <label class="field-label" for="account-confirm-password">{{
        t("settings.account.changePassword.confirmPassword")
      }}</label>
      <input
        id="account-confirm-password"
        v-model="confirmPassword"
        data-testid="confirm-password-input"
        type="password"
        autocomplete="new-password"
        :disabled="changePasswordSubmitting"
        @keyup.enter="onChangePasswordSubmit"
      />
      <p v-if="confirmPassword && !newPasswordMatches" class="hint">
        {{ t("settings.account.changePassword.hintMismatch") }}
      </p>

      <button
        type="button"
        class="secondary"
        data-testid="change-password-submit"
        :disabled="!canSubmitChangePassword"
        @click="onChangePasswordSubmit"
      >
        {{
          changePasswordSubmitting
            ? t("settings.account.changePassword.submitting")
            : t("settings.account.changePassword.submit")
        }}
      </button>
      <p v-if="changePasswordError" class="error">{{ changePasswordError }}</p>
      <p v-if="changePasswordSuccess" class="success">{{ changePasswordSuccess }}</p>
    </section>

    <section class="account-section danger-zone">
      <h3>{{ t("settings.account.dangerZone.title") }}</h3>
      <p class="hint warning-text">{{ t("settings.account.dangerZone.warning") }}</p>

      <button
        type="button"
        class="secondary"
        data-testid="danger-zone-toggle"
        @click="toggleDangerZone"
      >
        {{ t("settings.account.dangerZone.toggle") }}
      </button>

      <template v-if="dangerZoneExpanded">
        <label class="field-label" for="danger-current-password">{{
          t("settings.account.dangerZone.currentPassword")
        }}</label>
        <input
          id="danger-current-password"
          v-model="dangerPassword"
          data-testid="danger-current-password"
          type="password"
          autocomplete="current-password"
          :disabled="deleteSubmitting"
        />

        <label class="field-label" for="danger-confirm-email">{{
          t("settings.account.dangerZone.typeEmailToConfirm")
        }}</label>
        <input
          id="danger-confirm-email"
          v-model="dangerEmailConfirm"
          data-testid="danger-confirm-email"
          type="text"
          autocomplete="off"
          spellcheck="false"
          :disabled="deleteSubmitting"
        />

        <button
          type="button"
          class="danger-btn"
          data-testid="danger-delete-button"
          :disabled="!canDelete"
          @click="onDeleteClick"
        >
          {{
            deleteSubmitting
              ? t("settings.account.dangerZone.deleting")
              : t("settings.account.dangerZone.deleteButton")
          }}
        </button>
        <p v-if="deleteError" class="error">{{ deleteError }}</p>
      </template>
    </section>
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.account-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.account-section:last-child {
  border-bottom: none;
  padding-bottom: 0;
}
.account-section h3 {
  margin: 0 0 4px;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-dim);
}
.field-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.email-value {
  margin: 0;
  color: var(--fg);
  font-size: 13px;
}
.hint {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.warning-text {
  color: var(--bad);
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.success {
  color: var(--good);
  font-size: 12px;
  margin: 0;
}
.relay-url-row {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 8px;
}
.checkbox-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--fg-dim);
  font-size: 12px;
}
.checkbox-row input {
  flex: 0 0 auto;
}
.inline-error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
input[type="text"],
input[type="email"],
select,
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
button.secondary {
  align-self: flex-start;
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
button.secondary:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
button.secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
button.primary-action {
  background: var(--accent);
  border-color: var(--accent);
  color: #0d1117;
  font-weight: 600;
}
button.primary-action:hover:not(:disabled) {
  background: #79b8ff;
  color: #0d1117;
}
.danger-btn {
  align-self: flex-start;
  background: transparent;
  color: var(--bad);
  border: 1px solid var(--bad);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
.danger-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bad) 8%, transparent 92%);
}
.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
