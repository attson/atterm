<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, ref, watch, type CSSProperties } from "vue";
import { Palette } from "lucide-vue-next";
import TerminalView from "./TerminalView.vue";
import PaneSplitter from "./PaneSplitter.vue";
import SelectDropdown, { type SelectOption } from "./SelectDropdown.vue";
import type { Endpoint } from "../lib/api";
import type { SessionInfo } from "../lib/connection";
import type { Pane, Tab, TerminalAppearance } from "../lib/types";
import {
  TERMINAL_THEMES,
  terminalThemePreview,
  type TerminalThemeDefinition,
} from "../lib/terminalThemes";
import { extractSessionLabel } from "../lib/terminalBell";
import { resolveSSHAppearance, resolveSSHTheme } from "../lib/sshAppearance";
import { useServicePreviewActive } from "../composables/useServicePreviewActive";
import { useI18n } from "../i18n/useI18n";
import { RATIO_DEFAULT, clampRatio } from "../lib/layout";
import {
  SESSION_DND_MIME,
  carriesSessionDrag,
  clearDraggingSession,
  draggingSession,
  setDraggingSession,
} from "../lib/paneDrop";

const props = defineProps<{
  tab: Tab;
  endpointFor: (pane: Pane) => Endpoint | null;
  directEndpoint?: Endpoint | null;
  preferDirect?: boolean;
  sessionInfoFor: (pane: Pane) => SessionInfo | null;
  viewerCountFor?: (sessionId: string) => number;
  active: boolean;
  terminalThemeId?: string;
  terminalTheme: TerminalThemeDefinition["xtermTheme"];
  commandNotifyThresholdSec: number;
  searchRequestSeq?: number;
  appearance?: TerminalAppearance;
  servicePreviewAvailable?: boolean;
}>();

const emit = defineEmits<{
  (e: "set-active-pane", paneIdx: number): void;
  (e: "close-pane", paneIdx: number): void;
  (e: "drop-session", payload: { paneIdx: number; sessionId: string }): void;
  (e: "detach-session", sessionId: string): void;
  (e: "toast", message: string): void;
  (e: "terminal-theme-changed", themeID: string): void;
  (e: "update:col-ratio", ratio: number): void;
  (e: "update:row-ratio", ratio: number): void;
}>();

const { t } = useI18n();
const servicePreviewActive = useServicePreviewActive();

// `.cell-controls` is a right-anchored, width:auto (shrink-to-fit) absolute box.
// When the preview controls teleport into the badge, the WebKit build Wails
// ships does not recompute that shrink-to-fit width — the pill stays collapsed
// (~18px) until a real window resize forces a full relayout. Toggling display
// off/on flushes the cached width so it sizes correctly the moment a preview
// opens or closes, without the user having to resize the window.
watch(servicePreviewActive, async () => {
  await nextTick();
  gridRoot.value?.querySelectorAll<HTMLElement>(".cell-controls").forEach((el) => {
    const prev = el.style.display;
    el.style.display = "none";
    void el.offsetHeight;
    el.style.display = prev;
  });
});

const AREA_FOR_LAYOUT = {
  single:     ["a"],
  vertical:   ["a", "b"],
  horizontal: ["a", "b"],
  grid2x2:    ["a", "b", "c", "d"],
} as const;

const areaFor = computed(() => AREA_FOR_LAYOUT[props.tab.layout]);

const gridRoot = ref<HTMLDivElement | null>(null);
const dragging = ref(false);

// Index of the pane a session is currently hovering over, or null. Only set
// for drags that carry SESSION_DND_MIME: anything else (a file from the OS,
// selected text) is left alone so the pane never swallows a drop it has no
// meaning for, and so a future file-drop feature can claim those events.
const dropTargetIdx = ref<number | null>(null);

function onPaneDragOver(e: DragEvent, idx: number) {
  if (!carriesSessionDrag(e.dataTransfer?.types)) return;
  // preventDefault is what marks this a valid drop target, and it has to happen
  // on EVERY dragover — the browser treats one un-prevented event as "this
  // element stopped accepting the drop" and then never fires drop at all.
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
  dropTargetIdx.value = idx;
}

function onPaneDragLeave(e: DragEvent, idx: number) {
  // Moving between a cell's own children fires dragleave on the cell; ignore
  // those so the highlight does not flicker as the pointer crosses the
  // terminal, the badges, and back.
  const to = e.relatedTarget as Node | null;
  if (to && e.currentTarget instanceof Node && e.currentTarget.contains(to)) return;
  if (dropTargetIdx.value === idx) dropTargetIdx.value = null;
}

function onPaneDrop(e: DragEvent, idx: number) {
  dropTargetIdx.value = null;
  if (!carriesSessionDrag(e.dataTransfer?.types)) return;
  e.preventDefault();
  // draggingSession() first: WebKit hands back an empty string here for a
  // custom MIME type, which silently swallowed every drop.
  const sessionId = draggingSession() || (e.dataTransfer?.getData(SESSION_DND_MIME) ?? "");
  if (!sessionId) return;
  emit("drop-session", { paneIdx: idx, sessionId });
}

// Same payload as a sidebar row, so a pane dropped on another pane is simply a
// move — the existing drop path handles it — while the tab bar reads it as a
// detach.
function onGripDragStart(e: DragEvent, pane: Pane) {
  if (!pane.sessionId) return;
  setDraggingSession(pane.sessionId);
  if (!e.dataTransfer) return;
  e.dataTransfer.setData(SESSION_DND_MIME, pane.sessionId);
  e.dataTransfer.effectAllowed = "move";
}

function onGripDragEnd() {
  clearDraggingSession();
}

function getContainerRect(): DOMRect | null {
  return gridRoot.value?.getBoundingClientRect() ?? null;
}

const gridStyle = computed<CSSProperties>(() => {
  const c = clampRatio(props.tab.colRatio);
  const r = clampRatio(props.tab.rowRatio);
  const cl = c.toFixed(4);
  const cr = (1 - c).toFixed(4);
  const rt = r.toFixed(4);
  const rb = (1 - r).toFixed(4);
  switch (props.tab.layout) {
    case "single":
      return {};
    case "vertical":
      return { gridTemplate: `"a b" / ${cl}fr ${cr}fr` };
    case "horizontal":
      return { gridTemplate: `"a" ${rt}fr "b" ${rb}fr / 1fr` };
    case "grid2x2":
      return { gridTemplate: `"a b" ${rt}fr "c d" ${rb}fr / ${cl}fr ${cr}fr` };
  }
  return {};
});

const showColSplitter = computed(
  () => props.tab.layout === "vertical" || props.tab.layout === "grid2x2",
);
const showRowSplitter = computed(
  () => props.tab.layout === "horizontal" || props.tab.layout === "grid2x2",
);

function onColUpdate(next: number) {
  emit("update:col-ratio", next);
}
function onRowUpdate(next: number) {
  emit("update:row-ratio", next);
}
function onColReset() {
  emit("update:col-ratio", RATIO_DEFAULT);
}
function onRowReset() {
  emit("update:row-ratio", RATIO_DEFAULT);
}
function onSplitterPointerDown() {
  dragging.value = true;
}
function onSplitterCommit() {
  dragging.value = false;
}

function onPaneClick(idx: number) {
  if (idx !== props.tab.activePaneIdx) emit("set-active-pane", idx);
}

function isPaneFocused(idx: number): boolean {
  return props.active && idx === props.tab.activePaneIdx;
}

function paneKey(pane: Pane, idx: number): string {
  return pane.sessionId ?? `empty-${idx}`;
}

const defaultAppearance: TerminalAppearance = {
  fontHead: "", fontSize: 13, lineHeight: 1, cursorStyle: "block", cursorBlink: true, scrollback: 5000,
};

function appearanceFor(pane: Pane): TerminalAppearance {
  return resolveSSHAppearance(props.appearance ?? defaultAppearance, sessionInfoForPane(pane)?.ssh_appearance);
}

function themeFor(pane: Pane): TerminalThemeDefinition["xtermTheme"] {
  return resolveSSHTheme(props.terminalTheme, sessionInfoForPane(pane)?.ssh_appearance);
}

const themeMenuSessionId = ref("");

function themeOptionsFor(): SelectOption[] {
  return TERMINAL_THEMES.map((theme) => ({
    value: theme.id,
    label: t(theme.labelKey),
    description: t(theme.descriptionKey),
    preview: terminalThemePreview(theme),
  }));
}

function toggleThemeMenu(sessionId: string): void {
  themeMenuSessionId.value = themeMenuSessionId.value === sessionId ? "" : sessionId;
}

function setGlobalTheme(value: string): void {
  if (TERMINAL_THEMES.some((theme) => theme.id === value)) {
    emit("terminal-theme-changed", value);
  }
  themeMenuSessionId.value = "";
}

function onThemeMenuDocumentMousedown(e: MouseEvent): void {
  if (!themeMenuSessionId.value) return;
  const picker = e.target instanceof Element ? e.target.closest(".pane-theme-picker") : null;
  if (picker?.getAttribute("data-theme-picker-session") === themeMenuSessionId.value) return;
  themeMenuSessionId.value = "";
}

watch(themeMenuSessionId, (sessionId, previous) => {
  if (sessionId && !previous) document.addEventListener("mousedown", onThemeMenuDocumentMousedown);
  if (!sessionId && previous) document.removeEventListener("mousedown", onThemeMenuDocumentMousedown);
});

onBeforeUnmount(() => {
  document.removeEventListener("mousedown", onThemeMenuDocumentMousedown);
});

function sessionInfoForPane(pane: Pane): SessionInfo | null {
  return props.sessionInfoFor(pane);
}

function formatWho(info: SessionInfo | null): string {
  if (!info) return "";
  const u = info.user || "";
  const h = info.host || "";
  if (u && h) return `${u}@${h}`;
  return h || u || "";
}

function requestServicePreview(pane: Pane): void {
  if (!pane.sessionId) return;
  window.dispatchEvent(new CustomEvent("atterm:open-service-preview", {
    detail: { sessionId: pane.sessionId },
  }));
}
</script>

<template>
  <div
    ref="gridRoot"
    class="pane-grid"
    :class="tab.layout"
    :style="gridStyle"
  >
    <div
      v-for="(pane, idx) in tab.panes"
      :key="paneKey(pane, idx)"
      class="cell"
      :class="{ 'drop-target': dropTargetIdx === idx }"
      :style="{ gridArea: areaFor[idx] }"
      data-test="pane-cell"
      @mousedown="onPaneClick(idx)"
      @dragover="onPaneDragOver($event, idx)"
      @dragleave="onPaneDragLeave($event, idx)"
      @drop="onPaneDrop($event, idx)"
    >
      <div class="term-host">
        <TerminalView
          v-if="pane.sessionId && (endpointFor(pane) || pane.peerDirect)"
          :endpoint="endpointFor(pane)"
          :direct-endpoint="pane.remote ? directEndpoint : null"
          :prefer-direct="pane.remote && preferDirect"
          :session-id="pane.sessionId"
          :active="active"
          :focused="isPaneFocused(idx)"
          :expected-cols="sessionInfoFor(pane)?.cols"
          :expected-rows="sessionInfoFor(pane)?.rows"
          :remote-permission="sessionInfoFor(pane)?.remote_permission"
          :session-label="extractSessionLabel(sessionInfoFor(pane))"
          :session-type="sessionInfoFor(pane)?.type"
          :current-command="sessionInfoFor(pane)?.current_command"
          :current-cwd="sessionInfoFor(pane)?.cwd"
          :avoid-top-right-badge="pane.remote || (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          :theme="themeFor(pane)"
          :is-local-session="!pane.remote"
          :peer-direct="pane.peerDirect === true"
          :command-notify-threshold-sec="commandNotifyThresholdSec"
          :resize-suspended="dragging"
          :search-request-seq="searchRequestSeq"
          :can-detach="tab.layout !== 'single'"
          :appearance="appearanceFor(pane)"
          @toast="emit('toast', $event)"
          @detach="pane.sessionId && emit('detach-session', pane.sessionId)"
        />
        <div v-else class="empty">{{ t("terminal.emptyPaneHint") }}</div>
      </div>

      <div class="cell-controls">
        <span
          v-if="pane.sessionId && isPaneFocused(idx)"
          class="session-input-history-controls-slot"
          :id="`session-input-history-controls-${pane.sessionId}`"
        ></span>

        <div
          v-if="pane.sessionId"
          class="pane-theme-picker"
          :data-theme-picker-session="pane.sessionId"
        >
          <button
            type="button"
            class="pane-theme-trigger"
            :class="{ active: themeMenuSessionId === pane.sessionId }"
            :data-test="`terminal-theme-trigger-${pane.sessionId}`"
            v-tooltip="t('terminal.theme.open')"
            :aria-label="t('terminal.theme.open')"
            :aria-expanded="themeMenuSessionId === pane.sessionId"
            @mousedown.stop
            @click.stop="toggleThemeMenu(pane.sessionId)"
          >
            <Palette :size="15" aria-hidden="true" />
          </button>
          <div
            v-if="themeMenuSessionId === pane.sessionId"
            class="pane-theme-menu"
            :data-test="`terminal-theme-menu-${pane.sessionId}`"
            @mousedown.stop
            @click.stop
          >
            <span class="pane-theme-menu-title">{{ t("terminal.theme.currentPane") }}</span>
            <SelectDropdown
              :data-test="`terminal-theme-select-${pane.sessionId}`"
              :model-value="terminalThemeId || 'classic'"
              :options="themeOptionsFor()"
              :aria-label="t('terminal.theme.currentPane')"
              @update:model-value="setGlobalTheme($event)"
            />
          </div>
        </div>

        <div
          v-if="pane.sessionId && !pane.remote && (viewerCountFor?.(pane.sessionId) ?? 0) > 0"
          class="viewers-badge"
          v-tooltip="t('terminal.remoteViewerWatching', { count: viewerCountFor!(pane.sessionId) })"
        >
          <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7Z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <span>{{ viewerCountFor!(pane.sessionId) }}</span>
        </div>

        <!-- Preview controls live in their OWN block to the left of the host
             badge so opening a preview never replaces the remote-terminal badge.
             TerminalView teleports its switcher / add-form into this slot. -->
        <span
          v-if="pane.sessionId && pane.remote && servicePreviewAvailable"
          class="service-preview-controls-slot"
          :id="`service-preview-controls-${pane.sessionId}`"
          aria-live="polite"
        ></span>

        <div
          v-if="pane.sessionId && pane.remote"
          class="remote-badge"
          v-tooltip="
            (sessionInfoFor(pane)?.host_id
              ? t('sessions.hostIdTitle', { hostId: sessionInfoFor(pane)!.host_id ?? '' }) + '\n'
              : '') + t('sessions.sessionTitle', { sessionId: pane.sessionId })
          "
        >
          <button
            v-if="servicePreviewAvailable"
            type="button"
            class="remote-preview-trigger"
            data-testid="remote-preview-trigger"
            v-tooltip="t('terminal.preview.open')"
            :aria-label="t('terminal.preview.open')"
            @mousedown.stop
            @click.stop="requestServicePreview(pane)"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M2 16.1A5 5 0 0 1 5.9 20" />
              <path d="M2 12.05A9 9 0 0 1 9.95 20" />
              <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
              <line x1="2" y1="20" x2="2.01" y2="20" />
            </svg>
          </button>
          <span v-if="formatWho(sessionInfoFor(pane))" class="who">
            {{ formatWho(sessionInfoFor(pane)) }}
          </span>
          <span v-else class="who dim">{{ t("terminal.remote") }}</span>
          <span class="sid">{{ pane.sessionId.slice(0, 8) }}</span>
          <span
            class="route-indicator-slot"
            :id="`session-route-indicator-${pane.sessionId}`"
            aria-live="polite"
          ></span>
        </div>

        <!-- Drag handle. The terminal itself cannot be draggable — xterm needs
             the mouse for text selection — so the grip is the one place a pane
             can be picked up by. Only shown when there is somewhere to detach
             to: in a single-pane tab the session already owns its tab. -->
        <span
          v-if="pane.sessionId && tab.layout !== 'single' && isPaneFocused(idx)"
          class="pane-grip"
          data-test="pane-grip"
          draggable="true"
          v-tooltip="t('terminal.dragPaneOut')"
          @mousedown.stop
          @dragstart="onGripDragStart($event, pane)"
          @dragend="onGripDragEnd"
        >⠿</span>
        <button
          v-if="tab.layout !== 'single' && isPaneFocused(idx)"
          type="button"
          class="close-pane"
          v-tooltip="t('terminal.closePaneTitle')"
          :aria-label="t('terminal.closePaneTitle')"
          @mousedown.stop
          @click.stop="emit('close-pane', idx)"
        >×</button>
      </div>
    </div>

    <PaneSplitter
      v-if="showColSplitter"
      orientation="col"
      :ratio="tab.colRatio"
      :container-rect="getContainerRect"
      @pointerdown="onSplitterPointerDown"
      @update:ratio="onColUpdate"
      @commit="onSplitterCommit"
      @reset="onColReset"
    />
    <PaneSplitter
      v-if="showRowSplitter"
      orientation="row"
      :ratio="tab.rowRatio"
      :container-rect="getContainerRect"
      @pointerdown="onSplitterPointerDown"
      @update:ratio="onRowUpdate"
      @commit="onSplitterCommit"
      @reset="onRowReset"
    />
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
.pane-grid.single { grid-template: "a"; }
/* vertical / horizontal / grid2x2 templates set via :style from gridStyle */

.cell {
  position: relative;
  background: var(--terminal-bg);
  overflow: hidden;
}
/* Drop feedback for a session dragged out of the sidebar. Drawn with an inset
   shadow rather than a border so the terminal underneath keeps its exact
   geometry — a real border would resize the pane and make xterm reflow mid
   drag. ::after carries the tint so it sits above the terminal canvas without
   giving the cell a stacking context of its own. */
.cell.drop-target {
  box-shadow: inset 0 0 0 2px var(--accent);
}
.cell.drop-target::after {
  content: "";
  position: absolute;
  inset: 0;
  background: color-mix(in srgb, var(--accent) 12%, transparent);
  pointer-events: none;
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
  z-index: 6;
  /* badge + close button float over the terminal; clicks on empty space
     between them should pass through to xterm. Each child opts back in. */
  pointer-events: none;
}
.service-preview-controls-slot {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  pointer-events: auto;
}
.session-input-history-controls-slot {
  display: inline-flex;
  align-items: center;
  pointer-events: auto;
}
.pane-theme-picker {
  position: relative;
  display: inline-flex;
  pointer-events: auto;
}
.pane-theme-trigger {
  width: 26px; height: 26px; padding: 0;
  display: inline-flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); border-radius: 6px;
  color: var(--fg-dim); background: rgba(13, 17, 23, 0.88);
  cursor: pointer;
}
.pane-theme-trigger:hover,
.pane-theme-trigger:focus-visible,
.pane-theme-trigger.active {
  color: var(--fg); border-color: var(--accent); outline: none;
}
.pane-theme-menu {
  position: absolute; top: 32px; right: 0; z-index: 9;
  width: 310px; max-width: min(310px, calc(100vw - 32px)); padding: 10px;
  display: flex; flex-direction: column; gap: 7px;
  border: 1px solid var(--border); border-radius: 8px;
  background: var(--terminal-overlay); box-shadow: 0 8px 24px rgba(0, 0, 0, 0.32);
}
.pane-theme-menu-title {
  color: var(--fg); font-size: 11px; font-weight: 600;
}
.pane-theme-menu :deep(.trigger) { height: 28px; font-size: 12px; }
.pane-theme-menu :deep(.menu) { right: 0; left: auto; background: var(--terminal-overlay); }
.pane-theme-menu :deep(.menu-with-previews) { width: 100%; }
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
.remote-preview-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  padding: 0;
  border: 0;
  border-radius: 4px;
  color: inherit;
  background: transparent;
  cursor: pointer;
  pointer-events: auto;
}
.remote-preview-trigger:hover { color: var(--fg); background: rgba(255, 255, 255, 0.1); }
.remote-preview-trigger svg { display: block; }
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
.route-indicator-slot {
  display: contents;
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
  pointer-events: auto;
  transition: background 120ms, color 120ms;
}
.pane-grip {
  cursor: grab;
  /* .cell-controls is pointer-events:none so gaps between the badges fall
     through to xterm; every child has to opt back in or it is inert. */
  pointer-events: auto;
  /* WebKit will not start a drag on an arbitrary element from the draggable
     attribute alone — doubly so for one that is user-select:none. */
  -webkit-user-drag: element;
  padding: 0 3px;
  font-size: 12px;
  line-height: 1;
  color: var(--fg-dim);
  user-select: none;
}
.pane-grip:active { cursor: grabbing; }
@media (hover: none) {
  /* No hover to reveal it, and HTML5 drag never fires on touch anyway. */
  .pane-grip { display: none; }
}
.close-pane:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
