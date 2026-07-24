<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  count: number;
  openCount: number;
  canMerge: boolean;
}>();

const emit = defineEmits<{
  (e: "merge"): void;
  (e: "close-selected"): void;
  (e: "clear"): void;
}>();

const { t } = useI18n();

const mergeTitle = computed(() => {
  if (props.count > 4) return t("tasks.bulk.mergeTooMany");
  return t("tasks.bulk.mergeTab");
});

const closeTitle = computed(() => {
  if (props.openCount === 0) return t("tasks.bulk.closeNoneOpen");
  return t("tasks.bulk.closeHint", { count: props.openCount });
});

function onMerge() {
  if (!props.canMerge) return;
  emit("merge");
}

function onClose() {
  if (props.openCount === 0) return;
  emit("close-selected");
}

function onClear() {
  emit("clear");
}
</script>

<template>
  <div class="bulk-bar" data-test="bulk-action-bar" role="toolbar">
    <span class="counter" data-test="bulk-selected-count">
      {{ t("tasks.bulk.selectedCount", { count }) }}
    </span>
    <button
      class="btn primary"
      data-test="bulk-merge"
      type="button"
      :disabled="!canMerge"
      :title="mergeTitle"
      @click="onMerge"
    >
      {{ t("tasks.bulk.mergeTab") }} ({{ count }})
    </button>
    <button
      class="btn"
      data-test="bulk-close"
      type="button"
      :disabled="openCount === 0"
      :title="closeTitle"
      @click="onClose"
    >
      {{ t("tasks.bulk.close", { count: openCount }) }}
    </button>
    <button
      class="btn ghost"
      data-test="bulk-clear"
      type="button"
      :title="t('tasks.bulk.cancel')"
      @click="onClear"
    >
      {{ t("tasks.bulk.cancel") }}
    </button>
  </div>
</template>

<style scoped>
.bulk-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 6px 4px;
}
.counter {
  font-size: 11px;
  color: var(--fg-dim);
  margin-right: 4px;
  white-space: nowrap;
}
.btn {
  background: none;
  border: 1px solid rgba(255, 255, 255, 0.14);
  color: inherit;
  cursor: pointer;
  padding: 3px 8px;
  font-size: 11px;
  border-radius: 3px;
  line-height: 1.4;
}
.btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.06);
}
.btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.btn.primary {
  border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  color: var(--accent);
}
.btn.primary:hover:not(:disabled) {
  background: color-mix(in srgb, var(--accent) 12%, transparent);
}
.btn.ghost {
  border-color: transparent;
  opacity: 0.75;
}
</style>
