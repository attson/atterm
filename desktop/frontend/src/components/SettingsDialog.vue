<script lang="ts" setup>
import { onMounted, ref } from "vue";
import {
  getLogPreview,
  getTerminalThemePreference,
  installUpdate,
  type LogPreview,
} from "../lib/api";
import { getTerminalTheme } from "../lib/terminalThemes";
import SettingsGeneral from "./SettingsGeneral.vue";
import SettingsRelay from "./SettingsRelay.vue";
import SettingsLogging from "./SettingsLogging.vue";
import SettingsUpdates from "./SettingsUpdates.vue";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";
import LogViewerDialog from "./LogViewerDialog.vue";

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "command-notify-threshold-changed", seconds: number): void;
}>();

const activeTab = ref<"general" | "relay" | "logging" | "updates">("general");
const persistedTheme = ref(getTerminalTheme(props.terminalThemeId).id);

const relayRef = ref<InstanceType<typeof SettingsRelay> | null>(null);
const relayDirty = ref(false);
const pendingTab = ref<"general" | "relay" | "logging" | "updates" | null>(null);
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

function switchTab(next: "general" | "relay" | "logging" | "updates") {
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

function onDisconnectClick() {
  relayRef.value?.disconnect();
}
</script>

<template>
  <div class="backdrop" @click.self="close">
    <div class="settings-dialog">
      <header class="settings-header">
        <h2>Settings</h2>
        <button class="close-btn" @click="close" :disabled="relayRef?.saving">×</button>
      </header>

      <div class="settings-body">
        <aside class="settings-nav">
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'general' }"
            @click="switchTab('general')"
          >General</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'relay' }"
            @click="switchTab('relay')"
          >Relay</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'logging' }"
            @click="switchTab('logging')"
          >Logging</button>
          <button
            class="settings-nav-item"
            :class="{ active: activeTab === 'updates' }"
            @click="switchTab('updates')"
          >Updates</button>
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
            v-show="activeTab === 'logging'"
            @open-log-viewer="openLogViewer"
          />
          <SettingsUpdates
            v-show="activeTab === 'updates'"
            @request-install="onForceInstallClick"
          />
        </section>
      </div>

      <footer v-if="activeTab === 'relay'" class="settings-footer">
        <button @click="close" :disabled="relayRef?.saving">cancel</button>
        <button
          v-if="relayRef?.connected"
          class="danger"
          @click="onDisconnectClick"
          :disabled="relayRef?.saving"
        >disconnect</button>
        <button
          class="primary"
          :disabled="!relayRef?.canSave"
          @click="onSaveClick"
        >{{ relayRef?.saveLabel ?? "save & connect" }}</button>
      </footer>
    </div>

    <div v-if="showDiscardConfirm" class="discard-backdrop" @click.self="onKeepEditing">
      <div class="discard-dialog">
        <h3>Discard unsaved relay changes?</h3>
        <p>Your edits to relay URL, token, permissions, or insecure mode are not saved yet.</p>
        <div class="discard-row">
          <button @click="onKeepEditing">stay</button>
          <button class="danger" @click="onConfirmDiscard">discard</button>
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
