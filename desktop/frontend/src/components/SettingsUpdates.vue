<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  checkUpdate,
  getAutoCheckUpdates,
  getUpdateState,
  setAutoCheckUpdates,
  startDownload,
  type UpdateState,
} from "../lib/api";

defineEmits<{
  (e: "request-install", version: string): void;
}>();

const state = ref<UpdateState | null>(null);
const autoCheck = ref(true);
const checkingNow = ref(false);
const loading = ref(true);
const error = ref("");
let pollHandle: number | null = null;

onMounted(async () => {
  try {
    const [st, ac] = await Promise.all([getUpdateState(), getAutoCheckUpdates()]);
    state.value = st;
    autoCheck.value = ac;
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
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  }
}

async function onAutoCheckToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  autoCheck.value = target.checked;
  await setAutoCheckUpdates(target.checked);
}

const isDev = computed(
  () => state.value?.current === "dev" || state.value?.current === "",
);

const statusLine = computed(() => {
  const st = state.value;
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
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">loading…</div>
    <template v-else-if="state">
      <div class="grid">
        <div class="kv">
          <span class="k">current version</span>
          <span class="v">{{ state.current || "(unknown)" }}</span>
        </div>
        <div class="kv">
          <span class="k">status</span>
          <span class="v">{{ statusLine }}</span>
        </div>
        <div
          v-if="state.download_path && (state.ready || state.downloading)"
          class="kv"
        >
          <span class="k">download path</span>
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
        automatically check for updates
      </label>

      <details v-if="!isDev && state.notes" class="notes">
        <summary>release notes</summary>
        <pre>{{ state.notes }}</pre>
      </details>

      <div v-if="!isDev" class="actions">
        <button
          @click="onCheckNow"
          :disabled="checkingNow || state.checking"
        >check now</button>
        <button
          v-if="state.available && !state.ready && !state.downloading"
          class="primary"
          @click="onDownload"
        >download {{ state.latest }}</button>
        <button
          v-if="state.downloading"
          class="primary"
          disabled
        >downloading… {{ state.download_pct }}%</button>
        <button
          v-if="state.ready"
          class="primary danger"
          @click="$emit('request-install', state.latest)"
        >force install &amp; restart</button>
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
</style>
