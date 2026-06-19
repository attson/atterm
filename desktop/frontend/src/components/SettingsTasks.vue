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

const { t } = useI18n();

const expandByDefault = ref(true);
const presetIds: PresetId[] = ["iconOnly", "iconLabel"];
const groupByIds: TaskGroupBy[] = ["host", "state"];
const preset = useTaskPreset();
const groupBy = useTaskGroupBy();

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
</style>
