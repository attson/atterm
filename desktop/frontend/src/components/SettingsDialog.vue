<script lang="ts" setup>
import { onMounted, ref } from "vue";
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
import SettingsLogging from "./SettingsLogging.vue";
import SettingsUpdates from "./SettingsUpdates.vue";
import SettingsPlugins from "./SettingsPlugins.vue";
import SettingsShortcuts from "./SettingsShortcuts.vue";
import SettingsDiagnostics from "./SettingsDiagnostics.vue";
import SettingsTemplates from "./SettingsTemplates.vue";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";
import LogViewerDialog from "./LogViewerDialog.vue";
import { useI18n } from "../i18n/useI18n";

const caps = usePlatform().caps;
const { t } = useI18n();

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
  initialTab?: "general" | "relay" | "logging" | "updates" | "shortcuts" | "diagnostics" | "templates";
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
}>();

const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates">(props.initialTab ?? "general");

const hiddenTabs = new Set<string>()
if (!caps.autoUpdate) hiddenTabs.add('updates')
if (!caps.pluginHost) { hiddenTabs.add('plugins'); hiddenTabs.add('shortcuts') }
if (!caps.fileDialog) hiddenTabs.add('logging')
if (hiddenTabs.has(activeTab.value)) activeTab.value = 'general'

const persistedTheme = ref(getTerminalTheme(props.terminalThemeId).id);

const relayRef = ref<InstanceType<typeof SettingsRelay> | null>(null);
const relayDirty = ref(false);
const pendingTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | null>(null);
const showDiscardConfirm = ref(false);

const logPreview = ref<LogPreview | null>(null);
const logViewerError = ref("");
const logViewerLoading = ref(false);
const showLogViewer = ref(false);

const showConfirm = ref(false);
const updateVersionForConfirm = ref("");

onMounted(async () => {
  try {
    const themeID = await getTerminalThemePreference();
    persistedTheme.value = getTerminalTheme(themeID).id;
  } catch {
    /* keep the prop value */
  }
});

function switchTab(next: "general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates") {
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
          >{{ t("settings.tabs.general") }}</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'relay' }"
            @click="switchTab('relay')"
          >{{ t("settings.tabs.relay") }}</button>
          <button
            v-if="caps.fileDialog"
            class="settings-nav-item"
            :class="{ active: activeTab === 'logging' }"
            @click="switchTab('logging')"
          >{{ t("settings.tabs.logging") }}</button>
          <button
            v-if="caps.autoUpdate"
            class="settings-nav-item"
            :class="{ active: activeTab === 'updates' }"
            @click="switchTab('updates')"
          >{{ t("settings.tabs.updates") }}</button>
          <button
            v-if="caps.pluginHost"
            class="settings-nav-item"
            :class="{ active: activeTab === 'plugins' }"
            @click="switchTab('plugins')"
          >{{ t("settings.tabs.plugins") }}</button>
          <button
            v-if="caps.pluginHost"
            class="settings-nav-item"
            :class="{ active: activeTab === 'shortcuts' }"
            @click="switchTab('shortcuts')"
          >{{ t("settings.tabs.shortcuts") }}</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'templates' }"
            @click="switchTab('templates')"
          >{{ t("settings.templates.tab") }}</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'diagnostics' }"
            @click="switchTab('diagnostics')"
          >{{ t("settings.diagnostics.tab") }}</button>
        </aside>

        <section class="settings-pane">
          <SettingsGeneral
            v-show="activeTab === 'general'"
            :terminal-theme-id="persistedTheme"
            @terminal-theme-changed="onTerminalThemeChanged"
            @command-notify-threshold-changed="onCommandNotifyThresholdChanged"
          />
          <SettingsRelay
            v-show="activeTab === 'relay'"
            ref="relayRef"
            @dirty="onRelayDirty"
            @relay-config-changed="onRelayConfigChanged"
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
  display: block;
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
