<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  cancelDownload,
  getUpdateState,
  installUpdate,
  startDownload,
  type UpdateState,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const emit = defineEmits<{ (e: "dismiss"): void }>();

const { t } = useI18n();

const state = ref<UpdateState | null>(null);
const clickInFlight = ref(false);
const cancelling = ref(false);
let pollHandle: number | null = null;

onMounted(async () => {
  try {
    state.value = await getUpdateState();
  } catch {
    /* poll below refreshes; never trap boot on an updater failure */
  }
  pollHandle = window.setInterval(async () => {
    try {
      state.value = await getUpdateState();
    } catch {
      /* ignore — the ⚙ badge already surfaces updater health */
    }
  }, 2000);
});

onBeforeUnmount(() => {
  if (pollHandle !== null) window.clearInterval(pollHandle);
});

const statusLine = computed(() => {
  const st = state.value;
  if (!st) return "";
  if (st.error) return st.error;
  if (st.ready) return t("startupUpdate.statusReady", { version: st.latest });
  if (st.downloading)
    return t("startupUpdate.statusDownloading", { version: st.latest, pct: st.download_pct });
  return t("startupUpdate.statusAvailable", { version: st.latest });
});

async function onDownload() {
  clickInFlight.value = true;
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  } finally {
    clickInFlight.value = false;
  }
}

async function onCancel() {
  cancelling.value = true;
  try {
    await cancelDownload();
  } catch {
    /* poll surfaces error */
  } finally {
    cancelling.value = false;
  }
}

async function onInstall() {
  try {
    await installUpdate();
  } catch {
    /* state.error reflects in poll */
  }
}
</script>

<template>
  <div class="startup-update-backdrop" role="dialog" aria-modal="true">
    <div class="startup-update-dialog">
      <header>
        <h2>{{ t("startupUpdate.title") }}</h2>
        <p class="subtitle">
          {{ t("startupUpdate.currentToLatest", { current: state?.current ?? "", latest: state?.latest ?? "" }) }}
        </p>
      </header>

      <p class="status">{{ statusLine }}</p>

      <details v-if="state?.notes" class="notes">
        <summary>{{ t("startupUpdate.releaseNotes") }}</summary>
        <pre>{{ state.notes }}</pre>
      </details>

      <footer>
        <button
          data-testid="startup-update-later"
          class="btn-secondary"
          @click="emit('dismiss')"
        >{{ t("startupUpdate.later") }}</button>
        <button
          v-if="state?.ready"
          data-testid="startup-update-install"
          class="btn-primary"
          @click="onInstall"
        >{{ t("startupUpdate.installRestart") }}</button>
        <button
          v-else-if="state?.downloading"
          data-testid="startup-update-cancel"
          class="btn-primary danger"
          :disabled="cancelling"
          @click="onCancel"
        >{{ cancelling ? t("startupUpdate.cancelling") : t("startupUpdate.cancel", { pct: state?.download_pct ?? 0 }) }}</button>
        <button
          v-else
          data-testid="startup-update-download"
          class="btn-primary"
          :disabled="clickInFlight"
          @click="onDownload"
        >{{ t("startupUpdate.downloadInstall") }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.startup-update-backdrop {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
  z-index: 210;
}
.startup-update-dialog {
  background: var(--bg, #0d1117);
  color: var(--fg, #d1d5db);
  border-radius: 8px;
  min-width: 420px; max-width: 640px; max-height: 80vh;
  overflow: hidden; display: flex; flex-direction: column;
  padding: 16px 20px;
  gap: 12px;
}
.startup-update-dialog header { display: flex; flex-direction: column; gap: 4px; }
.startup-update-dialog h2 { font-size: 1.1rem; margin: 0; }
.subtitle { font-size: 0.85rem; opacity: 0.7; margin: 0; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.status { font-size: 0.85rem; margin: 0; }
.notes { font-size: 12px; }
.notes summary { opacity: 0.7; cursor: pointer; }
.notes pre {
  background: var(--bg); border: 1px solid var(--border, #444);
  padding: 8px; border-radius: 6px; white-space: pre-wrap; word-break: break-word;
  max-height: 160px; overflow-y: auto; font-size: 11px; margin: 6px 0 0;
}
footer { display: flex; justify-content: flex-end; gap: 8px; }
.btn-primary {
  background: var(--accent, #2563eb); color: #0d1117; border: 0;
  padding: 6px 12px; border-radius: 4px; cursor: pointer; font-weight: 600;
}
.btn-primary.danger { background: var(--bad, #ef4444); }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn-secondary {
  background: transparent; color: inherit; border: 1px solid var(--border, #444);
  padding: 6px 12px; border-radius: 4px; cursor: pointer;
}
</style>
