<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import type { TaskState, TaskStatePreset } from "../lib/taskState";
import { useTaskPreset } from "../composables/useTaskPreset";
import { useRunningSpin } from "../composables/useRunningSpin";

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

// Running icons rotate off one shared, throttled rAF clock (see useRunningSpin)
// instead of a per-copy CSS animation, which is what pinned the renderer CPU on
// GPU-less machines. Reference-count the clock only while this icon is running,
// so the loop stops the moment the last running icon unmounts or leaves the
// running state.
const spin = useRunningSpin();
const isRunning = computed(() => props.state === "running");
// 12 steps per turn at ~8fps ≈ 1.5s per revolution — the cadence the old
// spinnerDurationMs used.
const runAngle = computed(() => (isRunning.value ? (spin.phase.value * 30) % 360 : 0));
const runArcTransform = computed(() =>
  isRunning.value ? `rotate(${runAngle.value} 8 8)` : undefined,
);

let held = false;
watch(
  isRunning,
  (running) => {
    if (running && !held) {
      held = true;
      spin.acquire();
    } else if (!running && held) {
      held = false;
      spin.release();
    }
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  if (held) {
    held = false;
    spin.release();
  }
});
</script>

<template>
  <span
    class="task-state-icon"
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
        <!-- 3/4 arc, spun via the shared clock's transform (no per-copy CSS
             animation). rotate(deg 8 8) turns it about the icon centre and is
             unaffected by the parent unread scale. -->
        <path
          v-if="state === 'running'"
          class="task-running-arc"
          d="M14 8 a6 6 0 1 1 -3 -5.196"
          :stroke="glyphColor"
          stroke-width="2"
          stroke-linecap="round"
          :transform="runArcTransform"
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
