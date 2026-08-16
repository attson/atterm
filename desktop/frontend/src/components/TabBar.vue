<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from "vue";
import { ShieldUser, Server } from "lucide-vue-next";
import type { SessionInfo } from "../lib/connection";
import type { Tab } from "../lib/types";
import { useI18n } from "../i18n/useI18n";
import TaskStateIcon from "./TaskStateIcon.vue";
import { stateColor, type TaskState } from "../lib/taskState";
import { usableAITitle } from "../lib/sessionLabel";
import { SESSION_DND_MIME, carriesSessionDrag, draggingSession } from "../lib/paneDrop";

interface TabSummary {
  id: string;
  layout: Tab["layout"];
  activeSession: SessionInfo | null;
  activeRemote: boolean;
  paneCount: number;
  disconnected?: boolean;
}

withDefaults(
  defineProps<{
    tabs: TabSummary[];
    currentId: string | null;
    starting: boolean;
    canNewLocal?: boolean;
    // Mirrors the desktop-update indicator that used to live on TitleBar's
    // settings button. Kept optional/default-false so web/mobile callers
    // that never surface an update state don't need to pass it.
    updateBadge?: boolean;
    // Whether the current user is an admin. Gates the admin button entirely
    // — non-admins never see it, regardless of adminOpen.
    isAdmin?: boolean;
    // Whether the admin panel is the currently active main-area view.
    adminOpen?: boolean;
  }>(),
  { canNewLocal: true, updateBadge: false, isAdmin: false, adminOpen: false },
);

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close", id: string): void;
  (e: "new"): void;
  (e: "new-ssh"): void;
  (e: "reorder", fromId: string, targetId: string, position: "before" | "after"): void;
  // Settings lives here (not TitleBar) so it's reachable regardless of
  // caps.windowControls — TitleBar (and its old settings button) is hidden
  // entirely on web/mobile, but TabBar always renders alongside App.vue.
  (e: "open-settings"): void;
  (e: "toggle-admin"): void;
  /** A session was dragged out of a pane and dropped here: give it its own tab. */
  (e: "detach-session", sessionId: string): void;
}>();

const DRAG_THRESHOLD = 4;
const EDGE_SCROLL_ZONE = 40;
const EDGE_SCROLL_SPEED = 8;

const drag = ref<null | {
  fromId: string;
  startX: number;
  active: boolean;
  pointerId: number;
  overId: string | null;
  position: "before" | "after" | null;
  hadDrag: boolean;
}>(null);
const tabsEl = ref<HTMLElement | null>(null);
let lastClientX = 0;
let rafId: number | null = null;

function isCloseTarget(e: PointerEvent): boolean {
  const t = e.target as HTMLElement | null;
  return !!t && !!t.closest?.(".close");
}

function onTabPointerDown(id: string, e: PointerEvent) {
  if (e.button !== 0) return;
  if (isCloseTarget(e)) return;
  drag.value = {
    fromId: id,
    startX: e.clientX,
    active: false,
    pointerId: e.pointerId,
    overId: null,
    position: null,
    hadDrag: false,
  };
}

function hitTest(clientX: number): { id: string; position: "before" | "after" } | null {
  if (!tabsEl.value) return null;
  const nodes = Array.from(tabsEl.value.querySelectorAll<HTMLElement>(".tab"));
  if (nodes.length === 0) return null;
  for (const el of nodes) {
    const r = el.getBoundingClientRect();
    if (clientX >= r.left && clientX <= r.right) {
      const mid = (r.left + r.right) / 2;
      return { id: el.dataset.tabId!, position: clientX < mid ? "before" : "after" };
    }
  }
  const firstR = nodes[0].getBoundingClientRect();
  if (clientX < firstR.left) return { id: nodes[0].dataset.tabId!, position: "before" };
  const lastR = nodes[nodes.length - 1].getBoundingClientRect();
  return { id: nodes[nodes.length - 1].dataset.tabId!, position: "after" };
}

function tickEdgeScroll() {
  const d = drag.value;
  const el = tabsEl.value;
  if (!d || !d.active || !el) { rafId = null; return; }
  const rect = el.getBoundingClientRect();
  if (lastClientX - rect.left < EDGE_SCROLL_ZONE) {
    el.scrollLeft = Math.max(0, el.scrollLeft - EDGE_SCROLL_SPEED);
  } else if (rect.right - lastClientX < EDGE_SCROLL_ZONE) {
    el.scrollLeft = el.scrollLeft + EDGE_SCROLL_SPEED;
  } else {
    rafId = null;
    return;
  }
  rafId = requestAnimationFrame(tickEdgeScroll);
}

function onWindowPointerMove(e: PointerEvent) {
  const d = drag.value;
  if (!d || e.pointerId !== d.pointerId) return;
  lastClientX = e.clientX;
  if (!d.active) {
    if (Math.abs(e.clientX - d.startX) < DRAG_THRESHOLD) return;
    d.active = true;
    d.hadDrag = true;
  }
  if (tabsEl.value) {
    const rect = tabsEl.value.getBoundingClientRect();
    const inLeft = e.clientX - rect.left < EDGE_SCROLL_ZONE;
    const inRight = rect.right - e.clientX < EDGE_SCROLL_ZONE;
    if ((inLeft || inRight) && rafId === null) {
      rafId = requestAnimationFrame(tickEdgeScroll);
    }
  }
  const hit = hitTest(e.clientX);
  if (!hit) return;
  if (hit.id === d.fromId) return;
  if (hit.id === d.overId && hit.position === d.position) return;
  d.overId = hit.id;
  d.position = hit.position;
  emit("reorder", d.fromId, hit.id, hit.position);
}

function suppressNextClick() {
  const el = tabsEl.value;
  if (!el) return;
  const stop = (ev: MouseEvent) => {
    ev.stopPropagation();
    ev.preventDefault();
    el.removeEventListener("click", stop, true);
  };
  el.addEventListener("click", stop, true);
  // If the trailing click never arrives (drag ended off-tab), clean up on
  // the next macrotask so a later, unrelated click on the tabbar still fires.
  setTimeout(() => el.removeEventListener("click", stop, true), 0);
}

function endDrag(cancelled: boolean) {
  const d = drag.value;
  if (!d) return;
  if (d.hadDrag && !cancelled) suppressNextClick();
  drag.value = null;
  if (rafId !== null) {
    cancelAnimationFrame(rafId);
    rafId = null;
  }
}

function onWindowPointerUp(e: PointerEvent) {
  if (!drag.value || e.pointerId !== drag.value.pointerId) return;
  endDrag(false);
}

function onWindowPointerCancel(e: PointerEvent) {
  if (!drag.value || e.pointerId !== drag.value.pointerId) return;
  endDrag(true);
}

onMounted(() => {
  window.addEventListener("pointermove", onWindowPointerMove);
  window.addEventListener("pointerup", onWindowPointerUp);
  window.addEventListener("pointercancel", onWindowPointerCancel);
});
onBeforeUnmount(() => {
  window.removeEventListener("pointermove", onWindowPointerMove);
  window.removeEventListener("pointerup", onWindowPointerUp);
  window.removeEventListener("pointercancel", onWindowPointerCancel);
  if (rafId !== null) cancelAnimationFrame(rafId);
});

const { t: i18nT } = useI18n();

function shortTitle(s: SessionInfo | null): string {
  if (!s) return i18nT("terminal.emptyTab");
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped === "") return "/";
    const base = stripped.split("/").pop();
    if (base) return base;
  }
  const first = (s.command || "").split(/\s+/)[0] || i18nT("terminal.shellFallback");
  return first.split("/").pop() || first;
}

// AI sessions surface the OSC 0/1/2 title their tool sets (claude, codex…)
// when one is available; everything else falls back to shortTitle so shell
// tabs keep their cwd-basename display.
/**
 * Same tint contract as the sidebar's rows (TaskGroupedList.unreadTint): the
 * tab wears its session's state colour while unread, so the two surfaces read
 * as one system instead of each inventing its own unread marker.
 */
function unreadTint(s: SessionInfo | null): Record<string, string> | undefined {
  if (s?.unread !== true) return undefined;
  return { "--unread-c": stateColor((s.task_state as TaskState | undefined) ?? "idle") };
}

function tabTitle(s: SessionInfo | null): string {
  if (s) {
    const title = usableAITitle({
      type: s.type,
      title: s.title,
      current_command: s.current_command,
      cwd: s.cwd,
    });
    if (title) return title;
  }
  return shortTitle(s);
}

function layoutLabel(t: TabSummary): string {
  switch (t.layout) {
    case "single": return "";
    case "vertical": return "▮▮";
    case "horizontal": return "▬\n▬";
    case "grid2x2": return "▦";
  }
}

function layoutTitle(t: TabSummary): string {
  switch (t.layout) {
    case "single": return "";
    case "vertical": return i18nT("terminal.layoutVertical");
    case "horizontal": return i18nT("terminal.layoutHorizontal");
    case "grid2x2": return i18nT("terminal.layoutGrid2x2");
  }
}

function onClose(e: MouseEvent, id: string) {
  e.stopPropagation();
  emit("close", id);
}

// Dropping a pane's grip here pulls that session out into a tab of its own.
// The strip only claims drags that carry our own session type; anything else —
// an OS file, selected text — passes through untouched.
const dropActive = ref(false);

function onStripDragOver(e: DragEvent) {
  if (!carriesSessionDrag(e.dataTransfer?.types)) return;
  // Must fire on EVERY dragover: one un-prevented event and the browser stops
  // treating the strip as a drop target, so drop never arrives.
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
  dropActive.value = true;
}

function onStripDragLeave(e: DragEvent) {
  const to = e.relatedTarget as Node | null;
  if (to && e.currentTarget instanceof Node && e.currentTarget.contains(to)) return;
  dropActive.value = false;
}

function onStripDrop(e: DragEvent) {
  dropActive.value = false;
  if (!carriesSessionDrag(e.dataTransfer?.types)) return;
  e.preventDefault();
  // draggingSession() first — WebKit returns nothing from getData for a custom
  // MIME type, which would make the drop a silent no-op.
  const sessionId = draggingSession() || (e.dataTransfer?.getData(SESSION_DND_MIME) ?? "");
  if (!sessionId) return;
  emit("detach-session", sessionId);
}

</script>

<template>
  <div
    class="tabbar"
    :class="{ 'drop-active': dropActive }"
    data-test="tabbar"
    @dragover="onStripDragOver"
    @dragleave="onStripDragLeave"
    @drop="onStripDrop"
  >
    <div class="tabs" ref="tabsEl">
      <div
        v-for="(t, idx) in tabs"
        :key="t.id"
        :data-tab-id="t.id"
        class="tab"
        :class="{
          active: t.id === currentId,
          unread: t.activeSession?.unread === true,
          remote: t.activeRemote,
          disconnected: t.disconnected,
          dragging: drag?.active && drag?.fromId === t.id,
        }"
        :style="unreadTint(t.activeSession)"
        :title="(t.activeRemote ? i18nT('terminal.remotePrefix') : '') + (t.disconnected ? i18nT('terminal.tabDisconnectedSuffix') + ' ' : '') + (t.activeSession?.command ?? '')"
        @click="emit('activate', t.id)"
        @pointerdown="onTabPointerDown(t.id, $event)"
      >
        <span class="num">{{ idx + 1 }}:</span>
        <span v-if="t.layout !== 'single'" class="layout-icon" :title="layoutTitle(t)">{{ layoutLabel(t) }}</span>
        <span
          v-else-if="t.activeRemote"
          class="dot remote-dot"
          :class="{ disconnected: t.disconnected }"
        >
          <TaskStateIcon
            :state="(t.activeSession?.task_state as TaskState | undefined) ?? 'idle'"
            :unread="t.activeSession?.unread === true"
            :size="10"
          />
        </span>
        <TaskStateIcon
          v-else
          :state="(t.activeSession?.task_state as TaskState | undefined) ?? 'idle'"
          :unread="t.activeSession?.unread === true"
          :size="10"
        />
        <span class="title">{{ tabTitle(t.activeSession) }}</span>
        <button class="close" @pointerdown.stop @click="onClose($event, t.id)">×</button>
      </div>
    </div>
    <button
      v-if="canNewLocal"
      class="plus"
      data-test="new-local-shell"
      :disabled="starting"
      :title="starting ? i18nT('terminal.starting') : i18nT('terminal.newTab')"
      @click="emit('new')"
    >+</button>
    <button
      v-if="canNewLocal"
      class="ssh-btn"
      type="button"
      data-test="new-ssh"
      title="SSH hosts"
      @click="emit('new-ssh')"
    >
      <Server :size="14" />
    </button>
    <button
      v-if="isAdmin"
      class="admin-btn"
      type="button"
      data-test="admin-button"
      :class="{ active: adminOpen }"
      :title="i18nT('terminal.adminPanel')"
      @click="emit('toggle-admin')"
    >
      <ShieldUser :size="14" />
    </button>
    <button
      class="settings-btn"
      type="button"
      data-test="tabbar-settings"
      :title="i18nT('terminal.relaySettings')"
      @click="emit('open-settings')"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="14" height="14"
        viewBox="0 0 24 24"
        fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
        aria-hidden="true"
      >
        <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
      <span v-if="updateBadge" class="dot"></span>
    </button>
  </div>
</template>

<style scoped>
/* Whole strip lights up while a pane is hovering over it: the drop is "give
   this session a tab", not "insert it between these two", so there is nothing
   finer to aim at. */
.tabbar.drop-active {
  box-shadow: inset 0 -2px 0 var(--accent);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
}
.tabbar {
  position: sticky;
  top: 0;
  z-index: 12;
  display: flex; align-items: stretch; background: var(--panel);
  flex: 0 0 auto; height: 28px;
  overflow: hidden;
}
.tabs {
  display: flex; flex: 1 1 auto; overflow-x: auto;
  scrollbar-width: none;
  -ms-overflow-style: none;
}
.tabs::-webkit-scrollbar { display: none; }

.tab {
  display: flex; align-items: center; gap: 6px; padding: 0 8px 0 12px;
  border-right: 1px solid var(--border); font-size: 12px;
  color: var(--fg-dim); cursor: pointer; user-select: none;
  white-space: nowrap; min-width: 110px; max-width: 220px;
  transition: background 120ms;
  touch-action: pan-y;
}
.tab.dragging { opacity: 0.5; }
.tab:hover { background: rgba(255, 255, 255, 0.04); }
.tab.active {
  background: var(--bg); color: var(--fg);
  box-shadow: inset 0 -2px 0 var(--accent);
}
.tab .num {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px; color: var(--fg-dim);
}
.tab.active .num { color: var(--accent); }
.tab .dot { font-size: 9px; color: var(--good); }
.tab .remote-dot { color: #d29922; }
.tab .remote-dot.disconnected { color: var(--fg-dim); }
.tab.disconnected .title { color: var(--fg-dim); font-style: italic; }
.tab .layout-icon {
  font-size: 11px; color: var(--fg-dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  letter-spacing: -1px;
}
.tab .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis;
}
.tab .close {
  border: none; background: transparent; padding: 0 4px; font-size: 14px;
  line-height: 1; color: var(--fg-dim); border-radius: 4px; opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.tab:hover .close, .tab.active .close { opacity: 1; }
.tab .close:hover { background: rgba(248, 81, 73, 0.18); color: var(--bad); }
.plus {
  border: none; background: transparent; color: var(--fg-dim);
  font-size: 18px; line-height: 1; padding: 0 14px; cursor: pointer;
  border-left: 1px solid var(--border); transition: color 120ms, background 120ms;
}
.plus:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.plus:disabled { opacity: 0.4; cursor: not-allowed; }
.ssh-btn {
  display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim);
  padding: 0 10px; cursor: pointer;
  border-left: 1px solid var(--border);
  transition: color 120ms, background 120ms;
}
.ssh-btn:hover { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.ssh-btn svg { display: block; }
.settings-btn {
  position: relative;
  display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim);
  padding: 0 10px; cursor: pointer;
  border-left: 1px solid var(--border);
  transition: color 120ms, background 120ms;
}
.settings-btn:hover { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.settings-btn svg { display: block; }
.settings-btn .dot {
  position: absolute; top: 4px; right: 4px;
  width: 6px; height: 6px;
  background: #d29922;
  border-radius: 50%;
}
.admin-btn {
  position: relative;
  display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim);
  padding: 0 10px; cursor: pointer;
  border-left: 1px solid var(--border);
  transition: color 120ms, background 120ms;
}
.admin-btn:hover { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.admin-btn.active { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
/* Unread tab. Same language as the sidebar row — a wash of the session's state
   colour plus a bolder title — but the accent bar sits at the bottom edge to
   match how .tab.active already marks itself, rather than on the left where a
   tab has no room for it. */
.tab.unread {
  background: color-mix(in srgb, var(--unread-c) 10%, transparent);
}
.tab.unread .title { font-weight: 600; color: #fff; }
/* Active wins the bottom bar (it is the tab you are looking at); unread keeps
   the wash, which .active's own background sits on top of. */
.tab.unread:not(.active) {
  box-shadow: inset 0 -2px 0 var(--unread-c);
}

@media (max-width: 767px) {
  .tabbar {
    height: 36px;
  }
  .tab {
    min-width: 86px;
    max-width: 150px;
    padding: 0 6px 0 10px;
    gap: 4px;
  }
  .tab .num {
    display: none;
  }
  .tab .close {
    opacity: 1;
    padding: 0 2px;
  }
  .settings-btn,
  .admin-btn {
    min-width: 40px;
    padding: 0 12px;
  }
}
</style>
