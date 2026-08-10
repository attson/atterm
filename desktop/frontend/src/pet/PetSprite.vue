<script lang="ts" setup>
import { computed } from "vue";
import type { PetMood } from "../lib/petState";

/**
 * The pet itself: one rounded blob whose eyes, mouth and colour are driven by
 * mood. Geometry rather than a sprite sheet so state changes tween instead of
 * cutting between frames, and so it stays crisp at any DPI.
 *
 * Deliberately not emoji: red line #13 requires inline SVG for UI glyphs
 * because platform emoji fonts render inconsistently (and are missing
 * outright on some Linux setups).
 */
const props = withDefaults(
  defineProps<{ mood: PetMood; size?: number; muted?: boolean }>(),
  { size: 40, muted: false },
);

const BODY = "#0d1117";

const COLOR: Record<PetMood, string> = {
  idle: "#57606a",
  running: "#2f81f7",
  waiting: "#d29922",
  failed: "#f85149",
};

const fill = computed(() => COLOR[props.mood]);

// Motion is the pet's whole point, so it is tied to mood — but muting kills
// it. A muted pet still shows colour and counts; muting suppresses movement,
// not information.
const animClass = computed(() => {
  if (props.muted) return "";
  return `anim-${props.mood}`;
});
</script>

<template>
  <svg
    :class="animClass"
    :height="size"
    :width="size"
    aria-hidden="true"
    viewBox="0 0 56 56"
  >
    <path
      :fill="fill"
      class="body"
      d="M10 14q0 -6 5 -3l5 3q8 -2 16 0l5 -3q5 -3 5 3v18a18 18 0 0 1 -36 0z"
    />

    <template v-if="mood === 'idle'">
      <path
        :stroke="BODY"
        d="M15 27q4 -4 8 0M33 27q4 -4 8 0"
        fill="none"
        stroke-linecap="round"
        stroke-width="3"
      />
      <path
        :stroke="BODY"
        d="M24 36q3 2 6 0"
        fill="none"
        stroke-linecap="round"
        stroke-width="2.5"
      />
    </template>

    <template v-else-if="mood === 'running'">
      <circle :fill="BODY" cx="20" cy="27" r="3.2" />
      <circle :fill="BODY" cx="36" cy="27" r="3.2" />
      <path
        :stroke="BODY"
        d="M23 35q5 4 10 0"
        fill="none"
        stroke-linecap="round"
        stroke-width="2.5"
      />
    </template>

    <template v-else-if="mood === 'waiting'">
      <circle :fill="BODY" cx="20" cy="26" r="4.6" />
      <circle :fill="BODY" cx="36" cy="26" r="4.6" />
      <circle cx="21.4" cy="24.6" fill="#fff" r="1.5" />
      <circle cx="37.4" cy="24.6" fill="#fff" r="1.5" />
      <ellipse :fill="BODY" cx="28" cy="37" rx="4" ry="3.2" />
    </template>

    <template v-else>
      <path
        :stroke="BODY"
        d="M16 24l7 7M23 24l-7 7M33 24l7 7M40 24l-7 7"
        stroke-linecap="round"
        stroke-width="2.6"
      />
      <path
        :stroke="BODY"
        d="M22 38q6 -4 12 0"
        fill="none"
        stroke-linecap="round"
        stroke-width="2.5"
      />
    </template>
  </svg>
</template>

<style scoped>
svg {
  display: block;
  overflow: visible;
}

.body {
  transition: fill 0.35s ease;
}

.anim-idle {
  animation: breathe 3.4s ease-in-out infinite;
}
.anim-running {
  animation: bob 1s ease-in-out infinite;
}
.anim-waiting {
  animation: jump 0.6s cubic-bezier(0.3, 0.7, 0.4, 1) infinite;
}
.anim-failed {
  animation: droop 2.6s ease-in-out infinite;
}

@keyframes breathe {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.05);
  }
}
@keyframes bob {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-4px);
  }
}
@keyframes jump {
  0%,
  100% {
    transform: translateY(0);
  }
  35% {
    transform: translateY(-6px);
  }
  62% {
    transform: translateY(0);
  }
}
@keyframes droop {
  0%,
  100% {
    transform: translateY(0) rotate(0);
  }
  50% {
    transform: translateY(3px) rotate(-5deg);
  }
}

/* Respect the OS "reduce motion" setting — a perpetually bouncing always-on-top
   window is exactly what that setting exists to stop. Colour still conveys
   state without any movement. */
@media (prefers-reduced-motion: reduce) {
  .anim-idle,
  .anim-running,
  .anim-waiting,
  .anim-failed {
    animation: none;
  }
}
</style>
