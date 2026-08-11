<script setup lang="ts">
import { computed } from "vue";
import type { TaskState, TaskStatePreset } from "../lib/taskState";
import { useTaskPreset } from "../composables/useTaskPreset";

// Renders the per-session state indicator as pure inline SVG. Previously
// dispatched on text glyphs (·, ◐, ✓, ✗) via preset.glyphOf, but iOS 26.3
// WKWebView failed to resolve the ◐ / ✓ / ✗ glyphs from the CJK-first
// font stack and rendered them as .notdef "?" boxes. SVG paths eliminate
// the OS font dependency entirely.
const props = withDefaults(
  defineProps<{
    state: TaskState;
    size?: number;
    preset?: TaskStatePreset;
    unread?: boolean;
  }>(),
  { size: 12, unread: false },
);

const fallback = useTaskPreset();
const preset = computed(() => props.preset ?? fallback.active.value);

const showUnread = computed(() => props.state === "completed" && props.unread);
const color = computed(() => (showUnread.value ? "#e6edf3" : preset.value.colorOf(props.state)));
const spinMs = computed(() => preset.value.spinnerDurationMs(props.state));
const pulse = computed(() => preset.value.animatePulse(props.state));
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
    :data-state="state"
    :data-unread="showUnread ? 'true' : undefined"
  >
    <svg
      :width="size"
      :height="size"
      viewBox="0 0 16 16"
      fill="none"
      aria-hidden="true"
    >
      <!-- running: 3/4 arc, spinning -->
      <path
        v-if="state === 'running'"
        class="task-spinner"
        d="M14 8 a6 6 0 1 1 -3 -5.196"
        :stroke="color"
        stroke-width="2"
        stroke-linecap="round"
        :style="{ animationDuration: spinMs + 'ms' }"
      />
      <!-- unread completed: unread is the main state until the row is seen -->
      <circle
        v-else-if="showUnread"
        class="task-unread-dot"
        cx="8"
        cy="8"
        r="4"
        :fill="color"
      />
      <!-- completed: check mark -->
      <path
        v-else-if="state === 'completed'"
        class="task-completed-check"
        d="M3 8 l3 3 l7 -7"
        :stroke="color"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
      <!-- failed: X -->
      <path
        v-else-if="state === 'failed'"
        d="M4 4 L12 12 M12 4 L4 12"
        :stroke="color"
        stroke-width="2"
        stroke-linecap="round"
      />
      <!-- waiting_input: half-filled circle -->
      <g v-else-if="state === 'waiting_input'">
        <circle cx="8" cy="8" r="6" :stroke="color" stroke-width="1.5" fill="none" />
        <path d="M8 2 A6 6 0 0 0 8 14 Z" :fill="color" />
      </g>
      <!-- idle / disconnected / closed: small dot -->
      <circle v-else cx="8" cy="8" r="2" :fill="color" />
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
  transform-origin: 50% 50%;
}
@keyframes task-pulse {
  from { opacity: 0.5; }
  to { opacity: 1; }
}
@keyframes task-spin {
  to { transform: rotate(360deg); }
}
</style>
