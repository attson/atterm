<script lang="ts" setup>
import { nextTick, ref, watch } from "vue";
import type { LogPreview } from "../lib/api";
import { useI18n } from "../i18n/useI18n";
import LogLines from "./LogLines.vue";
import type { LogLevel } from "../lib/parseLogLine";

const props = defineProps<{
  preview: LogPreview;
  loading?: boolean;
  error?: string;
}>();

const emit = defineEmits<{
  (e: "close"): void;
  (e: "refresh"): void;
}>();

const { t } = useI18n();
const minLevel = ref<LogLevel>("DEBUG");

// Logs are append-only and the user almost always wants the latest tail.
// Auto-scroll the <pre> to the bottom whenever new content lands (mount
// and every refresh). Keep it simple: always jump to bottom; if the user
// wanted to read an older section they can refresh to come back here.
const contentEl = ref<any>(null);
async function scrollToBottom() {
  await nextTick();
  const el = (contentEl.value as any)?.$el as HTMLElement | undefined;
  if (el) el.scrollTop = el.scrollHeight;
}
watch(() => props.preview.content, () => { void scrollToBottom(); }, { immediate: true });

async function copyContent() {
  await navigator.clipboard.writeText(props.preview.content);
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <div class="header">
        <h2>{{ t("settings.logging.title") }}</h2>
        <div class="path" :title="props.preview.path">{{ props.preview.path }}</div>
      </div>

      <div v-if="props.error" class="error">{{ props.error }}</div>
      <div v-else-if="props.loading" class="empty">{{ t("common.loading") }}</div>
      <div v-else-if="!props.preview.exists" class="empty">
        {{ t("settings.logging.noContent") }}
      </div>
      <div v-else class="content-wrap">
        <div v-if="props.preview.truncated" class="hint">
          {{ t("settings.logging.truncated") }}
        </div>
        <LogLines ref="contentEl" class="content" :content="props.preview.content" :minLevel="minLevel" />
      </div>

      <div class="row">
        <label class="lvl-filter">{{ t("settings.logging.levelFilter") }}
          <select v-model="minLevel">
            <option value="DEBUG">DEBUG+</option>
            <option value="INFO">INFO+</option>
            <option value="WARN">WARN+</option>
            <option value="ERROR">ERROR</option>
          </select>
        </label>
        <button @click="emit('refresh')">{{ t("common.refresh") }}</button>
        <button @click="copyContent" :disabled="!props.preview.content">{{ t("common.copy") }}</button>
        <button class="primary" @click="emit('close')">{{ t("common.close") }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 110;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: min(860px, calc(100vw - 32px));
  max-height: calc(100vh - 32px);
  box-sizing: border-box;
  display: flex; flex-direction: column; gap: 12px;
}
.header {
  display: flex; flex-direction: column; gap: 4px;
}
.header h2 {
  margin: 0; font-size: 14px; font-weight: 600;
  letter-spacing: 0.05em; text-transform: uppercase; color: var(--fg-dim);
}
.path {
  color: var(--fg-dim);
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow-wrap: anywhere;
}
.content-wrap {
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.content {
  margin: 0;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px;
  overflow: auto;
  min-height: 200px;
  max-height: calc(100vh - 220px);
  color: var(--fg);
  font-size: 11px;
  line-height: 1.45;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.row {
  display: flex; justify-content: flex-end; gap: 8px;
}
.hint,
.empty {
  color: var(--fg-dim);
  font-size: 12px;
}
.error {
  color: var(--bad);
  font-size: 12px;
}
.primary {
  background: var(--accent); color: #0d1117; border-color: var(--accent);
  font-weight: 600;
}
.lvl-filter { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--fg-dim); margin-right: auto; }
.lvl-filter select { height: 26px; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; }
</style>
