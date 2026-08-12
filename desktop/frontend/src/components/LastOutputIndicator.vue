<script setup lang="ts">
import { computed } from "vue";
import type { TaskState } from "../lib/taskState";
import { formatLastOutput } from "../lib/lastOutput";

const props = defineProps<{
  lastOutputAt?: number;
  taskState?: TaskState | string;
  nowMs: number;
}>();

const display = computed(() => formatLastOutput(props.lastOutputAt, props.taskState, props.nowMs));
</script>

<template>
  <span
    v-if="display"
    class="last-output"
    :class="{ live: display.live }"
    :title="display.title"
    :aria-label="display.title"
    data-test="last-output"
  >
    <svg class="last-output-clock" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" aria-hidden="true">
      <circle cx="6" cy="6" r="4.5" />
      <path d="M6 3.3v3l2 1.2" />
    </svg>
    <span>{{ display.text }}</span>
  </span>
</template>

<style scoped>
.last-output {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex: none;
  color: var(--fg-dim, #6e7681);
  white-space: nowrap;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 9px;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}
.last-output-clock { width: 10px; height: 10px; flex: none; }
.last-output.live { color: var(--accent, #58a6ff); }
.last-output.live .last-output-clock { animation: last-output-tick 1.2s steps(2, end) infinite; }
@keyframes last-output-tick {
  0%, 45% { opacity: 1; }
  46%, 100% { opacity: 0.45; }
}
@media (prefers-reduced-motion: reduce) {
  .last-output.live .last-output-clock { animation: none; }
}
</style>
