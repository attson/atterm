<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  getCommandNotifyThresholdSeconds,
  getDefaultShell,
  getLocalePreference,
  getNotificationsEnabled,
  getRecoveryDialogEnabled,
  getShellIntegrationEnabled,
  getWebglRendererEnabled,
  listShells,
  setCommandNotifyThresholdSeconds,
  setDefaultShell,
  setNotificationsEnabled,
  setRecoveryDialogEnabled,
  setShellIntegrationEnabled,
  setWebglRendererEnabled,
  setTerminalThemePreference,
} from "../lib/api";
import { type LocalePreference, type MessageKey } from "../i18n";
import { useI18n } from "../i18n/useI18n";
import { TERMINAL_THEMES, getTerminalTheme } from "../lib/terminalThemes";
import SelectDropdown from "./SelectDropdown.vue";
import SettingsTerminalAppearance, { type TerminalAppearanceState } from "./SettingsTerminalAppearance.vue";
import { usePlatform } from "../platform";
import { enablePushFlow, disablePushFlow, type EnableReason } from "@shared/api/push-flow";
import { testPush } from "@shared/api/push";

const props = defineProps<{
  terminalThemeId: string;
}>();

const emit = defineEmits<{
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
  (e: "appearance-changed", state: TerminalAppearanceState): void;
}>();

const selected = ref(getTerminalTheme(props.terminalThemeId).id);
const persisted = ref(selected.value);
const saving = ref(false);
const error = ref("");
const { t, languageOptions, localePreference, setLocalePreference: setRuntimeLocalePreference } = useI18n();
const platform = usePlatform();
const caps = platform.caps;

const selectedLocale = ref<LocalePreference>(localePreference.value);
const persistedLocale = ref<LocalePreference>(localePreference.value);
const localeSaving = ref(false);

const localizedLanguageOptions = computed(() =>
  languageOptions.map((option) => ({
    value: option.value,
    label: t(option.labelKey),
  })),
);

const themeOptions = computed(() =>
  TERMINAL_THEMES.map((theme) => ({
    value: theme.id,
    label: t(theme.labelKey),
    description: t(theme.descriptionKey),
  })),
);

const notificationsEnabled = ref(true);
const notificationsLoading = ref(true);
const shellIntegrationEnabled = ref(true);
const shellIntegrationLoading = ref(true);
const recoveryEnabled = ref(true);
const recoveryEnabledLoading = ref(true);
const defaultShellLoading = ref(true);
const defaultShellSaving = ref(false);
const selectedDefaultShell = ref("auto");
const persistedDefaultShell = ref("auto");
const customShellPath = ref("");
const availableShells = ref<string[]>([]);
const webglRendererEnabled = ref(true);
const webglRendererLoading = ref(true);
const commandNotifyThresholdSec = ref(10);
const commandNotifyThresholdLoading = ref(true);

// Wails also sets caps.notifications=true (system notifications), but has
// no Service Worker at all, so browser Push only ever makes sense when
// both the platform capability AND a real navigator.serviceWorker are
// present. Capacitor has its own native push channel and doesn't run a
// Service Worker either, so this gate excludes it too without needing a
// dedicated cap.
const pushSupported = computed(
  () => caps.notifications && typeof navigator !== "undefined" && "serviceWorker" in navigator,
);
const pushEnabled = ref(false);
const pushLoading = ref(true);
const pushBusy = ref(false);
const pushTestBusy = ref(false);
const pushError = ref("");

const PUSH_ERROR_KEY: Record<EnableReason, MessageKey> = {
  denied: "settings.general.pushNotifications.errors.denied",
  disabled: "settings.general.pushNotifications.errors.disabled",
  "key-failed": "settings.general.pushNotifications.errors.keyFailed",
  "subscribe-failed": "settings.general.pushNotifications.errors.subscribeFailed",
  "subscribe-rejected": "settings.general.pushNotifications.errors.subscribeRejected",
};

const defaultShellOptions = computed(() => [
  { value: "auto", label: t("settings.general.auto") },
  ...availableShells.value.map((shell) => ({ value: shell, label: shell })),
  { value: "__custom__", label: t("settings.general.customPath") },
]);

// Shared by onMounted and the prefs:changed listener below (Task 4 fix
// round 2) so a remote pull and a fresh mount observe Go the same way —
// mirrors SettingsTerminalAppearance.vue's loadAppearance(). Read-only:
// assigns refs from getDefaultShell()/listShells() and nothing else. Must
// never call setDefaultShell — writing back a value we just pulled would
// push it straight back out, and the other device would pull it, write it
// back, push it again, forever (design §7.2). Go itself already reads the
// synced value fresh at spawn time (app.go DefaultShellOrDefault), so this
// function only needs to keep the *panel display* from going stale — it
// isn't what makes a remote shell change take effect.
async function loadDefaultShell() {
  if (!caps.wailsBindings) return;
  try {
    const [configured, shells] = await Promise.all([getDefaultShell(), listShells()]);
    availableShells.value = shells;
    if (configured === "auto") {
      selectedDefaultShell.value = "auto";
    } else if (shells.includes(configured)) {
      selectedDefaultShell.value = configured;
      customShellPath.value = configured;
    } else {
      selectedDefaultShell.value = "__custom__";
      customShellPath.value = configured;
    }
    persistedDefaultShell.value = selectedDefaultShell.value;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    defaultShellLoading.value = false;
  }
}

// default_shell has no draft/Save/Discard flow (unlike SettingsShortcuts.vue,
// which is why that panel does not get a live listener): the dropdown
// commits on `change` via onDefaultShellChange, and the one text field
// (customShellPath, used only while "__custom__" is selected) already
// accepts being overwritten by a reload the same way
// SettingsTerminalAppearance.vue's scrollback field does mid-keystroke —
// an accepted, pre-existing trade in this design, not a new one.
let prefsChangedOff: (() => void) | null = null;
onMounted(() => {
  prefsChangedOff = platform.events.on("prefs:changed", () => {
    void loadDefaultShell();
  });
});
onBeforeUnmount(() => {
  prefsChangedOff?.();
  prefsChangedOff = null;
});

onMounted(async () => {
  if (caps.wailsBindings) {
    try {
      const preference = await getLocalePreference();
      selectedLocale.value = preference;
      persistedLocale.value = preference;
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    }
  }
  if (caps.wailsBindings) {
    try {
      notificationsEnabled.value = await getNotificationsEnabled();
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    } finally {
      notificationsLoading.value = false;
    }
    try {
      shellIntegrationEnabled.value = await getShellIntegrationEnabled();
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    } finally {
      shellIntegrationLoading.value = false;
    }
    try {
      recoveryEnabled.value = await getRecoveryDialogEnabled();
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    } finally {
      recoveryEnabledLoading.value = false;
    }
    await loadDefaultShell();
    try {
      webglRendererEnabled.value = await getWebglRendererEnabled();
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    } finally {
      webglRendererLoading.value = false;
    }
    try {
      commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
    } catch (e: any) {
      error.value = e?.message ?? String(e);
    } finally {
      commandNotifyThresholdLoading.value = false;
    }
  }
  await refreshPushState();
});

async function refreshPushState() {
  if (!pushSupported.value) {
    pushLoading.value = false;
    return;
  }
  try {
    if (!navigator.serviceWorker.controller) {
      pushEnabled.value = false;
      return;
    }
    const registration = await navigator.serviceWorker.ready;
    const subscription = await registration.pushManager.getSubscription();
    pushEnabled.value = subscription !== null;
  } catch {
    pushEnabled.value = false;
  } finally {
    pushLoading.value = false;
  }
}

async function onPushToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const checked = target.checked;
  pushError.value = "";
  pushBusy.value = true;
  try {
    const registration = await navigator.serviceWorker.ready;
    if (checked) {
      const result = await enablePushFlow({
        notification: {
          permission: Notification.permission,
          requestPermission: () => Notification.requestPermission(),
        },
        registration,
      });
      if (!result.ok) {
        target.checked = false;
        pushEnabled.value = false;
        pushError.value = t(PUSH_ERROR_KEY[result.reason] ?? "settings.general.pushNotifications.errors.generic");
        return;
      }
      pushEnabled.value = true;
    } else {
      await disablePushFlow({ registration });
      pushEnabled.value = false;
    }
  } catch (e: any) {
    target.checked = pushEnabled.value;
    pushError.value = e?.message ?? String(e);
  } finally {
    pushBusy.value = false;
  }
}

async function onTestPush() {
  pushError.value = "";
  pushTestBusy.value = true;
  try {
    await testPush();
  } catch (e: any) {
    pushError.value = e?.message ?? String(e);
  } finally {
    pushTestBusy.value = false;
  }
}

async function onNotificationsToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = notificationsEnabled.value;
  notificationsEnabled.value = target.checked;
  error.value = "";
  try {
    await setNotificationsEnabled(target.checked);
  } catch (e: any) {
    notificationsEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onShellIntegrationToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = shellIntegrationEnabled.value;
  shellIntegrationEnabled.value = target.checked;
  error.value = "";
  try {
    await setShellIntegrationEnabled(target.checked);
  } catch (e: any) {
    shellIntegrationEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onRecoveryEnabledToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = recoveryEnabled.value;
  recoveryEnabled.value = target.checked;
  error.value = "";
  try {
    await setRecoveryDialogEnabled(target.checked);
  } catch (e: any) {
    recoveryEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onDefaultShellChange() {
  if (selectedDefaultShell.value === "__custom__") return;
  const previous = persistedDefaultShell.value;
  defaultShellSaving.value = true;
  error.value = "";
  try {
    await setDefaultShell(selectedDefaultShell.value);
    persistedDefaultShell.value = selectedDefaultShell.value;
  } catch (e: any) {
    selectedDefaultShell.value = previous;
    error.value = e?.message ?? String(e);
  } finally {
    defaultShellSaving.value = false;
  }
}

async function onCustomShellSave() {
  const next = customShellPath.value.trim();
  if (!next) {
    error.value = t("settings.general.customShellRequired");
    return;
  }
  defaultShellSaving.value = true;
  error.value = "";
  try {
    await setDefaultShell(next);
    selectedDefaultShell.value = "__custom__";
    persistedDefaultShell.value = "__custom__";
    if (!availableShells.value.includes(next)) {
      availableShells.value = [next, ...availableShells.value];
    }
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    defaultShellSaving.value = false;
  }
}

async function onWebglRendererToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = webglRendererEnabled.value;
  webglRendererEnabled.value = target.checked;
  error.value = "";
  try {
    await setWebglRendererEnabled(target.checked);
  } catch (e: any) {
    webglRendererEnabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onCommandNotifyThresholdChange(e: Event) {
  const target = e.target as HTMLInputElement;
  const raw = Number(target.value);
  const next = Number.isFinite(raw) ? Math.max(1, Math.min(600, Math.round(raw))) : 10;
  const previous = commandNotifyThresholdSec.value;
  commandNotifyThresholdSec.value = next;
  target.value = String(next);
  error.value = "";
  try {
    await setCommandNotifyThresholdSeconds(next);
    emit("command-notify-threshold-changed", next);
  } catch (e: any) {
    commandNotifyThresholdSec.value = previous;
    target.value = String(previous);
    error.value = e?.message ?? String(e);
  }
}

// SettingsTerminalAppearance owns the font/size/line-height/cursor/scrollback
// state and persistence; we just re-emit its event so App.vue (Task 4) keeps
// seeing "appearance-changed" on this component, same as before the split.
function onAppearanceChanged(state: TerminalAppearanceState) {
  emit("appearance-changed", state);
}

async function onLocaleChange() {
  const next = selectedLocale.value;
  const previous = persistedLocale.value;
  const previousRuntime = localePreference.value;
  localeSaving.value = true;
  error.value = "";
  try {
    await setRuntimeLocalePreference(next);
    persistedLocale.value = next;
  } catch (e: any) {
    selectedLocale.value = previous;
    persistedLocale.value = previous;
    try {
      await setRuntimeLocalePreference(previousRuntime);
    } catch {
      // Keep the original save error visible; runtime setter already rolls back on failure.
    }
    error.value = e?.message ?? String(e);
  } finally {
    localeSaving.value = false;
  }
}

async function onChange() {
  const nextTheme = getTerminalTheme(selected.value).id;
  const previous = persisted.value;
  selected.value = nextTheme;
  saving.value = true;
  error.value = "";
  try {
    if (caps.wailsBindings) {
      await setTerminalThemePreference(nextTheme);
    }
    persisted.value = nextTheme;
    emit("terminal-theme-changed", nextTheme);
  } catch (e: any) {
    selected.value = previous;
    emit("terminal-theme-changed", previous);
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="tab-pane">
    <label class="field-label">{{ t("settings.general.languageLabel") }}</label>
    <SelectDropdown
      v-model="selectedLocale"
      :options="localizedLanguageOptions"
      :disabled="localeSaving"
      :aria-label="t('settings.general.languageLabel')"
      @update:modelValue="onLocaleChange"
    />
    <p class="hint">{{ t("settings.general.languageHint") }}</p>

    <label class="field-label">{{ t("settings.general.terminalTheme") }}</label>
    <SelectDropdown
      v-model="selected"
      :options="themeOptions"
      :disabled="saving"
      :aria-label="t('settings.general.terminalTheme')"
      @update:modelValue="onChange"
    />
    <p class="hint">
      {{ t("settings.general.terminalThemeHint") }}
    </p>

    <SettingsTerminalAppearance @appearance-changed="onAppearanceChanged" />

    <label class="checkbox" v-if="caps.wailsBindings && !notificationsLoading">
      <input
        type="checkbox"
        :checked="notificationsEnabled"
        @change="onNotificationsToggle"
      />
      {{ t("settings.general.notificationsBell") }}
    </label>
    <p class="hint" v-if="caps.wailsBindings && !notificationsLoading">
      {{ t("settings.general.notificationsHint") }}
    </p>

    <label class="checkbox" v-if="caps.wailsBindings && !shellIntegrationLoading">
      <input
        type="checkbox"
        :checked="shellIntegrationEnabled"
        @change="onShellIntegrationToggle"
      />
      {{ t("settings.general.shellIntegration") }}
    </label>
    <p class="hint" v-if="caps.wailsBindings && !shellIntegrationLoading">
      {{ t("settings.general.shellIntegrationHint") }}
    </p>

    <label class="checkbox" v-if="caps.wailsBindings && !recoveryEnabledLoading">
      <input
        type="checkbox"
        :checked="recoveryEnabled"
        @change="onRecoveryEnabledToggle"
      />
      {{ t("settings.general.recoveryEnabled") }}
    </label>
    <p class="hint" v-if="caps.wailsBindings && !recoveryEnabledLoading">
      {{ t("settings.general.recoveryEnabledHint") }}
    </p>

    <label class="field-label" v-if="caps.wailsBindings && !defaultShellLoading">{{ t("settings.general.defaultShell") }}</label>
    <SelectDropdown
      v-if="caps.wailsBindings && !defaultShellLoading"
      v-model="selectedDefaultShell"
      :options="defaultShellOptions"
      :aria-label="t('settings.general.defaultShell')"
      :disabled="defaultShellSaving"
      @update:modelValue="onDefaultShellChange"
    />
    <p class="hint" v-if="caps.wailsBindings && !defaultShellLoading">
      {{ t("settings.general.defaultShellHint") }}
    </p>
    <div v-if="caps.wailsBindings && !defaultShellLoading && selectedDefaultShell === '__custom__'" class="custom-shell-row">
      <input
        class="text-input"
        v-model="customShellPath"
        :placeholder="t('settings.general.customShellPath')"
        :aria-label="t('settings.general.customShellPath')"
        :disabled="defaultShellSaving"
      />
      <button :disabled="defaultShellSaving" @click="onCustomShellSave">{{ t("settings.general.saveCustom") }}</button>
    </div>

    <label class="checkbox" v-if="caps.wailsBindings && !webglRendererLoading">
      <input
        type="checkbox"
        :checked="webglRendererEnabled"
        @change="onWebglRendererToggle"
      />
      {{ t("settings.general.webglRenderer") }}
    </label>
    <p class="hint" v-if="caps.wailsBindings && !webglRendererLoading">
      {{ t("settings.general.webglHint") }}
    </p>

    <label class="field-label" v-if="caps.wailsBindings && !commandNotifyThresholdLoading">
      {{ t("settings.general.commandNotifyThreshold") }}
    </label>
    <input
      v-if="caps.wailsBindings && !commandNotifyThresholdLoading"
      class="number-input"
      type="number"
      min="1"
      max="600"
      :value="commandNotifyThresholdSec"
      @change="onCommandNotifyThresholdChange"
    />
    <p class="hint" v-if="caps.wailsBindings && !commandNotifyThresholdLoading">
      {{ t("settings.general.commandNotifyHint") }}
    </p>

    <p v-if="error" class="error">{{ error }}</p>

    <section v-if="pushSupported" class="push-section" data-testid="push-section">
      <h3 class="section-title">{{ t("settings.general.pushNotifications.title") }}</h3>
      <label class="checkbox" v-if="!pushLoading">
        <input
          type="checkbox"
          data-testid="push-toggle"
          :checked="pushEnabled"
          :disabled="pushBusy"
          @change="onPushToggle"
        />
        {{ t("settings.general.pushNotifications.enable") }}
      </label>
      <button
        v-if="pushEnabled"
        type="button"
        class="push-test-button"
        data-testid="push-test"
        :disabled="pushTestBusy"
        @click="onTestPush"
      >
        {{ t("settings.general.pushNotifications.test") }}
      </button>
      <p v-if="pushError" class="error" data-testid="push-error">{{ pushError }}</p>
    </section>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.field-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.hint {
  font-size: 12px;
  color: var(--fg-dim);
  margin: 0;
  line-height: 1.5;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.number-input {
  width: 80px;
  padding: 4px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
}
.text-input {
  padding: 6px 8px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
}
.custom-shell-row {
  display: flex;
  gap: 8px;
}
.custom-shell-row .text-input {
  flex: 1 1 auto;
}
.custom-shell-row button {
  padding: 6px 10px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
}
.push-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.section-title {
  margin: 0;
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.push-test-button {
  align-self: flex-start;
  padding: 6px 10px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--fg);
  font: inherit;
  cursor: pointer;
}
.push-test-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
