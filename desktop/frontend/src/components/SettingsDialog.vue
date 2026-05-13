<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import {
  checkUpdate,
  getAutoCheckUpdates,
  getLoggingConfig,
  getLogPreview,
  getRelayConfig,
  getTerminalThemePreference,
  getUpdateState,
  installUpdate,
  pickLogFilePath,
  setAutoCheckUpdates,
  setLoggingConfig,
  setRelayConfig,
  setTerminalThemePreference,
  startDownload,
  type LogPreview,
  type UpdateState,
} from "../lib/api";
import {
  TERMINAL_THEMES,
  getTerminalTheme,
} from "../lib/terminalThemes";
import ConfirmInstallDialog from "./ConfirmInstallDialog.vue";

const props = defineProps<{
  localSessionCount: number;
  remoteSessionCount: number;
  terminalThemeId: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "relay-config-changed"): void;
  (e: "terminal-theme-changed", themeID: string): void;
}>();

const url = ref("");
const token = ref("");
const allowInsecureRelay = ref(false);
const remotePermission = ref("full");
const selectedTerminalTheme = ref(getTerminalTheme(props.terminalThemeId).id);
const persistedTerminalTheme = ref(getTerminalTheme(props.terminalThemeId).id);
const connected = ref(false);
const loading = ref(true);
const saving = ref(false);
const error = ref("");
const logToFileEnabled = ref(true);
const logFilePath = ref("");
const effectiveLogFilePath = ref("");
const logPreview = ref<LogPreview | null>(null);
const showLogViewer = ref(false);

const updateState = ref<UpdateState | null>(null);
const autoCheck = ref(true);
const checkingNow = ref(false);
const showConfirm = ref(false);
let pollHandle: number | null = null;

onMounted(async () => {
  try {
    const [cfg, loggingCfg, st, ac, themeID] = await Promise.all([
      getRelayConfig(),
      getLoggingConfig(),
      getUpdateState(),
      getAutoCheckUpdates(),
      getTerminalThemePreference(),
    ]);
    url.value = cfg.url;
    token.value = cfg.token;
    allowInsecureRelay.value = cfg.allow_insecure_relay;
    remotePermission.value = cfg.remote_permission || "full";
    connected.value = cfg.connected;
    logToFileEnabled.value = loggingCfg.enabled;
    logFilePath.value = loggingCfg.path;
    effectiveLogFilePath.value = loggingCfg.effective_path;
    updateState.value = st;
    autoCheck.value = ac;
    selectedTerminalTheme.value = getTerminalTheme(themeID).id;
    persistedTerminalTheme.value = selectedTerminalTheme.value;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
  pollHandle = window.setInterval(async () => {
    try {
      updateState.value = await getUpdateState();
    } catch {
      /* ignore — relay polling already surfaces general health */
    }
  }, 2000);
});

onBeforeUnmount(() => {
  if (pollHandle !== null) window.clearInterval(pollHandle);
});

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
    connected.value = cfg.connected;
    emit("relay-config-changed");
    emit("close");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

async function disconnect() {
  saving.value = true;
  error.value = "";
  try {
    await setRelayConfig({ url: "", token: "", allow_insecure_relay: false });
    url.value = "";
    token.value = "";
    allowInsecureRelay.value = false;
    remotePermission.value = "full";
    connected.value = false;
    emit("relay-config-changed");
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    saving.value = false;
  }
}

function close() {
  if (!saving.value) emit("close");
}

async function onTerminalThemeChange() {
  const nextTheme = getTerminalTheme(selectedTerminalTheme.value).id;
  const previousTheme = persistedTerminalTheme.value;
  selectedTerminalTheme.value = nextTheme;
  error.value = "";
  try {
    await setTerminalThemePreference(nextTheme);
    persistedTerminalTheme.value = nextTheme;
    emit("terminal-theme-changed", nextTheme);
  } catch (e: any) {
    selectedTerminalTheme.value = previousTheme;
    emit("terminal-theme-changed", previousTheme);
    error.value = e?.message ?? String(e);
  }
}

async function onCheckNow() {
  checkingNow.value = true;
  try {
    await checkUpdate();
  } catch {
    /* state.error reflects in poll */
  } finally {
    checkingNow.value = false;
  }
}

async function onDownload() {
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  }
}

function onForceInstallClick() {
  showConfirm.value = true;
}

async function onConfirmInstall() {
  showConfirm.value = false;
  try {
    await installUpdate();
    // App will quit shortly; nothing more to do.
  } catch {
    /* state.error reflects in poll */
  }
}

async function onAutoCheckToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  autoCheck.value = target.checked;
  await setAutoCheckUpdates(target.checked);
}

async function onLoggingToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previousEnabled = logToFileEnabled.value;
  logToFileEnabled.value = target.checked;
  error.value = "";
  try {
    await setLoggingConfig({
      enabled: target.checked,
      path: logFilePath.value,
    });
    const cfg = await getLoggingConfig();
    logToFileEnabled.value = cfg.enabled;
    logFilePath.value = cfg.path;
    effectiveLogFilePath.value = cfg.effective_path;
  } catch (e: any) {
    logToFileEnabled.value = previousEnabled;
    error.value = e?.message ?? String(e);
  }
}

async function onPickLogFilePath() {
  error.value = "";
  try {
    const pickedPath = await pickLogFilePath();
    if (!pickedPath) return;
    await setLoggingConfig({
      enabled: logToFileEnabled.value,
      path: pickedPath,
    });
    const cfg = await getLoggingConfig();
    logToFileEnabled.value = cfg.enabled;
    logFilePath.value = cfg.path;
    effectiveLogFilePath.value = cfg.effective_path;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

async function onResetLogFilePath() {
  error.value = "";
  try {
    await setLoggingConfig({
      enabled: logToFileEnabled.value,
      path: "",
    });
    const cfg = await getLoggingConfig();
    logToFileEnabled.value = cfg.enabled;
    logFilePath.value = cfg.path;
    effectiveLogFilePath.value = cfg.effective_path;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

async function openLogViewer() {
  error.value = "";
  try {
    logPreview.value = await getLogPreview();
    showLogViewer.value = true;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

const updateStatusLine = computed(() => {
  const st = updateState.value;
  if (!st) return "";
  if (st.current === "dev" || st.current === "") {
    return "development build — auto-update disabled";
  }
  if (st.error) return st.error;
  if (st.checking || checkingNow.value) return "checking…";
  if (st.ready) return `${st.latest} downloaded — ready to install`;
  if (st.downloading) return `downloading ${st.latest} (${st.download_pct}%)`;
  if (st.available) return `${st.latest} available`;
  if (st.last_check_at > 0) return `up to date · last checked ${formatAgo(st.last_check_at)}`;
  return "not checked yet";
});

function formatAgo(unixSec: number) {
  const diffSec = Math.floor(Date.now() / 1000) - unixSec;
  if (diffSec < 60) return "just now";
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} min ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} h ago`;
  return `${Math.floor(diffSec / 86400)} d ago`;
}

const showUpdates = computed(() => updateState.value !== null);
const isDev = computed(
  () => updateState.value?.current === "dev" || updateState.value?.current === "",
);
</script>

<template>
  <div class="backdrop" @click.self="close">
    <div class="dialog">
      <h2>relay settings</h2>

      <div v-if="loading" class="dim">loading…</div>
      <template v-else>
        <p class="hint">
          configure a remote atterm-relay so this machine's sessions can be
          attached from other devices. when no one is attached, no bytes leave
          this machine.
        </p>

        <label>terminal theme</label>
        <select
          v-model="selectedTerminalTheme"
          :disabled="saving"
          @change="onTerminalThemeChange"
        >
          <option
            v-for="theme in TERMINAL_THEMES"
            :key="theme.id"
            :value="theme.id"
          >
            {{ theme.label }} — {{ theme.description }}
          </option>
        </select>
        <p class="hint">
          Applies to all terminal panes immediately and is saved as your local desktop preference.
        </p>

        <label>relay url</label>
        <input
          v-model="url"
          type="text"
          placeholder="wss://relay.example.com"
          :disabled="saving"
          @keyup.enter="save"
        />

        <label>token</label>
        <input
          v-model="token"
          type="password"
          placeholder="shared bearer token"
          :disabled="saving"
          @keyup.enter="save"
        />

        <label>remote session permissions</label>
        <select v-model="remotePermission" :disabled="saving">
          <option value="view">view only — remote clients can watch output</option>
          <option value="control">control — allow input and resize</option>
          <option value="full">full — allow input, resize, and image paste</option>
        </select>
        <p class="hint">
          This is announced as the owner policy for sessions from this desktop.
          Relay-side read-only tokens can still reduce access to view only.
        </p>

        <label class="checkbox insecure-toggle">
          <input
            v-model="allowInsecureRelay"
            type="checkbox"
            :disabled="saving"
          />
          enable insecure mode (allow ws:// cleartext relay)
        </label>
        <p v-if="allowInsecureRelay" class="warning">
          ws:// sends the relay token, terminal output, and your input in
          clear text. Use this only on trusted private networks.
        </p>

        <div class="status">
          <span :class="connected ? 'on' : 'off'">●</span>
          {{ connected ? "uplink running" : "uplink stopped" }}
        </div>

        <div class="updates">
          <h2>logging</h2>
          <label class="checkbox">
            <input
              type="checkbox"
              :checked="logToFileEnabled"
              @change="onLoggingToggle"
            />
            write logs to file
          </label>
          <div class="grid">
            <div class="kv">
              <span class="k">current log file</span>
              <span class="v path" :title="effectiveLogFilePath">
                {{ effectiveLogFilePath }}
              </span>
            </div>
          </div>
          <div class="row">
            <button @click="onPickLogFilePath">change location</button>
            <button @click="onResetLogFilePath">reset default</button>
            <button @click="openLogViewer">view logs</button>
          </div>
          <div v-if="showLogViewer && logPreview" class="hint">
            loaded preview from {{ logPreview.path }}
          </div>
        </div>

        <div v-if="error" class="error">{{ error }}</div>

        <div class="row">
          <button @click="close" :disabled="saving">cancel</button>
          <button
            v-if="connected"
            @click="disconnect"
            :disabled="saving"
            class="danger"
          >disconnect</button>
          <button
            class="primary"
            @click="save"
            :disabled="saving || !url.trim()"
          >
            {{ saving ? "saving…" : "save & connect" }}
          </button>
        </div>

        <div v-if="showUpdates" class="updates">
          <h2>updates</h2>
          <div class="grid">
            <div class="kv">
              <span class="k">current version</span>
              <span class="v">{{ updateState!.current || "(unknown)" }}</span>
            </div>
            <div class="kv">
              <span class="k">status</span>
              <span class="v">{{ updateStatusLine }}</span>
            </div>
            <div
              v-if="updateState!.download_path && (updateState!.ready || updateState!.downloading)"
              class="kv"
            >
              <span class="k">download path</span>
              <span class="v path" :title="updateState!.download_path">
                {{ updateState!.download_path }}
              </span>
            </div>
          </div>

          <div v-if="!isDev" class="row autocheck">
            <label class="checkbox">
              <input
                type="checkbox"
                :checked="autoCheck"
                @change="onAutoCheckToggle"
              />
              automatically check for updates
            </label>
          </div>

          <details v-if="!isDev && updateState!.notes" class="notes">
            <summary>release notes</summary>
            <pre>{{ updateState!.notes }}</pre>
          </details>

          <div v-if="!isDev" class="row">
            <button
              @click="onCheckNow"
              :disabled="checkingNow || updateState!.checking"
            >check now</button>
            <button
              v-if="updateState!.available && !updateState!.ready && !updateState!.downloading"
              class="primary"
              @click="onDownload"
            >download {{ updateState!.latest }}</button>
            <button
              v-if="updateState!.downloading"
              class="primary"
              disabled
            >downloading… {{ updateState!.download_pct }}%</button>
            <button
              v-if="updateState!.ready"
              class="primary danger"
              @click="onForceInstallClick"
            >force install &amp; restart</button>
          </div>
        </div>
      </template>
    </div>
    <ConfirmInstallDialog
      v-if="showConfirm && updateState"
      :version="updateState.latest"
      :local-count="props.localSessionCount"
      :remote-count="props.remoteSessionCount"
      @confirm="onConfirmInstall"
      @cancel="showConfirm = false"
    />
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 460px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 32px);
  overflow-y: auto;
  box-sizing: border-box;
  display: flex; flex-direction: column; gap: 8px;
}
.dialog h2 {
  margin: 0 0 12px; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.hint {
  font-size: 12px; color: var(--fg-dim); margin: 0 0 8px; line-height: 1.5;
}
label {
  font-size: 12px; color: var(--fg-dim); margin-top: 6px;
}
.status {
  font-size: 12px; color: var(--fg-dim); margin-top: 10px;
  display: flex; align-items: center; gap: 6px;
}
.status .on { color: var(--good); }
.status .off { color: var(--fg-dim); }
.dim { color: var(--fg-dim); font-size: 13px; padding: 8px 0; }
.error { color: var(--bad); font-size: 12px; margin-top: 6px; }
.warning {
  color: var(--bad); font-size: 12px; line-height: 1.45; margin: 2px 0 0;
}
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px;
}
button.danger {
  border-color: var(--bad); color: var(--bad);
}
button.danger:hover { background: rgba(248, 81, 73, 0.1); }

.updates {
  border-top: 1px solid var(--border);
  margin-top: 16px;
  padding-top: 16px;
}
.updates h2 { margin-bottom: 8px; }
.grid {
  display: grid; gap: 6px; font-size: 12px;
}
.kv { display: flex; align-items: flex-start; gap: 12px; }
.kv .k { color: var(--fg-dim); width: 130px; }
.kv .v { color: var(--fg); }
.kv .path {
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
.autocheck { justify-content: flex-start; margin-top: 12px; }
.checkbox {
  display: flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--fg);
}
.insecure-toggle {
  margin-top: 10px;
}
.notes {
  margin-top: 8px; font-size: 12px; color: var(--fg);
}
.notes summary { color: var(--fg-dim); cursor: pointer; }
.notes pre {
  background: var(--bg); border: 1px solid var(--border);
  padding: 8px; border-radius: 6px;
  white-space: pre-wrap; word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-height: 160px; overflow-y: auto;
  font-size: 11px;
}
button.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
button.primary:hover { background: #79b8ff; border-color: #79b8ff; color: #0d1117; }
button.primary.danger {
  background: var(--bad); color: #0d1117; border-color: var(--bad);
}
button.primary.danger:hover {
  background: #ff6f6a; border-color: #ff6f6a; color: #0d1117;
}
</style>
