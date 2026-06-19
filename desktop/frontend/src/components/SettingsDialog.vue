<script lang="ts" setup>
import { computed, onMounted, ref } from "vue";
import {
  getLogPreview,
  getTerminalThemePreference,
  installUpdate,
  type LogPreview,
} from "../lib/api";
import { getTerminalTheme } from "../lib/terminalThemes";
import { usePlatform } from "../platform";
import SettingsGeneral from "./SettingsGeneral.vue";
import SettingsRelay from "./SettingsRelay.vue";
import SettingsWebhooks from "./SettingsWebhooks.vue";
import SettingsLogging from "./SettingsLogging.vue";
import SettingsUpdates from "./SettingsUpdates.vue";
import SettingsPlugins from "./SettingsPlugins.vue";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import SettingsDiagnostics from "./SettingsDiagnostics.vue";
import SettingsTemplates from "./SettingsTemplates.vue";
import SettingsTasks from "./SettingsTasks.vue";
import SettingsFeishu from "./SettingsFeishu.vue";
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
type SettingsTabId = "general" | "tasks" | "relay" | "webhooks" | "plugins"
  | "shortcuts" | "templates" | "logging" | "updates" | "diagnostics" | "feishu";

const tabMeta: Record<SettingsTabId, { labelKey: MessageKey; english: string }> = {
  general:     { labelKey: "settings.tabs.general",        english: "General preferences" },
  tasks:       { labelKey: "tasks.settings.section",       english: "Tasks display" },
  relay:       { labelKey: "settings.tabs.relay",          english: "Relay" },
  webhooks:    { labelKey: "settings.tabs.webhooks",       english: "Outbound webhooks" },
  plugins:     { labelKey: "settings.tabs.plugins",        english: "Plugins" },
  shortcuts:   { labelKey: "settings.tabs.shortcuts",      english: "Keyboard shortcuts" },
  templates:   { labelKey: "settings.templates.tab",       english: "Quick templates" },
  logging:     { labelKey: "settings.tabs.logging",        english: "Logging" },
  updates:     { labelKey: "settings.tabs.updates",        english: "Updates" },
  diagnostics: { labelKey: "settings.diagnostics.tab",     english: "Diagnostics" },
  feishu:      { labelKey: "settings.feishu.title",        english: "Feishu integration" },
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
  tasks:       `<svg ${icoBase}><path d="M3 4h10M3 8h10M3 12h6"/></svg>`,
  relay:       `<svg ${icoBase}><circle cx="8" cy="8" r="1.4"/><path d="M4.4 4.4a5 5 0 0 0 0 7.2M11.6 11.6a5 5 0 0 0 0-7.2M2.4 2.4a8 8 0 0 0 0 11.2M13.6 13.6a8 8 0 0 0 0-11.2"/></svg>`,
  webhooks:    `<svg ${icoBase}><circle cx="4.5" cy="11.5" r="1.6"/><circle cx="11" cy="3.6" r="1.6"/><circle cx="11" cy="11.5" r="1.6"/><path d="M5.6 10.3 9.9 4.9M6.1 11.5h3.3"/></svg>`,
  plugins:     `<svg ${icoBase}><path d="M5 2v2.5H2.5V8H5v3.5h3.5V14H12V11.5h2V8h-2.5V4.5H8.5V2z"/></svg>`,
  shortcuts:   `<svg ${icoBase}><rect x="1.6" y="4.2" width="12.8" height="7.6" rx="1.6"/><path d="M4 7h.01M6.5 7h.01M9 7h.01M11.5 7h.01M4.5 9.5h7"/></svg>`,
  templates:   `<svg ${icoBase}><rect x="2.4" y="2.4" width="11.2" height="11.2" rx="1.4"/><path d="M2.4 6h11.2M6 6v7.6"/></svg>`,
  logging:     `<svg ${icoBase}><path d="M3 2.5h7l3 3v8H3z"/><path d="M10 2.5v3h3M5 8.5h6M5 11h4"/></svg>`,
  updates:     `<svg ${icoBase}><path d="M2.5 8a5.5 5.5 0 0 1 9.7-3.5L14 6.5"/><path d="M14 2.5v4h-4"/><path d="M13.5 8a5.5 5.5 0 0 1-9.7 3.5L2 9.5"/><path d="M2 13.5v-4h4"/></svg>`,
  diagnostics: `<svg ${icoBase}><circle cx="7.2" cy="7.2" r="4.4"/><path d="m10.4 10.4 3 3"/></svg>`,
  feishu:      `<svg ${icoBase}><path d="M3.5 4.5h6l2 2v5a1.5 1.5 0 0 1-1.5 1.5h-6.5a1 1 0 0 1-1-1V5.5a1 1 0 0 1 1-1Z"/><path d="M9.5 4.5v2h2"/></svg>`,
};

const relayHasToken = ref(false);

async function refreshRelayHasToken() {
  try {
    const cfg = await platform.relay.load();
    relayHasToken.value = !!(cfg && cfg.url && cfg.token);
  } catch {
    relayHasToken.value = false;
  }
}

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
  initialTab?: "general" | "relay" | "webhooks" | "logging" | "updates" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu";
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
}>();

const activeTab = ref<"general" | "relay" | "webhooks" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu">(props.initialTab ?? "general");

const hiddenTabs = new Set<string>()
if (!caps.autoUpdate) hiddenTabs.add('updates')
if (!caps.pluginHost) { hiddenTabs.add('plugins'); hiddenTabs.add('shortcuts') }
if (!caps.fileDialog) hiddenTabs.add('logging')
if (hiddenTabs.has(activeTab.value)) activeTab.value = 'general'

const persistedTheme = ref(getTerminalTheme(props.terminalThemeId).id);

const relayRef = ref<InstanceType<typeof SettingsRelay> | null>(null);
const relayDirty = ref(false);
const pendingTab = ref<"general" | "relay" | "webhooks" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu" | null>(null);
const showDiscardConfirm = ref(false);

const logPreview = ref<LogPreview | null>(null);
const logViewerError = ref("");
const logViewerLoading = ref(false);
const showLogViewer = ref(false);

const showConfirm = ref(false);
const updateVersionForConfirm = ref("");

onMounted(async () => {
  await refreshRelayHasToken();
  try {
    const themeID = await getTerminalThemePreference();
    persistedTheme.value = getTerminalTheme(themeID).id;
  } catch {
    /* keep the prop value */
  }
});

function switchTab(next: "general" | "relay" | "webhooks" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu") {
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
  void refreshRelayHasToken();
  emit("relay-config-changed");
}

function onTerminalThemeChanged(themeID: string) {
  persistedTheme.value = getTerminalTheme(themeID).id;
  emit("terminal-theme-changed", themeID);
}

function onCommandNotifyThresholdChanged(seconds: number) {
  emit("command-notify-threshold-changed", seconds);
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
            class="settings-nav-item"
            :class="{ active: activeTab === 'tasks' }"
            @click="switchTab('tasks')"
          >
            <span class="nav-icon" v-html="tabIcons.tasks"></span>
            <span class="nav-label">{{ t("tasks.settings.section") }}</span>
          </button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'relay' }"
            @click="switchTab('relay')"
          >
            <span class="nav-icon" v-html="tabIcons.relay"></span>
            <span class="nav-label">{{ t("settings.tabs.relay") }}</span>
          </button>
          <button
            v-if="relayHasToken"
            class="settings-nav-item"
            data-testid="webhooks-nav"
            :class="{ active: activeTab === 'webhooks' }"
            @click="switchTab('webhooks')"
          >
            <span class="nav-icon" v-html="tabIcons.webhooks"></span>
            <span class="nav-label">{{ t("settings.tabs.webhooks") }}</span>
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
            v-if="caps.fileDialog"
            class="settings-nav-item"
            :class="{ active: activeTab === 'logging' }"
            @click="switchTab('logging')"
          >
            <span class="nav-icon" v-html="tabIcons.logging"></span>
            <span class="nav-label">{{ t("settings.tabs.logging") }}</span>
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
            class="settings-nav-item"
            :class="{ active: activeTab === 'diagnostics' }"
            @click="switchTab('diagnostics')"
          >
            <span class="nav-icon" v-html="tabIcons.diagnostics"></span>
            <span class="nav-label">{{ t("settings.diagnostics.tab") }}</span>
          </button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'feishu' }"
            @click="switchTab('feishu')"
          >
            <span class="nav-icon" v-html="tabIcons.feishu"></span>
            <span class="nav-label">{{ t("settings.feishu.title") }}</span>
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
          />
          <SettingsTasks v-show="activeTab === 'tasks'" />
          <SettingsRelay
            v-show="activeTab === 'relay'"
            ref="relayRef"
            @dirty="onRelayDirty"
            @relay-config-changed="onRelayConfigChanged"
          />
          <SettingsWebhooks
            v-if="relayHasToken && activeTab === 'webhooks'"
          />
          <SettingsLogging
            v-if="caps.fileDialog"
            v-show="activeTab === 'logging'"
            @open-log-viewer="openLogViewer"
          />
          <SettingsUpdates
            v-if="caps.autoUpdate"
            v-show="activeTab === 'updates'"
            @request-install="onForceInstallClick"
          />
          <SettingsPlugins v-if="caps.pluginHost" v-show="activeTab === 'plugins'" />
          <SettingsShortcuts v-if="caps.pluginHost" v-show="activeTab === 'shortcuts'" />
          <SettingsTemplates v-if="activeTab === 'templates'" />
          <SettingsDiagnostics v-if="activeTab === 'diagnostics'" />
          <SettingsFeishu v-if="activeTab === 'feishu'" />
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
</style>
