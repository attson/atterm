<script lang="ts" setup>
import { computed, onMounted, ref, watch } from "vue";
import {
  getLogPreview,
  getTerminalThemePreference,
  installUpdate,
  type LogPreview,
} from "../lib/api";
import { getTerminalTheme } from "../lib/terminalThemes";
import { usePlatform } from "../platform";
import type { TerminalAppearance } from "../lib/types";
import SettingsGeneral from "./SettingsGeneral.vue";
import type { TerminalAppearanceState } from "./SettingsTerminalAppearance.vue";
import SettingsAccount from "./SettingsAccount.vue";
import SettingsRelay from "./SettingsRelay.vue";
import SettingsLogging from "./SettingsLogging.vue";
import SettingsUpdates from "./SettingsUpdates.vue";
import SettingsPlugins from "./SettingsPlugins.vue";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import SettingsDiagnostics from "./SettingsDiagnostics.vue";
import SettingsConfigIO from "./SettingsConfigIO.vue";
import SettingsTemplates from "./SettingsTemplates.vue";
import SettingsProfiles from "./SettingsProfiles.vue";
import SettingsTasks from "./SettingsTasks.vue";
import SettingsFeishu from "./SettingsFeishu.vue";
import SettingsDevices from "./SettingsDevices.vue";
import SettingsReceivedFiles from "./SettingsReceivedFiles.vue";
import SettingsProfilesMobile from "./SettingsProfilesMobile.vue";
import SettingsSSHHostsMobile from "./SettingsSSHHostsMobile.vue";
import SyncStatusIndicator from "./SyncStatusIndicator.vue";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";
import LogViewerDialog from "./LogViewerDialog.vue";
import { useI18n } from "../i18n/useI18n";
import type { MessageKey } from "../i18n";

const platform = usePlatform();
const caps = platform.caps;
const { t, resolvedLocale } = useI18n();

// Tab heading metadata: i18n key + English subtitle shown under H2 when the
// UI is in Chinese (CodeIsland-style "通用 General preferences" anchor).
// English locale skips the subtitle to avoid duplicate text.
type SettingsTabId = "general" | "account" | "tasks" | "relay" | "plugins"
  | "shortcuts" | "templates" | "profiles" | "logging" | "updates" | "diagnostics" | "feishu" | "devices" | "received-files"
  | "mobile-profiles" | "mobile-hosts";

const tabMeta: Record<SettingsTabId, { labelKey: MessageKey; english: string }> = {
  general:     { labelKey: "settings.tabs.general",        english: "General preferences" },
  account:     { labelKey: "settings.account.title",       english: "Account" },
  tasks:       { labelKey: "tasks.settings.section",       english: "Tasks display" },
  relay:       { labelKey: "settings.tabs.relay",          english: "Relay" },
  plugins:     { labelKey: "settings.tabs.plugins",        english: "Plugins" },
  shortcuts:   { labelKey: "settings.tabs.shortcuts",      english: "Keyboard shortcuts" },
  templates:   { labelKey: "settings.templates.tab",       english: "Quick templates" },
  profiles:    { labelKey: "settings.profiles.tab",        english: "Session profiles" },
  logging:     { labelKey: "settings.tabs.logging",        english: "Logging" },
  updates:     { labelKey: "settings.tabs.updates",        english: "Updates" },
  diagnostics: { labelKey: "settings.diagnostics.tab",     english: "Diagnostics" },
  feishu:      { labelKey: "settings.feishu.title",        english: "Feishu integration" },
  devices:     { labelKey: "settings.tabs.devices",       english: "Signed-in devices" },
  "received-files": { labelKey: "settings.tabs.receivedFiles", english: "Received files" },
  "mobile-profiles": { labelKey: "settings.tabs.mobileProfiles", english: "Synced profiles" },
  "mobile-hosts":    { labelKey: "settings.tabs.mobileHosts",    english: "SSH hosts" },
};

const activeTabLabel = computed(() => t(tabMeta[activeTab.value].labelKey));
const activeTabEnglish = computed(() =>
  resolvedLocale.value === "zh-CN" ? tabMeta[activeTab.value].english : ""
);

// 14px stroke-1.6 SVG icons for the sidebar. Inline as v-html — content
// is local & trusted, no XSS surface. Kept minimal so the bundle does
// not pull in a full icon library.
const icoBase = 'viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"';
const tabIcons: Record<SettingsTabId, string> = {
  general:     `<svg ${icoBase}><circle cx="8" cy="8" r="2.2"/><path d="M8 1.6v2M8 12.4v2M14.4 8h-2M3.6 8h-2M12.5 3.5l-1.4 1.4M4.9 11.1l-1.4 1.4M12.5 12.5l-1.4-1.4M4.9 4.9 3.5 3.5"/></svg>`,
  account:     `<svg ${icoBase}><circle cx="8" cy="5.6" r="2.6"/><path d="M3 14c0-2.8 2.2-4.6 5-4.6s5 1.8 5 4.6"/></svg>`,
  tasks:       `<svg ${icoBase}><path d="M3 4h10M3 8h10M3 12h6"/></svg>`,
  relay:       `<svg ${icoBase}><circle cx="8" cy="8" r="1.4"/><path d="M4.4 4.4a5 5 0 0 0 0 7.2M11.6 11.6a5 5 0 0 0 0-7.2M2.4 2.4a8 8 0 0 0 0 11.2M13.6 13.6a8 8 0 0 0 0-11.2"/></svg>`,
  plugins:     `<svg ${icoBase}><path d="M5 2v2.5H2.5V8H5v3.5h3.5V14H12V11.5h2V8h-2.5V4.5H8.5V2z"/></svg>`,
  shortcuts:   `<svg ${icoBase}><rect x="1.6" y="4.2" width="12.8" height="7.6" rx="1.6"/><path d="M4 7h.01M6.5 7h.01M9 7h.01M11.5 7h.01M4.5 9.5h7"/></svg>`,
  templates:   `<svg ${icoBase}><rect x="2.4" y="2.4" width="11.2" height="11.2" rx="1.4"/><path d="M2.4 6h11.2M6 6v7.6"/></svg>`,
  profiles:    `<svg ${icoBase}><rect x="1.6" y="3" width="12.8" height="10" rx="1.4"/><path d="M4.2 6.6h4.4M4.2 9h7.6"/><circle cx="11.4" cy="6.6" r="0.9" fill="currentColor" stroke="none"/></svg>`,
  logging:     `<svg ${icoBase}><path d="M3 2.5h7l3 3v8H3z"/><path d="M10 2.5v3h3M5 8.5h6M5 11h4"/></svg>`,
  updates:     `<svg ${icoBase}><path d="M2.5 8a5.5 5.5 0 0 1 9.7-3.5L14 6.5"/><path d="M14 2.5v4h-4"/><path d="M13.5 8a5.5 5.5 0 0 1-9.7 3.5L2 9.5"/><path d="M2 13.5v-4h4"/></svg>`,
  diagnostics: `<svg ${icoBase}><circle cx="7.2" cy="7.2" r="4.4"/><path d="m10.4 10.4 3 3"/></svg>`,
  feishu:      `<svg ${icoBase}><path d="M3.5 4.5h6l2 2v5a1.5 1.5 0 0 1-1.5 1.5h-6.5a1 1 0 0 1-1-1V5.5a1 1 0 0 1 1-1Z"/><path d="M9.5 4.5v2h2"/></svg>`,
  devices:     `<svg ${icoBase}><rect x="1.6" y="2.4" width="12.8" height="8" rx="1.4"/><path d="M4.8 14h6.4M8 10.4V14"/></svg>`,
  "received-files": `<svg ${icoBase}><path d="M3 3.5h4l1.5 2H13v6.5H3z"/><path d="M8 8v3.5M6 10l2 2 2-2"/></svg>`,
  "mobile-profiles": `<svg ${icoBase}><rect x="1.6" y="3" width="12.8" height="10" rx="1.4"/><path d="M4.2 6.6h4.4M4.2 9h7.6"/><circle cx="11.4" cy="6.6" r="0.9" fill="currentColor" stroke="none"/></svg>`,
  "mobile-hosts":    `<svg ${icoBase}><circle cx="8" cy="8" r="1.4"/><path d="M4.4 4.4a5 5 0 0 0 0 7.2M11.6 11.6a5 5 0 0 0 0-7.2"/></svg>`,
};

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
  initialTab?: "general" | "account" | "relay" | "logging" | "updates" | "shortcuts" | "diagnostics" | "templates" | "profiles" | "tasks" | "feishu" | "devices";
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
  (e: "appearance-changed", appearance: TerminalAppearance): void;
  (e: "bindings-changed", bindings: Record<string, string>): void;
  (e: "profiles-changed"): void;
  (e: "session-created", sessionId: string): void;
}>();

// Logging is no longer a standalone tab — it lives inside the Diagnostics
// pane. Map any legacy `initialTab: 'logging'` onto diagnostics so deep links
// keep working.
const initialTab = props.initialTab === "logging" ? "diagnostics" : (props.initialTab ?? "general");
const activeTab = ref<"general" | "account" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "profiles" | "tasks" | "feishu" | "devices" | "received-files" | "mobile-profiles" | "mobile-hosts">(initialTab);

const hiddenTabs = new Set<string>()
if (!caps.autoUpdate) hiddenTabs.add('updates')
if (!caps.pluginHost) { hiddenTabs.add('plugins'); hiddenTabs.add('shortcuts') }
// SettingsAccount is for browser/Capacitor account state. On Wails the
// desktop relay config owns auth, so this tab is hidden.
if (caps.wailsBindings) hiddenTabs.add('account')
// Relay / Diagnostics / Received files / Feishu are desktop-only concerns
// (local relay config, Wails-side log/env dumps, local filesystem drops,
// desktop uplink) that don't apply once the client is a browser tab.
if (!caps.wailsBindings) {
  hiddenTabs.add('relay')
  hiddenTabs.add('diagnostics')
  hiddenTabs.add('received-files')
  hiddenTabs.add('feishu')
  // Session profiles (roadmap item 22) are a desktop-only concept — they
  // launch a local shell (desktop/relay_host.go's NewSession), which web /
  // Capacitor never do. Same reasoning as relay/diagnostics/feishu above.
  hiddenTabs.add('profiles')
}
// Read-only mobile views of the desktop's profiles/SSH hosts (design doc
// §2/§6) only make sense inside the Capacitor native wrapper: caps.capacitor
// is false on both Wails desktop and a plain browser tab (web/vite.config.ts
// aliases the web build's `@` onto this same src tree, so the browser tab
// would otherwise reach these tabs too — see SettingsProfilesMobile.vue's
// own gate comment).
if (!caps.capacitor) {
  hiddenTabs.add('mobile-profiles')
  hiddenTabs.add('mobile-hosts')
}
if (hiddenTabs.has(activeTab.value)) activeTab.value = 'general'

const persistedTheme = ref(getTerminalTheme(props.terminalThemeId).id);
// A remote prefs:changed pull can now update terminalThemeId out from under
// this dialog (App.vue's refreshTerminalTheme reassigns the prop-backing
// ref) — without this watcher persistedTheme would keep showing whatever was
// true when the dialog mounted until the user closed and reopened it.
watch(
  () => props.terminalThemeId,
  (id) => {
    persistedTheme.value = getTerminalTheme(id).id;
  },
);

const relayRef = ref<InstanceType<typeof SettingsRelay> | null>(null);
const relayDirty = ref(false);
const pendingTab = ref<"general" | "account" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "profiles" | "tasks" | "feishu" | "devices" | "received-files" | "mobile-profiles" | "mobile-hosts" | null>(null);
const showDiscardConfirm = ref(false);

const logPreview = ref<LogPreview | null>(null);
const logViewerError = ref("");
const logViewerLoading = ref(false);
const showLogViewer = ref(false);

const showConfirm = ref(false);
const updateVersionForConfirm = ref("");

const appVersion = ref("");
const versionLabel = computed(() => {
  const v = appVersion.value.trim();
  if (!v || v === "dev") return "AT Term (dev)";
  return `AT Term ${v.startsWith("v") ? v : `v${v}`}`;
});

onMounted(async () => {
  if (!caps.wailsBindings) return;
  try {
    const themeID = await getTerminalThemePreference();
    persistedTheme.value = getTerminalTheme(themeID).id;
  } catch {
    /* keep the prop value */
  }
});

onMounted(async () => {
  try {
    appVersion.value = await platform.system.getAppVersion();
  } catch {
    /* versionLabel falls back to the dev-build label */
  }
});

function switchTab(next: "general" | "account" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "profiles" | "tasks" | "feishu" | "devices" | "received-files" | "mobile-profiles" | "mobile-hosts") {
  if (activeTab.value === next) return;
  if (activeTab.value === "relay" && relayDirty.value) {
    pendingTab.value = next;
    showDiscardConfirm.value = true;
    return;
  }
  activeTab.value = next;
}

function onConfirmDiscard() {
  showDiscardConfirm.value = false;
  if (pendingTab.value) {
    activeTab.value = pendingTab.value;
    pendingTab.value = null;
  }
  relayDirty.value = false;
}

function onKeepEditing() {
  showDiscardConfirm.value = false;
  pendingTab.value = null;
}

function close() {
  if (relayRef.value?.saving) return;
  emit("close");
}

function onRelayDirty(value: boolean) {
  relayDirty.value = value;
}

function onRelayConfigChanged() {
  relayDirty.value = false;
  emit("relay-config-changed");
}

function onTerminalThemeChanged(themeID: string) {
  persistedTheme.value = getTerminalTheme(themeID).id;
  emit("terminal-theme-changed", themeID);
}

function onCommandNotifyThresholdChanged(seconds: number) {
  emit("command-notify-threshold-changed", seconds);
}

// SettingsGeneral re-emits SettingsTerminalAppearance's event unchanged (see
// its own onAppearanceChanged); this dialog is just a pass-through so App.vue
// doesn't need to know the settings tree got a level deeper. No cast needed:
// TerminalAppearanceState.cursorStyle is typed as the same union
// TerminalAppearance declares, so the two shapes already match structurally.
function onAppearanceChanged(state: TerminalAppearanceState) {
  emit("appearance-changed", state);
}

// SettingsShortcuts emits the full saved bindings map on save; pass it
// straight through so App.vue's useTerminalShortcuts stays in sync without
// waiting for the dialog to close and remount (see App.vue's onBindingsChanged).
function onBindingsChanged(bindings: Record<string, string>) {
  emit("bindings-changed", bindings);
}

// SettingsProfiles emits after a successful persist()/setDefault() so
// App.vue's picker can refresh independently of prefs:changed, which only
// fires when a Push to the relay actually succeeds (see SettingsProfiles.vue
// and App.vue's onProfilesChanged).
function onProfilesChanged() {
  emit("profiles-changed");
}

async function openLogViewer() {
  showLogViewer.value = true;
  await refreshLogViewer();
}

async function refreshLogViewer() {
  logViewerError.value = "";
  logViewerLoading.value = true;
  try {
    logPreview.value = await getLogPreview();
  } catch (e: any) {
    logViewerError.value = e?.message ?? String(e);
  } finally {
    logViewerLoading.value = false;
  }
}

function onForceInstallClick(version: string) {
  updateVersionForConfirm.value = version;
  showConfirm.value = true;
}

async function onConfirmInstall() {
  showConfirm.value = false;
  try {
    await installUpdate();
  } catch {
    /* state.error reflects in poll */
  }
}

function onSaveClick() {
  relayRef.value?.save();
}
</script>

<template>
  <div class="backdrop" @click.self="close">
    <div class="settings-dialog">
      <header class="settings-header">
        <h2>{{ t("settings.title") }}</h2>
        <!-- Desktop-only (design doc §6 "No mobile indicator"): SyncNow /
             GetSyncStatus are Wails-only bindings, and mobile syncs prefs
             over HTTP through a wholly separate path
             (lib/prefsSync.capacitor.ts), so there is nothing for this
             engine's status to report there. -->
        <SyncStatusIndicator v-if="caps.wailsBindings" class="header-sync-indicator" />
        <button class="close-btn" @click="close" :disabled="relayRef?.saving">×</button>
      </header>

      <div class="settings-body">
        <aside class="settings-nav">
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'general' }"
            @click="switchTab('general')"
          >
            <span class="nav-icon" v-html="tabIcons.general"></span>
            <span class="nav-label">{{ t("settings.tabs.general") }}</span>
          </button>
          <button
            v-if="!caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'account' }"
            @click="switchTab('account')"
          >
            <span class="nav-icon" v-html="tabIcons.account"></span>
            <span class="nav-label">{{ t("settings.account.title") }}</span>
          </button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'tasks' }"
            @click="switchTab('tasks')"
          >
            <span class="nav-icon" v-html="tabIcons.tasks"></span>
            <span class="nav-label">{{ t("tasks.settings.section") }}</span>
          </button>
          <button
            v-if="caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'relay' }"
            @click="switchTab('relay')"
          >
            <span class="nav-icon" v-html="tabIcons.relay"></span>
            <span class="nav-label">{{ t("settings.tabs.relay") }}</span>
          </button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'devices' }"
            @click="switchTab('devices')"
          >
            <span class="nav-icon" v-html="tabIcons.devices"></span>
            <span class="nav-label">{{ t("settings.tabs.devices") }}</span>
          </button>
          <button
            v-if="caps.capacitor"
            class="settings-nav-item"
            :class="{ active: activeTab === 'mobile-profiles' }"
            @click="switchTab('mobile-profiles')"
          >
            <span class="nav-icon" v-html="tabIcons['mobile-profiles']"></span>
            <span class="nav-label">{{ t("settings.tabs.mobileProfiles") }}</span>
          </button>
          <button
            v-if="caps.capacitor"
            class="settings-nav-item"
            :class="{ active: activeTab === 'mobile-hosts' }"
            @click="switchTab('mobile-hosts')"
          >
            <span class="nav-icon" v-html="tabIcons['mobile-hosts']"></span>
            <span class="nav-label">{{ t("settings.tabs.mobileHosts") }}</span>
          </button>
          <button
            v-if="caps.pluginHost"
            class="settings-nav-item"
            :class="{ active: activeTab === 'plugins' }"
            @click="switchTab('plugins')"
          >
            <span class="nav-icon" v-html="tabIcons.plugins"></span>
            <span class="nav-label">{{ t("settings.tabs.plugins") }}</span>
          </button>
          <button
            v-if="caps.pluginHost"
            class="settings-nav-item"
            :class="{ active: activeTab === 'shortcuts' }"
            @click="switchTab('shortcuts')"
          >
            <span class="nav-icon" v-html="tabIcons.shortcuts"></span>
            <span class="nav-label">{{ t("settings.tabs.shortcuts") }}</span>
          </button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'templates' }"
            @click="switchTab('templates')"
          >
            <span class="nav-icon" v-html="tabIcons.templates"></span>
            <span class="nav-label">{{ t("settings.templates.tab") }}</span>
          </button>
          <button
            v-if="caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'profiles' }"
            @click="switchTab('profiles')"
          >
            <span class="nav-icon" v-html="tabIcons.profiles"></span>
            <span class="nav-label">{{ t("settings.profiles.tab") }}</span>
          </button>
          <button
            v-if="caps.autoUpdate"
            class="settings-nav-item"
            :class="{ active: activeTab === 'updates' }"
            @click="switchTab('updates')"
          >
            <span class="nav-icon" v-html="tabIcons.updates"></span>
            <span class="nav-label">{{ t("settings.tabs.updates") }}</span>
          </button>
          <button
            v-if="caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'diagnostics' }"
            @click="switchTab('diagnostics')"
          >
            <span class="nav-icon" v-html="tabIcons.diagnostics"></span>
            <span class="nav-label">{{ t("settings.diagnostics.tab") }}</span>
          </button>
          <button
            v-if="caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'feishu' }"
            @click="switchTab('feishu')"
          >
            <span class="nav-icon" v-html="tabIcons.feishu"></span>
            <span class="nav-label">{{ t("settings.feishu.title") }}</span>
          </button>
          <button
            v-if="caps.wailsBindings"
            class="settings-nav-item"
            :class="{ active: activeTab === 'received-files' }"
            @click="switchTab('received-files')"
          >
            <span class="nav-icon" v-html="tabIcons['received-files']"></span>
            <span class="nav-label">{{ t("settings.tabs.receivedFiles") }}</span>
          </button>
        </aside>

        <section class="settings-pane">
          <header class="settings-pane-header">
            <h3 class="settings-pane-title">{{ activeTabLabel }}</h3>
            <span v-if="activeTabEnglish" class="settings-pane-subtitle">{{ activeTabEnglish }}</span>
          </header>
          <SettingsGeneral
            v-show="activeTab === 'general'"
            :terminal-theme-id="persistedTheme"
            @terminal-theme-changed="onTerminalThemeChanged"
            @command-notify-threshold-changed="onCommandNotifyThresholdChanged"
            @appearance-changed="onAppearanceChanged"
          />
          <SettingsAccount v-if="!caps.wailsBindings && activeTab === 'account'" />
          <SettingsTasks v-show="activeTab === 'tasks'" />
          <SettingsRelay
            v-if="caps.wailsBindings"
            v-show="activeTab === 'relay'"
            ref="relayRef"
            @dirty="onRelayDirty"
            @relay-config-changed="onRelayConfigChanged"
          />
          <SettingsUpdates
            v-if="caps.autoUpdate"
            v-show="activeTab === 'updates'"
            @request-install="onForceInstallClick"
          />
          <SettingsPlugins v-if="caps.pluginHost" v-show="activeTab === 'plugins'" />
          <SettingsShortcuts v-if="caps.pluginHost" v-show="activeTab === 'shortcuts'" @bindings-changed="onBindingsChanged" />
          <SettingsTemplates v-if="activeTab === 'templates'" />
          <SettingsProfiles v-if="activeTab === 'profiles' && caps.wailsBindings" @profiles-changed="onProfilesChanged" />
          <SettingsProfilesMobile
            v-if="activeTab === 'mobile-profiles' && caps.capacitor"
            @session-created="(sessionId) => emit('session-created', sessionId)"
          />
          <SettingsSSHHostsMobile v-if="activeTab === 'mobile-hosts' && caps.capacitor" />
          <div v-if="activeTab === 'diagnostics' && caps.wailsBindings" class="diag-merged">
            <section v-if="caps.fileDialog" class="merged-section">
              <h4 class="merged-section-title">{{ t("settings.tabs.logging") }}</h4>
              <SettingsLogging @open-log-viewer="openLogViewer" />
            </section>
            <section class="merged-section">
              <h4 v-if="caps.fileDialog" class="merged-section-title">{{ t("settings.diagnostics.section") }}</h4>
              <SettingsDiagnostics />
            </section>
            <!-- Export/import is a whole-config data operation (SSH hosts,
                 session profiles), not a single preference -- it belongs
                 beside Diagnostics's own save-dialog file export, not
                 buried at the bottom of the single-preference General pane.
                 SettingsConfigIO.vue self-gates internally too; the
                 caps.wailsBindings gate on the div.diag-merged container
                 above is defense in depth, matching SyncStatusIndicator's
                 precedent above. -->
            <section class="merged-section">
              <h4 class="merged-section-title">{{ t("settings.configio.title") }}</h4>
              <SettingsConfigIO />
            </section>
          </div>
          <SettingsFeishu v-if="activeTab === 'feishu' && caps.wailsBindings" />
          <SettingsDevices v-if="activeTab === 'devices'" />
          <SettingsReceivedFiles v-if="activeTab === 'received-files' && caps.wailsBindings" />
        </section>
      </div>

      <footer v-if="activeTab === 'relay'" class="settings-footer">
        <button @click="close" :disabled="relayRef?.saving">{{ t("common.cancel") }}</button>
        <button
          class="primary"
          :disabled="!relayRef?.canSave"
          @click="onSaveClick"
        >{{ relayRef?.saveLabel ?? t("settings.relay.saveConnect") }}</button>
      </footer>

      <footer class="version-footer" data-testid="settings-version-footer">
        {{ versionLabel }}
      </footer>
    </div>

    <div v-if="showDiscardConfirm" class="discard-backdrop" @click.self="onKeepEditing">
      <div class="discard-dialog">
        <h3>{{ t("settings.discardRelayTitle") }}</h3>
        <p>{{ t("settings.discardRelayBody") }}</p>
        <div class="discard-row">
          <button @click="onKeepEditing">{{ t("common.stay") }}</button>
          <button class="danger" @click="onConfirmDiscard">{{ t("common.discard") }}</button>
        </div>
      </div>
    </div>

    <ConfirmInstallDialog
      v-if="showConfirm"
      :version="updateVersionForConfirm"
      :local-count="props.localSessionCount"
      :remote-count="props.remoteSessionCount"
      @confirm="onConfirmInstall"
      @cancel="showConfirm = false"
    />
    <LogViewerDialog
      v-if="showLogViewer"
      :preview="logPreview ?? { path: '', exists: false, truncated: false, content: '' }"
      :loading="logViewerLoading"
      :error="logViewerError"
      @refresh="refreshLogViewer"
      @close="showLogViewer = false"
    />
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.settings-dialog {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 720px;
  height: 540px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.settings-header h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
}
.header-sync-indicator {
  flex: 1;
  min-width: 0;
  margin: 0 16px;
}
.close-btn {
  background: transparent;
  border: none;
  color: var(--fg-dim);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.close-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
  color: var(--fg);
}
.settings-body {
  flex: 1 1 auto;
  display: flex;
  min-height: 0;
}
.settings-nav {
  width: 160px;
  flex: 0 0 160px;
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  padding: 8px 6px;
  gap: 2px;
}
.settings-nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: left;
  background: transparent;
  border: none;
  color: var(--fg-dim);
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
.settings-nav-item .nav-icon {
  display: inline-flex;
  width: 16px;
  height: 16px;
  flex: 0 0 16px;
  color: var(--fg-dim);
}
.settings-nav-item .nav-icon :deep(svg) { width: 16px; height: 16px; }
.settings-nav-item.active .nav-icon { color: var(--accent); }
.settings-nav-item .nav-label { flex: 1 1 auto; min-width: 0; }
.settings-nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: var(--fg);
}
.settings-nav-item.active {
  background: rgba(88, 166, 255, 0.12);
  color: var(--accent);
  font-weight: 600;
}
.settings-pane {
  flex: 1 1 auto;
  padding: 20px 24px;
  overflow-y: auto;
}
.settings-pane-header {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin: 0 0 16px;
}
.settings-pane-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--fg);
}
.settings-pane-subtitle {
  font-size: 12px;
  color: var(--fg-dim);
}
.settings-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
}
.settings-footer button {
  height: 32px;
  padding: 6px 14px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
.settings-footer button:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.04);
}
.settings-footer button:disabled {
  opacity: 0.5;
  cursor: default;
}
.settings-footer .primary {
  background: var(--accent);
  color: #0d1117;
  border-color: var(--accent);
  font-weight: 600;
}
.settings-footer .primary:hover:not(:disabled) {
  background: #79b8ff;
  border-color: #79b8ff;
}
.settings-footer .danger {
  border-color: var(--bad);
  color: var(--bad);
}
.settings-footer .danger:hover:not(:disabled) {
  background: rgba(248, 81, 73, 0.1);
}
.version-footer {
  flex: 0 0 auto;
  padding: 4px 16px 8px;
  font-size: 11px;
  color: var(--fg-dim);
  text-align: right;
}

/* Diagnostics pane hosts two stacked sections: Logging (top) + Diagnostics
   snapshot (bottom), separated by a divider. */
.diag-merged {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.merged-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.merged-section + .merged-section {
  border-top: 1px solid var(--border);
  padding-top: 18px;
}
.merged-section-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
}

.discard-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 105;
}
.discard-dialog {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 380px;
  max-width: calc(100vw - 32px);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.discard-dialog h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
}
.discard-dialog p {
  margin: 0;
  font-size: 13px;
  color: var(--fg);
  line-height: 1.5;
}
.discard-row {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.discard-row button {
  height: 32px;
  padding: 6px 14px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
.discard-row button:hover {
  background: rgba(255, 255, 255, 0.04);
}
.discard-row .danger {
  border-color: var(--bad);
  color: var(--bad);
}
.discard-row .danger:hover {
  background: rgba(248, 81, 73, 0.1);
}

@media (max-width: 640px) {
  .backdrop {
    align-items: stretch;
    justify-content: stretch;
    padding: env(safe-area-inset-top) 0 env(safe-area-inset-bottom);
  }
  .settings-dialog {
    width: 100vw;
    height: auto;
    max-width: 100vw;
    max-height: 100dvh;
    border-left: none;
    border-right: none;
    border-radius: 0;
  }
  .settings-header {
    padding: 10px 12px;
  }
  .settings-body {
    flex-direction: column;
  }
  .settings-nav {
    width: 100%;
    flex: 0 0 auto;
    flex-direction: row;
    overflow-x: auto;
    overflow-y: hidden;
    border-right: none;
    border-bottom: 1px solid var(--border);
    padding: 6px 8px;
    scrollbar-width: none;
  }
  .settings-nav::-webkit-scrollbar { display: none; }
  .settings-nav-item {
    flex: 0 0 auto;
    width: auto;
    min-height: 34px;
    padding: 6px 10px;
    white-space: nowrap;
  }
  .settings-pane {
    min-height: 0;
    padding: 14px 16px 18px;
  }
  .settings-pane-header {
    margin-bottom: 12px;
  }
  .settings-pane-title {
    font-size: 18px;
  }
  .settings-footer {
    padding: 10px 12px;
  }
  .version-footer {
    padding: 4px 12px 8px;
  }
}
</style>
