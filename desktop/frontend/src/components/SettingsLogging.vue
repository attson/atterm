<script lang="ts" setup>
import { onMounted, ref } from "vue";
import {
  getLoggingConfig,
  pickLogFilePath,
  setLoggingConfig,
} from "../lib/api";

defineEmits<{
  (e: "open-log-viewer"): void;
}>();

const enabled = ref(true);
const path = ref("");
const effectivePath = ref("");
const loading = ref(true);
const error = ref("");

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
    <div v-if="loading" class="dim">loading…</div>
    <template v-else>
      <label class="checkbox">
        <input
          type="checkbox"
          :checked="enabled"
          @change="onToggle"
        />
        write logs to file
      </label>

      <div class="kv">
        <span class="k">current log file</span>
        <span class="v path" :title="effectivePath">{{ effectivePath }}</span>
      </div>

      <div class="actions">
        <button @click="onPickPath">change location</button>
        <button @click="onResetPath">reset default</button>
        <button @click="$emit('open-log-viewer')">view logs</button>
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
</style>
