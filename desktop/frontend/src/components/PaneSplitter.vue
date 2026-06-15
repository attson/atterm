<script lang="ts" setup>
import { computed, onBeforeUnmount, ref } from "vue";
import { clampRatio } from "../lib/layout";

const props = defineProps<{
  orientation: "col" | "row";
  ratio: number;
  containerRect: () => DOMRect | null;
}>();

const emit = defineEmits<{
  (e: "update:ratio", next: number): void;
  (e: "commit"): void;
  (e: "reset"): void;
}>();

const rootEl = ref<HTMLDivElement | null>(null);
let activePointerId: number | null = null;
let startCoord = 0;
let startRatio = 0;
let savedBodyCursor = "";

function onPointerDown(e: PointerEvent) {
  if (e.button !== 0) return;
  const el = rootEl.value;
  if (!el) return;
  activePointerId = e.pointerId;
  startRatio = props.ratio;
  startCoord = props.orientation === "col" ? e.clientX : e.clientY;
  try { el.setPointerCapture(e.pointerId); } catch { /* ignore */ }
  if (typeof document !== "undefined") {
    savedBodyCursor = document.body.style.cursor;
    document.body.style.cursor = props.orientation === "col" ? "col-resize" : "row-resize";
  }
  e.preventDefault();
}

function onPointerMove(e: PointerEvent) {
  if (activePointerId !== e.pointerId) return;
  const rect = props.containerRect();
  if (!rect) return;
  const size = props.orientation === "col" ? rect.width : rect.height;
  if (size <= 0) return;
  const current = props.orientation === "col" ? e.clientX : e.clientY;
  const delta = (current - startCoord) / size;
  emit("update:ratio", clampRatio(startRatio + delta));
}

function endDrag(e: PointerEvent) {
  if (activePointerId !== e.pointerId) return;
  const el = rootEl.value;
  try { el?.releasePointerCapture(e.pointerId); } catch { /* ignore */ }
  activePointerId = null;
  if (typeof document !== "undefined") {
    document.body.style.cursor = savedBodyCursor;
    savedBodyCursor = "";
  }
  emit("commit");
}

function onDblclick() {
  emit("reset");
}

onBeforeUnmount(() => {
  if (activePointerId !== null && rootEl.value) {
    try { rootEl.value.releasePointerCapture(activePointerId); } catch { /* ignore */ }
    if (typeof document !== "undefined") {
      document.body.style.cursor = savedBodyCursor;
    }
  }
});

const style = computed(() => {
  const pct = clampRatio(props.ratio) * 100;
  if (props.orientation === "col") {
    return { left: `calc(${pct}% - 3px)` };
  }
  return { top: `calc(${pct}% - 3px)` };
});

</script>

<template>
  <div
    ref="rootEl"
    class="pane-splitter"
    :class="orientation"
    :style="style"
    role="separator"
    :aria-orientation="orientation === 'col' ? 'vertical' : 'horizontal'"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="endDrag"
    @pointercancel="endDrag"
    @dblclick="onDblclick"
  />
</template>

<style scoped>
.pane-splitter {
  position: absolute;
  z-index: 5;
  background: transparent;
  user-select: none;
  touch-action: none;
}
.pane-splitter.col {
  top: 0;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
}
.pane-splitter.row {
  left: 0;
  right: 0;
  height: 6px;
  cursor: row-resize;
}
.pane-splitter:hover,
.pane-splitter:active {
  background: rgba(255, 255, 255, 0.08);
}
</style>
