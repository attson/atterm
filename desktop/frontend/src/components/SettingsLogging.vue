<script lang="ts" setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  getLogPreview,
  getLoggingConfig,
  pickLogFilePath,
  setLoggingConfig,
  type LogPreview,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import LogLines from "./LogLines.vue";
import type { LogLevel } from "../lib/parseLogLine";

defineEmits<{
  (e: "open-log-viewer"): void;
}>();

const enabled = ref(true);
const path = ref("");
const effectivePath = ref("");
const loading = ref(true);
const error = ref("");
const { t } = useI18n();

// Inline log tail: refresh every 3 s while the panel is mounted so the
// user can watch new lines land without opening the full-screen viewer.
const tail = ref<LogPreview | null>(null);
const tailError = ref("");
const tailLoading = ref(false);
let tailTimer: number | null = null;
const tailEl = ref<any>(null);
const tailMinLevel = ref<LogLevel>("DEBUG");

async function refreshTail() {
  if (!enabled.value) return;
  tailLoading.value = true;
  tailError.value = "";
  try {
    tail.value = await getLogPreview();
  } catch (e: any) {
    tailError.value = e?.message ?? String(e);
  } finally {
    tailLoading.value = false;
  }
  await nextTick();
  const el = (tailEl.value as any)?.$el as HTMLElement | undefined;
  if (el) el.scrollTop = el.scrollHeight;
}

watch(() => tail.value?.content, () => {
  /* auto-scroll handled inside refreshTail */
});

onMounted(async () => {
  try {
    const cfg = await getLoggingConfig();
    enabled.value = cfg.enabled;
    path.value = cfg.path;
    effectivePath.value = cfg.effective_path;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    loading.value = false;
  }
  await refreshTail();
  tailTimer = window.setInterval(refreshTail, 3000);
});

onBeforeUnmount(() => {
  if (tailTimer !== null) {
    window.clearInterval(tailTimer);
    tailTimer = null;
  }
});

async function onToggle(e: Event) {
  const target = e.target as HTMLInputElement;
  const previous = enabled.value;
  enabled.value = target.checked;
  error.value = "";
  try {
    await setLoggingConfig({ enabled: target.checked, path: path.value });
    const cfg = await getLoggingConfig();
    enabled.value = cfg.enabled;
    path.value = cfg.path;
    effectivePath.value = cfg.effective_path;
  } catch (e: any) {
    enabled.value = previous;
    error.value = e?.message ?? String(e);
  }
}

async function onPickPath() {
  error.value = "";
  try {
    const picked = await pickLogFilePath();
    if (!picked) return;
    await setLoggingConfig({ enabled: enabled.value, path: picked });
    const cfg = await getLoggingConfig();
    enabled.value = cfg.enabled;
    path.value = cfg.path;
    effectivePath.value = cfg.effective_path;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}

async function onResetPath() {
  error.value = "";
  try {
    await setLoggingConfig({ enabled: enabled.value, path: "" });
    const cfg = await getLoggingConfig();
    enabled.value = cfg.enabled;
    path.value = cfg.path;
    effectivePath.value = cfg.effective_path;
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  }
}
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <label class="checkbox">
        <input
          type="checkbox"
          :checked="enabled"
          @change="onToggle"
        />
        {{ t("settings.logging.writeLogs") }}
      </label>

      <div class="kv">
        <span class="k">{{ t("settings.logging.currentFile") }}</span>
        <span class="v path" :title="effectivePath">{{ effectivePath }}</span>
      </div>

      <div class="actions">
        <button @click="onPickPath">{{ t("settings.logging.changeLocation") }}</button>
        <button @click="onResetPath">{{ t("settings.logging.resetDefault") }}</button>
        <button @click="$emit('open-log-viewer')">{{ t("settings.logging.viewLogs") }}</button>
      </div>

      <p v-if="error" class="error">{{ error }}</p>

      <section v-if="enabled" class="tail-wrap">
        <header class="tail-header">
          <span class="tail-label">{{ t("settings.logging.liveTail") }}</span>
          <select v-model="tailMinLevel" class="tail-level">
            <option value="DEBUG">DEBUG+</option>
            <option value="INFO">INFO+</option>
            <option value="WARN">WARN+</option>
            <option value="ERROR">ERROR</option>
          </select>
          <button class="tail-refresh" :disabled="tailLoading" @click="refreshTail">
            {{ t("common.refresh") }}
          </button>
        </header>
        <p v-if="tailError" class="tail-error">{{ tailError }}</p>
        <p v-else-if="!tail || !tail.exists" class="tail-empty">
          {{ t("settings.logging.noContent") }}
        </p>
        <LogLines v-else ref="tailEl" class="tail-content" :content="tail.content" :minLevel="tailMinLevel" />
      </section>
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
.checkbox {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--fg);
}
.kv {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  font-size: 12px;
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
button:hover {
  background: rgba(255, 255, 255, 0.04);
}
.tail-wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}
.tail-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.tail-label {
  font-size: 12px;
  color: var(--fg-dim);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex: 1 1 auto;
}
.tail-refresh {
  height: 24px;
  padding: 2px 10px;
  font-size: 12px;
}
.tail-level { height: 24px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; font-size: 12px; }
.tail-empty {
  color: var(--fg-dim);
  font-size: 12px;
  margin: 0;
}
.tail-error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.tail-content {
  margin: 0;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px;
  color: var(--fg);
  font-size: 11px;
  line-height: 1.45;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  max-height: 280px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
