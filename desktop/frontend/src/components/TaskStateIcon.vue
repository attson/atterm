<script setup lang="ts">
import { computed } from "vue";
import type { TaskState, TaskStatePreset } from "../lib/taskState";
import { displayForType, type DisplayKey } from "../lib/sessionType";
import { useTaskPreset } from "../composables/useTaskPreset";

const props = withDefaults(
  defineProps<{
    state: TaskState;
    type?: DisplayKey;
    size?: number;
    preset?: TaskStatePreset;
  }>(),
  { size: 12 },
);

const fallback = useTaskPreset();
const preset = computed(() => props.preset ?? fallback.active.value);

const color = computed(() => preset.value.colorOf(props.state));
const glyph = computed(() => preset.value.glyphOf(props.state));
const spinMs = computed(() => preset.value.spinnerDurationMs(props.state));
const pulse = computed(() => preset.value.animatePulse(props.state));
const typeDisplay = computed(() =>
  preset.value.showTypeIcon && props.type ? displayForType(props.type) : null,
);
</script>

<template>
  <span
    class="task-state-icon"
    :class="{ pulse }"
    :style="{
      color,
      display: 'inline-flex',
      alignItems: 'center',
      gap: '2px',
    }"
  >
    <svg
      v-if="glyph === 'spinner'"
      class="task-spinner"
      :width="size"
      :height="size"
      viewBox="0 0 16 16"
      fill="none"
      :stroke="color"
      stroke-width="2"
      stroke-linecap="round"
      :style="{ animationDuration: spinMs + 'ms' }"
      aria-hidden="true"
    >
      <!-- 3/4 arc -->
      <path d="M14 8 a6 6 0 1 1 -3 -5.196" />
    </svg>
    <span
      v-else
      class="task-glyph"
      aria-hidden="true"
      :style="{ fontSize: size + 'px', lineHeight: 1 }"
    >
      {{ glyph }}
    </span>
    <svg
      v-if="typeDisplay"
      class="task-type"
      :width="size"
      :height="size"
      viewBox="0 0 16 16"
      fill="none"
      :stroke="typeDisplay.color"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path :d="typeDisplay.iconPath" />
    </svg>
  </span>
</template>

<style scoped>
.task-state-icon.pulse {
  animation: task-pulse 1.2s ease-in-out infinite alternate;
}
.task-spinner {
  /* duration is set via inline style based on preset.spinnerDurationMs */
  animation: task-spin 1s linear infinite;
}
@keyframes task-pulse {
  from { opacity: 0.5; }
  to { opacity: 1; }
}
@keyframes task-spin {
  to { transform: rotate(360deg); }
}
</style>
