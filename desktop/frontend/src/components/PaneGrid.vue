<script lang="ts" setup>
import { computed } from "vue";
import TerminalView from "./TerminalView.vue";
import type { Endpoint } from "../lib/api";
import type { Pane, Tab } from "../lib/types";

const props = defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  active: boolean;
}>();

const emit = defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane", paneIdx: number): void;
}>();

const AREA_FOR_LAYOUT = {
  single:     ["a"],
  vertical:   ["a", "b"],
  horizontal: ["a", "b"],
  grid2x2:    ["a", "b", "c", "d"],
} as const;

const areaFor = computed(() => AREA_FOR_LAYOUT[props.tab.layout]);

function onPaneClick(idx: number) {
  if (idx !== props.tab.activePaneIdx) emit("set-active-pane", idx);
}
</script>

<template>
  <div class="pane-grid" :class="tab.layout">
    <div
      v-for="(pane, idx) in tab.panes"
      :key="idx"
      class="cell"
      :style="{ gridArea: areaFor[idx] }"
      @mousedown="onPaneClick(idx)"
    >
      <TerminalView
        v-if="pane.sessionId && endpointFor(pane)"
        :endpoint="endpointFor(pane)!"
        :session-id="pane.sessionId"
        :active="active"
        :focused="active && idx === tab.activePaneIdx"
      />
      <div v-else class="empty">[empty pane — press ⌘D / Ctrl+D to fill]</div>
      <button
        v-if="tab.layout !== 'single'"
        class="close-pane"
        title="close pane (⌘W / Ctrl+W)"
        @click.stop="emit('close-pane', idx)"
      >×</button>
    </div>
  </div>
</template>

<style scoped>
.pane-grid {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 2px;
  background: #11161d;
}
.pane-grid.single     { grid-template: "a"; }
.pane-grid.vertical   { grid-template: "a b" / 1fr 1fr; }
.pane-grid.horizontal { grid-template: "a" 1fr "b" 1fr; }
.pane-grid.grid2x2    { grid-template: "a b" 1fr "c d" 1fr / 1fr 1fr; }

.cell {
  position: relative;
  background: #000;
  overflow: hidden;
}
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-dim);
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.close-pane {
  position: absolute;
  top: 4px;
  right: 4px;
  border: none;
  background: rgba(13, 17, 23, 0.7);
  color: var(--fg-dim);
  font-size: 14px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.cell:hover .close-pane { opacity: 1; }
.close-pane:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
