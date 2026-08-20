<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  checkUpdate,
  downloadVersion,
  getAutoCheckUpdates,
  getUpdateGHProxyURL,
  getUpdateState,
  setAutoCheckUpdates,
  setUpdateGHProxyURL,
  startDownload,
  cancelDownload,
  forceRedownload,
  type UpdateState,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import { formatAgo } from "../lib/formatAgo";

defineEmits<{
  (e: "request-install", version: string): void;
}>();

const state = ref<UpdateState | null>(null);
const autoCheck = ref(true);
const ghProxyURL = ref("");
const checkingNow = ref(false);
const savingProxy = ref(false);
const loading = ref(true);
const error = ref("");
const selectedLine = ref("");
const clickInFlight = ref(false);
const confirmingRedownload = ref(false);
const cancelling = ref(false);
let pollHandle: number | null = null;
const { t } = useI18n();

// Extracts the minor line (e.g. "v0.2") from a version/tag like "v0.2.3".
// Matches the backend VersionLine.Minor format, which keeps the "v" prefix.
function minorOf(version: string): string {
  const parts = version.replace(/^v/, "").split(".");
  if (parts.length < 2) return "";
  return `v${parts[0]}.${parts[1]}`;
}

// Default the selected line to the line matching the current install's minor;
// for dev builds (no matching minor) fall back to the first/highest line.
watch(
  () => state.value?.lines,
  (lines) => {
    if (!lines || lines.length === 0) return;
    if (selectedLine.value && lines.some((l) => l.minor === selectedLine.value)) {
      return;
    }
    const currentMinor = minorOf(state.value?.current ?? "");
    const match = lines.find((l) => l.minor === currentMinor);
    selectedLine.value = match ? match.minor : lines[0].minor;
  },
  { immediate: true },
);

const selectedLatest = computed(() => {
  const lines = state.value?.lines ?? [];
  const line = lines.find((l) => l.minor === selectedLine.value);
  return line?.latest ?? "";
});

onMounted(async () => {
  try {
    const [st, ac, proxyURL] = await Promise.all([
      getUpdateState(),
      getAutoCheckUpdates(),
      getUpdateGHProxyURL(),
    ]);
    state.value = st;
    autoCheck.value = ac;
    ghProxyURL.value = proxyURL;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
  pollHandle = window.setInterval(async () => {
    try {
      state.value = await getUpdateState();
    } catch {
      /* ignore — relay polling already surfaces general health */
    }
  }, 2000);
});

onBeforeUnmount(() => {
  if (pollHandle !== null) window.clearInterval(pollHandle);
});

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
  clickInFlight.value = true;
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  }
}

async function onDownloadSelected() {
  if (!selectedLatest.value) return;
  clickInFlight.value = true;
  try {
    await downloadVersion(selectedLatest.value);
  } catch {
    /* state.error reflects in poll */
  }
}

watch(
  () => state.value?.downloaded_exists,
  async (now, was) => {
    if (!now || was) return;
    if (!clickInFlight.value) return;
    if (confirmingRedownload.value) return;
    const tag = state.value?.latest ?? "";
    clickInFlight.value = false;
    confirmingRedownload.value = true;
    try {
      if (window.confirm(t("settings.updates.redownloadPrompt", { version: tag }))) {
        try {
          await forceRedownload(tag);
        } catch {
          /* poll surfaces error */
        }
      }
      // Cancel branch: nothing. state.ready is already true; the Install &
      // Restart button and the Redownload button are already rendered.
    } finally {
      confirmingRedownload.value = false;
    }
  },
);

async function onCancelDownload() {
  cancelling.value = true;
  try {
    await cancelDownload();
  } catch {
    /* poll surfaces error */
  } finally {
    cancelling.value = false;
  }
}

async function onRedownload() {
  const tag = state.value?.latest ?? "";
  if (!tag) return;
  try {
    await forceRedownload(tag);
  } catch {
    /* poll surfaces error */
  }
}

async function onAutoCheckToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  autoCheck.value = target.checked;
  await setAutoCheckUpdates(target.checked);
}

async function onSaveProxy() {
  savingProxy.value = true;
  error.value = "";
  try {
    await setUpdateGHProxyURL(ghProxyURL.value);
    ghProxyURL.value = await getUpdateGHProxyURL();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    savingProxy.value = false;
  }
}

const isDev = computed(
  () => state.value?.current === "dev" || state.value?.current === "",
);

const hasMultipleLines = computed(
  () => (state.value?.lines?.length ?? 0) >= 2,
);

const statusLine = computed(() => {
  const st = state.value;
  if (!st) return "";
  if (st.current === "dev" || st.current === "") {
    return t("settings.updates.devDisabled");
  }
  if (st.error) return st.error;
  if (st.checking || checkingNow.value) return t("settings.updates.checking");
  if (st.ready) return t("settings.updates.readyToInstall", { version: st.latest });
  if (st.downloading) return t("settings.updates.downloadingStatus", { version: st.latest, pct: st.download_pct });
  if (st.available) return t("settings.updates.available", { version: st.latest });
  if (st.last_check_at > 0) return t("settings.updates.upToDate", { ago: formatAgo(st.last_check_at) });
  return t("settings.updates.notChecked");
});
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else-if="state">
      <div class="grid">
        <div class="kv">
          <span class="k">{{ t("settings.updates.currentVersion") }}</span>
          <span class="v">{{ state.current || t("common.unknown") }}</span>
        </div>
        <div class="kv">
          <span class="k">{{ t("settings.updates.status") }}</span>
          <span class="v">{{ statusLine }}</span>
        </div>
        <div
          v-if="state.download_path && (state.ready || state.downloading)"
          class="kv"
        >
          <span class="k">{{ t("settings.updates.downloadPath") }}</span>
          <span class="v path" :title="state.download_path">
            {{ state.download_path }}
          </span>
        </div>
      </div>

      <label v-if="!isDev" class="checkbox">
        <input
          type="checkbox"
          :checked="autoCheck"
          @change="onAutoCheckToggle"
        />
        {{ t("settings.updates.autoCheck") }}
      </label>

      <div v-if="!isDev" class="proxy-box">
        <label class="field-label" for="update-gh-proxy">{{ t("settings.updates.ghProxy") }}</label>
        <div class="proxy-row">
          <input
            id="update-gh-proxy"
            v-model.trim="ghProxyURL"
            type="url"
            placeholder="https://gh-proxy.example.com/"
            spellcheck="false"
          />
          <button :disabled="savingProxy" @click="onSaveProxy">
            {{ savingProxy ? t("settings.relay.saving") : t("common.save") }}
          </button>
        </div>
        <p class="hint">
          {{ t("settings.updates.proxyHint") }}
        </p>
      </div>

      <details v-if="!isDev && state.notes" class="notes">
        <summary>{{ t("settings.updates.releaseNotes") }}</summary>
        <pre>{{ state.notes }}</pre>
      </details>

      <div
        v-if="!isDev && hasMultipleLines && state.lines && !state.ready"
        class="version-lines"
      >
        <div
          v-for="line in state.lines"
          :key="line.minor"
          class="version-line"
        >
          <label>
            <input
              type="radio"
              :value="line.minor"
              v-model="selectedLine"
            />
            {{ t("settings.updates.versionLine", { minor: line.minor }) }} → {{ line.latest }}
          </label>
        </div>
      </div>

      <div v-if="!isDev" class="actions">
        <button
          @click="onCheckNow"
          :disabled="checkingNow || state.checking"
        >{{ t("settings.updates.checkNow") }}</button>
        <button
          v-if="hasMultipleLines && !state.ready && !state.downloading"
          class="primary"
          :disabled="state.downloading || !selectedLatest"
          @click="onDownloadSelected"
        >{{ t("settings.updates.downloadVersion", { version: selectedLatest }) }}</button>
        <button
          v-if="!hasMultipleLines && state.available && !state.ready && !state.downloading"
          class="primary"
          @click="onDownload"
        >{{ t("settings.updates.downloadVersion", { version: state.latest }) }}</button>
        <button
          v-if="state.downloading"
          class="primary danger"
          :disabled="cancelling"
          @click="onCancelDownload"
        >{{ cancelling
            ? t('settings.updates.cancelling')
            : t('settings.updates.cancelDownload', { pct: state.download_pct }) }}</button>
        <button
          v-if="state.ready"
          class="primary danger"
          @click="$emit('request-install', state.latest)"
        >{{ t('settings.updates.forceInstallRestart') }}</button>
        <button
          v-if="state.ready"
          class="secondary"
          @click="onRedownload"
        >{{ t('settings.updates.redownload') }}</button>
      </div>

      <p v-if="error" class="error">{{ error }}</p>
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.dim {
  color: var(--fg-dim);
  font-size: 13px;
}
.grid {
  display: grid;
  gap: 6px;
  font-size: 12px;
}
.kv {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.kv .k {
  color: var(--fg-dim);
  width: 130px;
}
.kv .v {
  color: var(--fg);
}
.kv .path {
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.proxy-box {
  display: grid;
  gap: 6px;
}
.field-label {
  color: var(--fg-dim);
  font-size: 12px;
}
.proxy-row {
  display: flex;
  gap: 8px;
}
.proxy-row input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 6px 10px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font: inherit;
  font-size: 13px;
}
.hint {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.4;
}
.notes {
  font-size: 12px;
  color: var(--fg);
}
.notes summary {
  color: var(--fg-dim);
  cursor: pointer;
}
.notes pre {
  background: var(--bg);
  border: 1px solid var(--border);
  padding: 8px;
  border-radius: 6px;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-height: 160px;
  overflow-y: auto;
  font-size: 11px;
}
.version-lines {
  display: grid;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.version-line label {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}
.actions {
  display: flex;
  gap: 8px;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
button {
  height: 32px;
  padding: 6px 14px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
button:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.04);
}
button:disabled {
  opacity: 0.5;
  cursor: default;
}
button.primary {
  background: var(--accent);
  color: #0d1117;
  border-color: var(--accent);
  font-weight: 600;
}
button.primary:hover:not(:disabled) {
  background: #79b8ff;
  border-color: #79b8ff;
}
button.primary.danger {
  background: var(--bad);
  color: #0d1117;
  border-color: var(--bad);
}
button.primary.danger:hover:not(:disabled) {
  background: #ff6f6a;
  border-color: #ff6f6a;
}
button.secondary {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
}
button.secondary:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
</style>
