<script lang="ts" setup>
import { computed } from "vue";
import type { WidgetMood } from "../lib/widgetState";

/**
 * The widget's face: a 12x12 pixel cat whose eyes, mouth and colour are driven
 * by mood.
 *
 * Drawn as inline SVG rects with shape-rendering="crispEdges" rather than as
 * emoji or a bitmap: red line #13 requires inline SVG for UI glyphs (platform
 * emoji fonts render inconsistently and are missing outright on some Linux
 * setups), and vector rects stay sharp at any DPI where a PNG would not.
 *
 * Eye and mouth pixels are punched out in the card's background colour, so the
 * sprite is only correct on top of the widget card — which is the only place
 * it is used.
 */
const props = withDefaults(
  defineProps<{ mood: WidgetMood; size?: number; muted?: boolean }>(),
  // A multiple of 12 keeps every source pixel an exact integer of screen
  // pixels. 36 = 3px per pixel; 40 would give 3.33 and render some columns a
  // pixel wider than others — exactly the artefact you notice on pixel art.
  { size: 36, muted: false },
);

/** Card background — eyes and mouth are cut out of the body in this colour. */
const BG = "#0d1117";

const COLOR: Record<WidgetMood, string> = {
  idle: "#57606a",
  running: "#2f81f7",
  waiting: "#d29922",
  failed: "#f85149",
};

const fill = computed(() => COLOR[props.mood]);

// Motion is how the widget catches your eye, so it is tied to mood — but
// muting kills it. A muted widget still shows colour and counts; muting
// suppresses movement, not information.
const animClass = computed(() => (props.muted ? "" : `anim-${props.mood}`));
</script>

<template>
  <svg
    :class="animClass"
    :height="size"
    :width="size"
    aria-hidden="true"
    shape-rendering="crispEdges"
    viewBox="0 0 12 12"
  >
    <!-- ears -->
    <rect :fill="fill" height="2" width="2" x="2" y="1" />
    <rect :fill="fill" height="2" width="2" x="8" y="1" />
    <!-- head and body -->
    <rect :fill="fill" height="7" width="10" x="1" y="3" />
    <rect :fill="fill" height="1" width="8" x="2" y="10" />

    <!-- idle: eyes closed, content -->
    <template v-if="mood === 'idle'">
      <rect :fill="BG" height="1" width="2" x="3" y="6" />
      <rect :fill="BG" height="1" width="2" x="7" y="6" />
    </template>

    <!-- running: open eyes, watching the work -->
    <template v-else-if="mood === 'running'">
      <rect :fill="BG" height="2" width="1" x="3" y="5" />
      <rect :fill="BG" height="2" width="1" x="8" y="5" />
    </template>

    <!-- waiting: wide eyes and an open mouth — it wants something -->
    <template v-else-if="mood === 'waiting'">
      <rect :fill="BG" height="2" width="2" x="3" y="5" />
      <rect :fill="BG" height="2" width="2" x="7" y="5" />
      <rect :fill="BG" height="1" width="2" x="5" y="8" />
    </template>

    <!-- failed: x-ed out eyes, flat mouth -->
    <template v-else>
      <rect :fill="BG" height="1" width="1" x="3" y="5" />
      <rect :fill="BG" height="1" width="1" x="4" y="6" />
      <rect :fill="BG" height="1" width="1" x="3" y="7" />
      <rect :fill="BG" height="1" width="1" x="8" y="5" />
      <rect :fill="BG" height="1" width="1" x="7" y="6" />
      <rect :fill="BG" height="1" width="1" x="8" y="7" />
      <rect :fill="BG" height="1" width="4" x="4" y="9" />
    </template>
  </svg>
</template>

<style scoped>
svg {
  display: block;
}

/*
 * Pixel-art animation rules, all of which differ from what a smooth vector
 * sprite would use:
 *   - integer-pixel translations only. A fractional offset resamples the whole
 *     sprite and every edge goes soft.
 *   - steps() timing, so it reads as animation frames rather than a slide.
 *   - no scale() and no rotate(). Both resample the grid; a rotated pixel cat
 *     is a blurry cat.
 */
.anim-idle {
  animation: px-breathe 2.4s steps(1, end) infinite;
}
.anim-running {
  animation: px-bob 0.5s steps(1, end) infinite;
}
.anim-waiting {
  animation: px-jump 0.5s steps(1, end) infinite;
}
.anim-failed {
  animation: px-slump 2s steps(1, end) infinite;
}

@keyframes px-breathe {
  0%,
  60% {
    transform: translateY(0);
  }
  61%,
  100% {
    transform: translateY(1px);
  }
}
@keyframes px-bob {
  0%,
  49% {
    transform: translateY(0);
  }
  50%,
  100% {
    transform: translateY(-2px);
  }
}
@keyframes px-jump {
  0%,
  29% {
    transform: translateY(0);
  }
  30%,
  59% {
    transform: translateY(-4px);
  }
  60%,
  100% {
    transform: translateY(-2px);
  }
}
@keyframes px-slump {
  0%,
  50% {
    transform: translateY(0);
  }
  51%,
  100% {
    transform: translateY(1px);
  }
}

/* Respect the OS "reduce motion" setting — a perpetually hopping always-on-top
   window is exactly what that setting exists to stop. Colour and the face
   still convey state without any movement. */
@media (prefers-reduced-motion: reduce) {
  .anim-idle,
  .anim-running,
  .anim-waiting,
  .anim-failed {
    animation: none;
  }
}
</style>
