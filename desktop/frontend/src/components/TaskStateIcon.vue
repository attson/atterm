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

// Unread renders the *same* state glyph in a heavier weight — a solid
// state-colored disc with the glyph knocked out — plus a state-colored corner
// dot. Two things stay true that the previous four-point star broke: the row
// still says which state it is (the star replaced the glyph, so an unread row
// could not distinguish waiting from finished), and every state can carry
// unread, so TabBar no longer needs a second marker of its own.
const showUnread = computed(() => props.unread);
const color = computed(() => preset.value.colorOf(props.state));
// Knockout colour for the glyph inside a filled disc. Hardcoded rather than
// var(--bg): the desk widget has its own stylesheet and never defines the
// theme variables, so var(--bg) would fail to resolve there and fall back to
// black. taskState.ts hardcodes the state palette for the same reason.
const KNOCKOUT = "#0d1117";
const glyphColor = computed(() => (showUnread.value ? KNOCKOUT : color.value));
const spinMs = computed(() => preset.value.spinnerDurationMs(props.state));
// No pulse while unread: the filled disc plus the dot already carry the row,
// and stacking a fade on top only makes a list of unread rows restless.
const pulse = computed(() => !showUnread.value && preset.value.animatePulse(props.state));
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
      style="overflow: visible"
    >
      <!-- Unread backdrop: the glyph below is drawn in the knockout colour and
           scaled to fit inside this disc, so the shape reads as a hole. -->
      <circle
        v-if="showUnread"
        class="task-unread-disc"
        cx="8"
        cy="8"
        r="7.5"
        :fill="color"
      />
      <g :transform="showUnread ? 'translate(8,8) scale(0.62) translate(-8,-8)' : undefined">
        <!-- running: 3/4 arc, spinning -->
        <path
          v-if="state === 'running'"
          class="task-spinner"
          d="M14 8 a6 6 0 1 1 -3 -5.196"
          :stroke="glyphColor"
          stroke-width="2"
          stroke-linecap="round"
          :style="{ animationDuration: spinMs + 'ms' }"
        />
        <!-- completed: check mark -->
        <path
          v-else-if="state === 'completed'"
          class="task-completed-check"
          d="M3 8 l3 3 l7 -7"
          :stroke="glyphColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
        <!-- failed: X -->
        <path
          v-else-if="state === 'failed'"
          d="M4 4 L12 12 M12 4 L4 12"
          :stroke="glyphColor"
          stroke-width="2"
          stroke-linecap="round"
        />
        <!-- waiting_input: terminal prompt -->
        <path
          v-else-if="state === 'waiting_input'"
          class="task-waiting-prompt"
          d="M3 4.5 L6.5 8 L3 11.5 M8 11.5 H13"
          :stroke="glyphColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
        <!-- idle / disconnected / closed: small dot. Bigger inside a disc so
             it survives the 0.62 scale. -->
        <circle v-else cx="8" cy="8" :r="showUnread ? 3.2 : 2" :fill="glyphColor" />
      </g>
      <!-- Corner dot. The ring beneath it is painted in the knockout colour so
           the dot stays legible against the disc it sits on. -->
      <template v-if="showUnread">
        <circle cx="12.6" cy="3.4" r="3.3" :fill="KNOCKOUT" />
        <circle class="task-unread-dot" cx="12.6" cy="3.4" r="2.5" :fill="color" />
      </template>
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
