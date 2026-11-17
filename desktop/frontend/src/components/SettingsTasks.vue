<script setup lang="ts">
import { onMounted, ref } from "vue";
import { presets, type PresetId } from "../lib/taskState";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  type TaskGroupBy,
} from "../lib/api";
import { useTaskPreset } from "../composables/useTaskPreset";
import { useTaskGroupBy } from "../composables/useTaskGroupBy";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { RotateCcw } from "lucide-vue-next";
import {
  type SessionLabelColorKey,
  type SessionLabelMode,
  useSessionLabelAppearance,
} from "../composables/useSessionLabelAppearance";

const { t } = useI18n();

const expandByDefault = ref(true);
const presetIds: PresetId[] = ["iconOnly", "iconLabel"];
const groupByIds: TaskGroupBy[] = ["host", "state"];
const preset = useTaskPreset();
const groupBy = useTaskGroupBy();
const labelAppearance = useSessionLabelAppearance();
const labelColorKeys: SessionLabelColorKey[] = ["ssh", "codex", "claude"];

onMounted(async () => {
  try {
    const v = await getTaskPreset();
    if (v === "iconOnly" || v === "iconLabel") preset.activeId.value = v;
  } catch {
    /* fallback already applied */
  }
  try {
    const c = await getTaskSidebarCollapsed();
    expandByDefault.value = !c;
  } catch {
    /* default */
  }
});

async function onPresetChange(e: Event) {
  const id = (e.target as HTMLInputElement).value as PresetId;
  await preset.setPreset(id);
}
async function onToggleExpand(e: Event) {
  const checked = (e.target as HTMLInputElement).checked;
  expandByDefault.value = checked;
  await setTaskSidebarCollapsed(!checked);
}
async function onGroupByChange(e: Event) {
  const id = (e.target as HTMLInputElement).value as TaskGroupBy;
  await groupBy.setGroupBy(id);
}
function groupByLabel(id: TaskGroupBy): string {
  return id === "host" ? t("tasks.settings.groupByHost") : t("tasks.settings.groupByState");
}
function setLabelMode(mode: SessionLabelMode) {
  labelAppearance.setMode(mode);
}
function onLabelColorChange(key: SessionLabelColorKey, e: Event) {
  const input = e.target as HTMLInputElement;
  if (!labelAppearance.setColor(key, input.value)) {
    input.value = labelAppearance.colors.value[key];
  }
}
function labelPreviewStyle(key: SessionLabelColorKey) {
  return labelAppearance.mode.value === "category"
    ? { "--kind-color": labelAppearance.colors.value[key] }
    : undefined;
}
</script>

<template>
  <section class="settings-tasks">
    <div class="preset-list">
      <label v-for="id in presetIds" :key="id" class="preset-option">
        <input
          type="radio"
          name="preset"
          :value="id"
          :checked="preset.activeId.value === id"
          @change="onPresetChange"
        />
        <div class="preset-meta">
          <div class="preset-name">{{ t(`tasks.preset.${id}.name`) }}</div>
          <div class="preset-desc">{{ t(`tasks.preset.${id}.description`) }}</div>
          <div class="preset-preview">
            <TaskStateIcon state="running" :preset="presets[id]" />
            <TaskStateIcon state="waiting_input" :preset="presets[id]" />
            <TaskStateIcon state="completed" :preset="presets[id]" />
            <TaskStateIcon state="failed" :preset="presets[id]" />
          </div>
        </div>
      </label>
    </div>
    <div class="group-by-row" data-test="settings-group-by">
      <span class="group-by-label">{{ t("tasks.settings.groupBy") }}</span>
      <label v-for="id in groupByIds" :key="id" class="group-by-option">
        <input
          type="radio"
          name="taskGroupBy"
          :value="id"
          :checked="groupBy.activeId.value === id"
          @change="onGroupByChange"
        />
        {{ groupByLabel(id) }}
      </label>
    </div>
    <label class="expand-toggle">
      <input
        type="checkbox"
        :checked="expandByDefault"
        @change="onToggleExpand"
      />
      {{ t("tasks.settings.expandByDefault") }}
    </label>
    <div class="label-appearance" data-test="session-label-appearance">
      <div class="field-label">{{ t("tasks.settings.labelAppearance.title") }}</div>
      <div class="mode-switch" role="group" :aria-label="t('tasks.settings.labelAppearance.modeLabel')">
        <button
          type="button"
          :class="{ active: labelAppearance.mode.value === 'neutral' }"
          :aria-pressed="labelAppearance.mode.value === 'neutral'"
          data-test="session-label-mode-neutral"
          @click="setLabelMode('neutral')"
        >{{ t("tasks.settings.labelAppearance.neutral") }}</button>
        <button
          type="button"
          :class="{ active: labelAppearance.mode.value === 'category' }"
          :aria-pressed="labelAppearance.mode.value === 'category'"
          data-test="session-label-mode-category"
          @click="setLabelMode('category')"
        >{{ t("tasks.settings.labelAppearance.category") }}</button>
      </div>
      <p class="hint">{{ t("tasks.settings.labelAppearance.hint") }}</p>
      <div class="label-colors" :class="{ disabled: labelAppearance.mode.value === 'neutral' }">
        <div v-for="key in labelColorKeys" :key="key" class="label-color-row">
          <span
            class="kind-preview"
            :class="{ colored: labelAppearance.mode.value === 'category' }"
            :style="labelPreviewStyle(key)"
            :data-test="`session-label-preview-${key}`"
          >{{ key === 'ssh' ? 'SSH' : key }}</span>
          <input
            type="color"
            :value="labelAppearance.colors.value[key]"
            :disabled="labelAppearance.mode.value === 'neutral'"
            :aria-label="t(`tasks.settings.labelAppearance.${key}Color`)"
            :data-test="`session-label-color-${key}`"
            @input="onLabelColorChange(key, $event)"
          />
          <code>{{ labelAppearance.colors.value[key] }}</code>
          <button
            type="button"
            class="icon-button"
            :disabled="labelAppearance.mode.value === 'neutral'"
            :aria-label="t('tasks.settings.labelAppearance.resetOne', { label: key === 'ssh' ? 'SSH' : key })"
            :data-test="`session-label-reset-${key}`"
            @click="labelAppearance.resetColor(key)"
          ><RotateCcw :size="14" /></button>
        </div>
      </div>
      <button
        type="button"
        class="reset-all"
        :disabled="labelAppearance.mode.value === 'neutral'"
        data-test="session-label-reset-all"
        @click="labelAppearance.resetColors()"
      ><RotateCcw :size="14" />{{ t("tasks.settings.labelAppearance.resetAll") }}</button>
      <p v-if="labelAppearance.saveError.value" class="error" role="alert">
        {{ t("tasks.settings.labelAppearance.saveError") }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.section-title { font-size: 14px; font-weight: 500; margin: 0 0 12px; }
.preset-list { display: flex; flex-direction: column; gap: 12px; margin-bottom: 16px; }
.preset-option { display: flex; gap: 10px; padding: 10px; border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 4px; cursor: pointer; }
.preset-option:hover { background: rgba(255, 255, 255, 0.03); }
.preset-name { font-weight: 500; }
.preset-desc { font-size: 12px; opacity: 0.7; margin: 4px 0; }
.preset-preview { display: flex; gap: 10px; margin-top: 6px; }
.expand-toggle { display: flex; align-items: center; gap: 8px; padding: 6px 0; }
.group-by-row { display: flex; align-items: center; gap: 14px; margin-bottom: 10px; flex-wrap: wrap; }
.group-by-label { font-size: 13px; }
.group-by-option { display: flex; align-items: center; gap: 4px; font-size: 13px; cursor: pointer; }
.label-appearance { display: flex; flex-direction: column; align-items: flex-start; gap: 10px; margin-top: 8px; padding-top: 16px; border-top: 1px solid var(--border); width: 100%; }
.field-label { color: var(--fg-dim); font-size: 12px; text-transform: uppercase; letter-spacing: 0.05em; }
.hint, .error { margin: 0; font-size: 12px; line-height: 1.45; color: var(--fg-dim); }
.error { color: var(--bad); }
.mode-switch { display: inline-flex; border: 1px solid var(--border); border-radius: 5px; overflow: hidden; }
.mode-switch button { height: 30px; padding: 0 14px; border: 0; border-radius: 0; background: var(--bg); color: var(--fg); }
.mode-switch button + button { border-left: 1px solid var(--border); }
.mode-switch button.active { background: var(--accent); color: #0d1117; }
.label-colors { display: flex; flex-direction: column; gap: 8px; width: min(100%, 360px); }
.label-colors.disabled { opacity: 0.55; }
.label-color-row { display: grid; grid-template-columns: 64px 28px minmax(74px, 1fr) 28px; align-items: center; gap: 10px; min-width: 0; }
.label-color-row input[type="color"] { width: 28px; height: 28px; padding: 2px; border: 1px solid var(--border); border-radius: 4px; background: var(--bg); }
.label-color-row code { min-width: 0; color: var(--fg-dim); font-size: 12px; }
.kind-preview { justify-self: start; font-size: 9px; line-height: 1.4; padding: 1px 4px; border-radius: 3px; border: 1px solid var(--border); color: var(--fg-dim); }
.kind-preview.colored { color: var(--kind-color); border-color: color-mix(in srgb, var(--kind-color) 50%, transparent); background: color-mix(in srgb, var(--kind-color) 10%, transparent); }
.icon-button, .reset-all { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 28px; border: 1px solid var(--border); border-radius: 5px; background: var(--bg); color: var(--fg); }
.icon-button { width: 28px; padding: 0; }
.reset-all { padding: 0 10px; }
button:disabled, input:disabled { cursor: not-allowed; }
</style>
