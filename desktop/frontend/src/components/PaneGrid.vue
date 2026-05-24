<script lang="ts" setup>
import { computed } from "vue";
import TerminalView from "./TerminalView.vue";
import type { Endpoint } from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { Pane, Tab } from "../lib/types";
import type { TerminalThemeDefinition } from "../lib/terminalThemes";
import { extractSessionLabel } from "../lib/terminalBell";

const props = defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  sessionInfoFor: (pane: Pane) => SessionInfo | null;
  viewerCountFor?: (sessionId: string) => number;
  active: boolean;
  terminalTheme: TerminalThemeDefinition["xtermTheme"];
  commandNotifyThresholdSec: number;
}>();

const emit = defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane", paneIdx: number): void;
  (e: "toast", message: string): void;
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

function formatWho(info: SessionInfo | null): string {
  if (!info) return "";
  const u = info.user || "";
  const h = info.host || "";
  if (u && h) return `${u}@${h}`;
  return h || u || "";
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
      <div class="term-host">
        <TerminalView
          v-if="pane.sessionId && endpointFor(pane)"
          :endpoint="endpointFor(pane)!"
          :session-id="pane.sessionId"
          :active="active"
          :focused="active && idx === tab.activePaneIdx"
          :expected-cols="sessionInfoFor(pane)?.cols"
          :expected-rows="sessionInfoFor(pane)?.rows"
          :remote-permission="sessionInfoFor(pane)?.remote_permission"
          :session-label="extractSessionLabel(sessionInfoFor(pane))"
          :avoid-top-right-badge="pane.remote || (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          :theme="terminalTheme"
          :is-local-session="!pane.remote"
          :command-notify-threshold-sec="commandNotifyThresholdSec"
          @toast="emit('toast', $event)"
        />
        <div v-else class="empty">[empty pane — press ⌘N / Ctrl+N to fill]</div>
      </div>

      <div class="cell-controls">
        <div
          v-if="pane.sessionId && !pane.remote && (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          class="viewers-badge"
          :title="`${viewerCountFor!(pane.sessionId)} remote viewer(s) watching`"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <span>{{ viewerCountFor!(pane.sessionId) }}</span>
        </div>

        <div
          v-if="pane.sessionId && pane.remote"
          class="remote-badge"
          :title="
            (sessionInfoFor(pane)?.host_id
              ? 'host_id ' + sessionInfoFor(pane)!.host_id + '\n'
              : '') + 'session ' + pane.sessionId
          "
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="11" height="11"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.4"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M2 16.1A5 5 0 0 1 5.9 20" />
            <path d="M2 12.05A9 9 0 0 1 9.95 20" />
            <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
            <line x1="2" y1="20" x2="2.01" y2="20" />
          </svg>
          <span v-if="formatWho(sessionInfoFor(pane))" class="who">
            {{ formatWho(sessionInfoFor(pane)) }}
          </span>
          <span v-else class="who dim">remote</span>
          <span class="sid">{{ pane.sessionId.slice(0, 8) }}</span>
        </div>

        <button
          v-if="tab.layout !== 'single'"
          class="close-pane"
          title="close pane (⌘W / Ctrl+W)"
          @click.stop="emit('close-pane', idx)"
        >×</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pane-grid {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 2px;
  background: var(--terminal-grid);
}
.pane-grid.single     { grid-template: "a"; }
.pane-grid.vertical   { grid-template: "a b" / 1fr 1fr; }
.pane-grid.horizontal { grid-template: "a" 1fr "b" 1fr; }
.pane-grid.grid2x2    { grid-template: "a b" 1fr "c d" 1fr / 1fr 1fr; }

.cell {
  position: relative;
  background: var(--terminal-bg);
  overflow: hidden;
}
.term-host {
  position: absolute;
  inset: 0;
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
.cell-controls {
  position: absolute;
  top: 6px;
  right: 8px;
  display: flex;
  align-items: center;
  gap: 4px;
  /* badge + close button float over the terminal; clicks on empty space
     between them should pass through to xterm. Each child opts back in. */
  pointer-events: none;
}
.remote-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: rgba(13, 17, 23, 0.85);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 2px 8px;
  font-size: 11px;
  line-height: 1.5;
  color: #d29922;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  user-select: none;
  /* badge is informational only; let clicks fall through to terminal */
  pointer-events: none;
}
.remote-badge svg { display: block; }
.viewers-badge {
  position: absolute;
  top: 4px;
  right: 4px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.55);
  color: var(--fg);
  font-size: 11px;
  pointer-events: none;
}
.viewers-badge svg { display: block; }
.remote-badge .who { font-weight: 600; }
.remote-badge .who.dim { color: var(--fg-dim); font-weight: 400; }
.remote-badge .sid {
  color: var(--fg-dim);
  font-weight: 400;
}
.remote-badge .sid::before {
  content: "·";
  margin-right: 4px;
}
.close-pane {
  border: none;
  background: rgba(13, 17, 23, 0.7);
  color: var(--fg-dim);
  font-size: 14px;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
  cursor: pointer;
  opacity: 0;
  pointer-events: auto;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.cell:hover .close-pane { opacity: 1; }
.close-pane:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
