<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import WidgetSprite from "./WidgetSprite.vue";
import { onWidgetBootstrap, onWidgetState, widgetBridge } from "./bridge";
import type { WidgetMood, WidgetState } from "../lib/widgetState";

/**
 * The companion window (layout A): widget + summary header, then one row per
 * session. Collapsing hides the rows but keeps the header, so a folded widget
 * still answers "does anything need me?".
 *
 * This component owns no data. Snapshots arrive from the main app over a pipe
 * (see desktop/widget_process.go); user intent goes back the same way.
 */

const EMPTY: WidgetState = {
  mood: "idle",
  waitingCount: 0,
  runningCount: 0,
  failedCount: 0,
  completedCount: 0,
  idleCount: 0,
  headline: "连接中…",
  subline: "",
  rows: [],
  overflowCount: 0,
  aiOnly: false,
};

/** Measured to size the OS window; see widgetBridge.resize. */
const cardEl = ref<HTMLElement | null>(null);
let cardObserver: ResizeObserver | null = null;

const state = ref<WidgetState>(EMPTY);
const collapsed = ref(false);
const peeking = ref(false);
const menuOpen = ref(false);
const mutedUntil = ref(0);

/** Auto-peek timer id, so a newer attention event replaces the older one. */
let autoPeekTimer: number | null = null;
/** Ticks once a second so running durations count up between pushes. */
let clockTimer: number | null = null;
const nowMs = ref(Date.now());

const muted = computed(() => mutedUntil.value * 1000 > nowMs.value);

/** Rows are visible when expanded, or while a hover/auto peek is open. */
const showRows = computed(() => !collapsed.value || peeking.value);

const DOT: Record<WidgetMood, string> = {
  idle: "#3fb950",
  running: "#2f81f7",
  waiting: "#d29922",
  failed: "#f85149",
};

function dotColor(m: WidgetMood): string {
  // An idle row in this list is a finished command, so green reads as "done"
  // rather than the grey the sprite uses for "nothing happening at all".
  return DOT[m];
}

function formatAge(ms: number): string {
  if (ms <= 0) return "";
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${String(s % 60).padStart(2, "0")}s`;
  return `${Math.floor(m / 60)}h${String(m % 60).padStart(2, "0")}m`;
}

/**
 * liveAge advances the pushed ageMs with a local clock so the duration ticks
 * smoothly. Re-pushing every second purely to animate a counter would burn IPC
 * for something the window can compute itself.
 */
function liveAge(row: { ageMs: number }, pushedAt: number): string {
  if (row.ageMs <= 0) return "";
  return formatAge(row.ageMs + (nowMs.value - pushedAt));
}

const pushedAt = ref(Date.now());

function applyState(next: WidgetState) {
  const prev = state.value;
  state.value = next;
  pushedAt.value = Date.now();

  // Something newly needs the user. If the widget is folded away, open it briefly
  // so a collapsed widget can still raise its hand — then fold back so it does not
  // silently become a permanent panel.
  const escalated =
    next.waitingCount > prev.waitingCount || next.failedCount > prev.failedCount;
  if (escalated && collapsed.value && !muted.value) {
    autoPeek();
  }
}

function autoPeek() {
  peeking.value = true;
  if (autoPeekTimer !== null) window.clearTimeout(autoPeekTimer);
  autoPeekTimer = window.setTimeout(() => {
    peeking.value = false;
    autoPeekTimer = null;
  }, 3000);
}

function toggleCollapsed() {
  // A manual toggle wins over an in-flight auto-peek.
  if (autoPeekTimer !== null) {
    window.clearTimeout(autoPeekTimer);
    autoPeekTimer = null;
  }
  peeking.value = false;
  collapsed.value = !collapsed.value;
  widgetBridge.setCollapsed(collapsed.value);
}

// Peek tracks the pointer over the WHOLE card, not just the header: opening
// the list on header-hover and closing it on header-leave meant moving the
// pointer down onto a session row instantly collapsed the list out from under
// it, so the rows could never actually be clicked.
function onPeekEnter() {
  if (!collapsed.value || autoPeekTimer !== null) return;
  peeking.value = true;
}

function onPeekLeave() {
  // An auto-peek owns the window until its timer fires; a stray mouse-out
  // must not cut the 3s attention window short.
  if (!collapsed.value || autoPeekTimer !== null) return;
  peeking.value = false;
}

function activate(sessionId: string) {
  widgetBridge.activate(sessionId);
}

function muteFor(minutes: number) {
  const until = Math.floor(Date.now() / 1000) + minutes * 60;
  mutedUntil.value = until;
  widgetBridge.mute(until);
  menuOpen.value = false;
}

function unmute() {
  mutedUntil.value = 0;
  widgetBridge.mute(0);
  menuOpen.value = false;
}

function toggleAiOnly() {
  menuOpen.value = false;
  // Optimistic only in the menu's checkmark sense — the authoritative value
  // comes back on the next pushed snapshot, since the filter is applied by
  // the projection in the main app.
  widgetBridge.setAiOnly(!state.value.aiOnly);
}

function hideWidget() {
  menuOpen.value = false;
  widgetBridge.hide();
}

function openMenu(e: MouseEvent) {
  e.preventDefault();
  menuOpen.value = true;
}

function closeMenu() {
  menuOpen.value = false;
}

onMounted(() => {
  onWidgetBootstrap((boot) => {
    collapsed.value = boot.collapsed;
  });
  onWidgetState(applyState);

  // Drive the OS window height from the rendered card. A ResizeObserver
  // covers collapse, expand, peek and row-count changes with one mechanism,
  // so neither side has to enumerate those states.
  if (cardEl.value) {
    cardObserver = new ResizeObserver((entries) => {
      const h = Math.ceil(entries[0].borderBoxSize?.[0]?.blockSize ?? entries[0].contentRect.height);
      if (h > 0) widgetBridge.resize(h);
    });
    cardObserver.observe(cardEl.value);
  }

  clockTimer = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 1000);

  // Wails reports no drag-end event, so persist the position when the pointer
  // is released anywhere in the window — that is where a header drag ends.
  window.addEventListener("mouseup", widgetBridge.reportPosition);
  window.addEventListener("click", closeMenu);

  // Last: everything above must be listening before Go replays the parked
  // bootstrap and first snapshot.
  widgetBridge.ready();
});

onUnmounted(() => {
  cardObserver?.disconnect();
  if (clockTimer !== null) window.clearInterval(clockTimer);
  if (autoPeekTimer !== null) window.clearTimeout(autoPeekTimer);
  window.removeEventListener("mouseup", widgetBridge.reportPosition);
  window.removeEventListener("click", closeMenu);
});
</script>

<template>
  <div
    ref="cardEl"
    class="widget-window"
    @contextmenu="openMenu"
    @mouseenter="onPeekEnter"
    @mouseleave="onPeekLeave"
  >
    <header class="widget-header" @click="toggleCollapsed">
      <div class="sprite-wrap">
        <WidgetSprite :mood="state.mood" :muted="muted" :size="40" />
        <span v-if="state.waitingCount > 0" class="badge">{{ state.waitingCount }}</span>
      </div>
      <div class="meta">
        <div class="headline">{{ state.headline }}</div>
        <div v-if="state.subline" class="subline">{{ state.subline }}</div>
      </div>
      <span v-if="muted" class="muted-flag" title="已静音">zZ</span>
    </header>

    <div v-if="showRows" class="rows">
      <div class="divider" />
      <button
        v-for="row in state.rows"
        :key="row.sessionId"
        class="row"
        :class="{ done: row.state === 'idle' }"
        type="button"
        @click.stop="activate(row.sessionId)"
      >
        <span :style="{ background: dotColor(row.state) }" class="dot" />
        <span class="row-text">
          <span class="row-title">
            <span v-if="row.remoteHost" class="host">{{ row.remoteHost }} ·</span>
            {{ row.title }}
          </span>
          <span v-if="row.subtitle" class="row-sub">{{ row.subtitle }}</span>
        </span>
        <span v-if="row.kind" class="kind">{{ row.kind }}</span>
        <span v-else-if="liveAge(row, pushedAt)" class="age">{{ liveAge(row, pushedAt) }}</span>
      </button>

      <div v-if="state.rows.length === 0" class="empty">没有活跃会话</div>
      <div v-if="state.overflowCount > 0" class="overflow">还有 {{ state.overflowCount }} 个…</div>
    </div>

    <div v-if="menuOpen" class="menu" @click.stop>
      <button type="button" @click="toggleCollapsed(); closeMenu()">
        {{ collapsed ? "展开" : "折叠" }}
      </button>
      <template v-if="muted">
        <button type="button" @click="unmute">取消静音</button>
      </template>
      <template v-else>
        <button type="button" @click="muteFor(15)">静音 15 分钟</button>
        <button type="button" @click="muteFor(60)">静音 1 小时</button>
      </template>
      <div class="menu-divider" />
      <button type="button" @click="toggleAiOnly">
        {{ state.aiOnly ? "✓ 仅 AI 会话" : "仅 AI 会话" }}
      </button>
      <div class="menu-divider" />
      <button class="danger" type="button" @click="hideWidget">隐藏挂件</button>
    </div>
  </div>
</template>

<style scoped>
.widget-window {
  /* The OS window is transparent; this card is the only thing that paints.
     No box-shadow here: the window is non-opaque, so each platform derives a
     drop shadow from this card's alpha. A CSS shadow would both double that
     up and get clipped at the window edge. */
  position: relative;
  width: 252px;
  border-radius: 12px;
  background: rgba(13, 17, 23, 0.94);
  border: 1px solid #30363d;
  overflow: hidden;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  user-select: none;
}

.widget-header {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 9px 11px;
  cursor: pointer;
  /* Wails frameless drag (same mechanism as TitleBar.vue). Rows opt out below
     so clicking a session never drags the window instead of activating it. */
  --wails-draggable: drag;
}

.sprite-wrap {
  position: relative;
  flex: 0 0 auto;
}

.badge {
  position: absolute;
  top: -3px;
  right: -4px;
  min-width: 14px;
  height: 14px;
  padding: 0 3px;
  border-radius: 7px;
  background: #d29922;
  color: #0d1117;
  font-size: 9px;
  font-weight: 800;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1.5px solid #0d1117;
}

.meta {
  min-width: 0;
  flex: 1;
}

.headline {
  font-size: 11px;
  color: #e6edf3;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.subline {
  margin-top: 2px;
  font-size: 10px;
  color: #8b949e;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.muted-flag {
  font-size: 10px;
  color: #6e7681;
  flex: 0 0 auto;
}

.divider {
  height: 1px;
  background: #21262d;
}

.row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 11px;
  background: none;
  border: 0;
  text-align: left;
  cursor: pointer;
  font: inherit;
  --wails-draggable: no-drag;
}

.row:hover {
  background: rgba(110, 118, 129, 0.14);
}

.row.done {
  opacity: 0.6;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: 0 0 auto;
}

.row-text {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.row-title {
  font-size: 10.5px;
  color: #e6edf3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.host {
  color: #7d8590;
}

.row-sub {
  font-size: 9.5px;
  color: #7d8590;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kind {
  flex: 0 0 auto;
  font-size: 8.5px;
  padding: 1px 4px;
  border-radius: 3px;
  border: 1px solid #30363d;
  color: #8b949e;
}

.age {
  flex: 0 0 auto;
  font-size: 9px;
  color: #6e7681;
  font-variant-numeric: tabular-nums;
}

.empty,
.overflow {
  padding: 8px 11px;
  font-size: 10px;
  color: #6e7681;
}

.menu {
  position: absolute;
  right: 8px;
  top: 46px;
  z-index: 10;
  min-width: 128px;
  padding: 4px;
  border-radius: 8px;
  background: #161b22;
  border: 1px solid #30363d;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.6);
  --wails-draggable: no-drag;
}

.menu button {
  display: block;
  width: 100%;
  padding: 6px 9px;
  border: 0;
  border-radius: 5px;
  background: none;
  color: #e6edf3;
  font: inherit;
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}

.menu button:hover {
  background: rgba(110, 118, 129, 0.18);
}

.menu button.danger {
  color: #f85149;
}

.menu-divider {
  height: 1px;
  margin: 4px 2px;
  background: #21262d;
}
</style>
