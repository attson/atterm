<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import TaskRowInner from "./TaskRowInner.vue";
import LastOutputIndicator from "./LastOutputIndicator.vue";
import SessionRowMenu, { type MenuItem } from "./SessionRowMenu.vue";
import SessionDetailsPopover from "./SessionDetailsPopover.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { getUserHomeDir } from "../lib/api";
import { useTaskPreset } from "../composables/useTaskPreset";
import { useSessionPins } from "../composables/useSessionPins";
import { useSessionSelection } from "../composables/useSessionSelection";
import { matchesSession } from "../lib/sessionMatch";
import { compareSessionsBySidebarOrder, sessionUrgencyIndex } from "../lib/sessionSort";
import { SESSION_DND_MIME, clearDraggingSession, setDraggingSession } from "../lib/paneDrop";
import {
  titleOrCommand,
  rowTitle,
  hostNameWithIndex,
  coResidentIndex,
  taskStateLabel,
  commandLabel,
} from "../lib/sessionLabel";

const preset = useTaskPreset();
const showStateLabel = computed(() => preset.active.value.showLabel);

const props = withDefaults(defineProps<{
  byHost: Record<string, RemoteSession[]>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  groupBy?: "host" | "state";
  byState?: Record<string, RemoteSession[]>;
  activeSessionId?: string | null;
  // Local host_id (from GetHostInfo). When set and groupBy=host, the local
  // group is pinned to the top of the list and tagged with a "本机" chip so
  // users with several relay-attached hosts can spot their own machine at
  // a glance. Empty string disables both behaviors.
  localHostId?: string;
  // Local OS hostname (from GetHostInfo). When ≥2 host_ids in byHost share
  // this hostname, those groups get a "#N" suffix (lex order by host_id) so
  // dev + prod (or future profiles) on the same machine become distinguishable.
  // Empty string disables the suffix entirely.
  localHost?: string;
  // Session ids currently opened as panes in this desktop window. Only
  // these rows show the close affordance.
  openSessionIds?: string[];
  // Live search query from the sidebar header. Empty string = no filter.
  // Matched against title / cwd / current_command (case-insensitive
  // substring). See docs/superpowers/specs/2026-07-24-sidebar-search-design.md.
  searchQuery?: string;
  // Resolves a session id to its current pane location (tab + pane index),
  // if it is open as a pane in this window. Supplied by the parent so this
  // component stays free of App-scope tab/pane state; used only to render
  // the "pane location" row in SessionDetailsPopover.
  paneLocationFor?: (id: string) => { tabId: string; paneIdx: number } | null;
  // Resolves a tabId to its 0-based display index, for the same popover row.
  tabIndexById?: (tabId: string) => number;
  // True when the session shares a tab with other panes, so "move to its own
  // tab" is a real action. Only App knows the tab/pane shape.
  canDetachSession?: (id: string) => boolean;
}>(), {
  groupBy: "host",
  byState: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
  openSessionIds: () => [],
  searchQuery: "",
  paneLocationFor: () => null,
  tabIndexById: () => 0,
  canDetachSession: () => false,
});

const emit = defineEmits<{
  (e: "open", session: RemoteSession): void;
  (e: "close", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "merge-selected"): void;
  (e: "close-selected"): void;
  (e: "detach-session", sessionId: string): void;
}>();

const { t } = useI18n();
const sel = useSessionSelection();

// STATE_ORDER controls the visual order of state groups: most-urgent first.
const STATE_ORDER: TaskState[] = [
  "waiting_input",
  "failed",
  "running",
  "completed",
  "idle",
  "disconnected",
  "closed",
];

const pins = useSessionPins();
// Normalized query — trimmed + lowercased once per input change, then handed
// verbatim to matchesSession (which does not re-lowercase). Empty string
// short-circuits the filter (matchesSession returns true unconditionally).
const q = computed(() => props.searchQuery.trim().toLocaleLowerCase());
const openSessionIdSet = computed(() => new Set(props.openSessionIds));

const groups = computed<Record<string, RemoteSession[]>>(() =>
  props.groupBy === "state" ? props.byState : props.byHost,
);
// Same source as `groups`, minus any session currently pinned — pinned
// sessions are pulled out of their host/state group entirely and surface
// only in the virtual pinned group above (see `pinnedSessions` below).
const filteredGroups = computed<Record<string, RemoteSession[]>>(() => {
  const out: Record<string, RemoteSession[]> = {};
  for (const [k, list] of Object.entries(groups.value)) {
    out[k] = list.filter(
      (s) => !pins.isPinned(s.session_id) && matchesSession(s, q.value),
    );
  }
  return out;
});
// Pinned rows stay in one group, but use the same ordering as state-group rows:
// unread first, then latest activity, with a deterministic session-id tie break.
const groupKeys = computed<string[]>(() => {
  if (props.groupBy === "state") {
    return STATE_ORDER.filter((s) => (filteredGroups.value[s] ?? []).length > 0);
  }
  // Pin the local host to the top so the user's own machine is the first
  // thing visible — the rest stays alphabetical for stability across
  // relay churn (a remote dropping in/out shouldn't reshuffle the list).
  const keys = Object.keys(groups.value).sort()
    .filter((k) => (filteredGroups.value[k] ?? []).length > 0);
  if (!props.localHostId) return keys;
  const i = keys.indexOf(props.localHostId);
  if (i <= 0) return keys;
  return [props.localHostId, ...keys.slice(0, i), ...keys.slice(i + 1)];
});
const foldOpen = ref(false);
// Per-group collapse state, in-memory only (matches `foldOpen`'s session-
// local lifetime). Each entry is a group key that is currently collapsed;
// absence means expanded. Works for both groupBy="host" and groupBy="state".
const collapsedGroups = ref<Set<string>>(new Set());
function isGroupCollapsed(key: string): boolean {
  // Active query overrides folded state: every group appears expanded so
  // matches inside are visible. The underlying set is not mutated — clearing
  // the query restores the user's real collapse state.
  if (q.value) return false;
  return collapsedGroups.value.has(key);
}
function toggleGroupCollapsed(key: string) {
  // Mutate a fresh Set so Vue's reactivity picks up the change — Vue's
  // shallow refs see `.add()` / `.delete()` as same-instance and skip the
  // re-render.
  const next = new Set(collapsedGroups.value);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  collapsedGroups.value = next;
}
const home = ref("");
const nowMs = ref(Date.now());
let clockTimer: number | null = null;

onMounted(async () => {
  clockTimer = window.setInterval(() => {
    nowMs.value = Date.now();
  }, 1000);
  try {
    home.value = await getUserHomeDir();
  } catch { /* leave empty — helper still truncates */ }
});

onBeforeUnmount(() => {
  if (clockTimer !== null) window.clearInterval(clockTimer);
});

// Sentinel collapse key for the virtual pinned group — safe against
// collisions since host_ids are UUIDs and states are the fixed enum above.
const PINNED_KEY = "__pinned__";

// Flattens byHost/byState (whichever is active) and keeps only pinned
// sessions, sorted by task_state urgency like every other group.
const pinnedSessions = computed<RemoteSession[]>(() => {
  const out: RemoteSession[] = [];
  const seen = new Set<string>();
  const source = props.groupBy === "state" ? props.byState ?? {} : props.byHost ?? {};
  for (const list of Object.values(source)) {
    for (const s of list) {
      if (seen.has(s.session_id)) continue;
      if (pins.isPinned(s.session_id) && matchesSession(s, q.value)) {
        seen.add(s.session_id);
        out.push(s);
      }
    }
  }
  out.sort(compareSessionsBySidebarOrder);
  return out;
});

// Completed-fold rows, filtered by the same query as every other group —
// keeps the fold's count/header/rows consistent with what the empty-state
// hint is judging "any match" against.
const completedFiltered = computed<RemoteSession[]>(() =>
  props.completedSeen.filter((s) => matchesSession(s, q.value)),
);

const hasAnyMatch = computed(
  () => pinnedSessions.value.length > 0
    || groupKeys.value.length > 0
    || completedFiltered.value.length > 0,
);

// Flat id list in the exact visual order rows render (excluding group
// headers, empty-hint, fold-toggle). Used as the axis for Shift+click
// range selection. Rebuild on any change to pinned/filtered/completed.
const orderedVisibleIds = computed<string[]>(() => {
  const out: string[] = [];
  if (!isGroupCollapsed(PINNED_KEY)) {
    for (const s of pinnedSessions.value) out.push(s.session_id);
  }
  for (const key of groupKeys.value) {
    if (isGroupCollapsed(key)) continue;
    for (const s of filteredGroups.value[key] ?? []) out.push(s.session_id);
  }
  if (foldOpen.value) {
    for (const s of completedFiltered.value) out.push(s.session_id);
  }
  return out;
});

function onRowClick(e: MouseEvent, s: RemoteSession) {
  if (suppressClickSessionId === s.session_id) {
    suppressClickSessionId = "";
    e.preventDefault();
    return;
  }
  if (e.shiftKey) {
    sel.selectRange(s.session_id, orderedVisibleIds.value);
    return;
  }
  if (e.metaKey || e.ctrlKey) {
    sel.toggle(s.session_id);
    return;
  }
  sel.clear();
  emit("open", s);
}

const menuState = ref<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
}>({ open: false, x: 0, y: 0, session: null });

function openRowMenuAt(x: number, y: number, s: RemoteSession) {
  menuState.value = { open: true, x, y, session: s };
}

function onRowMenu(e: MouseEvent, s: RemoteSession) {
  e.preventDefault();
  openRowMenuAt(e.clientX, e.clientY, s);
}

const LONG_PRESS_MS = 1000;
const LONG_PRESS_MOVE_TOLERANCE_PX = 10;
let longPressTimer: ReturnType<typeof setTimeout> | null = null;
let longPressStart: { x: number; y: number; sessionId: string } | null = null;
let suppressClickSessionId = "";

function clearLongPressTimer() {
  if (longPressTimer) {
    clearTimeout(longPressTimer);
    longPressTimer = null;
  }
  longPressStart = null;
}

function onRowPointerDown(e: PointerEvent, s: RemoteSession) {
  if (e.pointerType === "mouse" || e.button !== 0) return;
  clearLongPressTimer();
  longPressStart = { x: e.clientX, y: e.clientY, sessionId: s.session_id };
  longPressTimer = setTimeout(() => {
    longPressTimer = null;
    suppressClickSessionId = s.session_id;
    openRowMenuAt(e.clientX, e.clientY, s);
  }, LONG_PRESS_MS);
}

function onRowPointerMove(e: PointerEvent) {
  if (!longPressStart) return;
  const dx = Math.abs(e.clientX - longPressStart.x);
  const dy = Math.abs(e.clientY - longPressStart.y);
  if (dx > LONG_PRESS_MOVE_TOLERANCE_PX || dy > LONG_PRESS_MOVE_TOLERANCE_PX) {
    clearLongPressTimer();
  }
}

function onRowPointerEnd() {
  clearLongPressTimer();
}

// Drag a row onto a pane to show that session there. Mouse-only by nature:
// HTML5 drag events never fire on touch, where the long-press menu above is
// the equivalent affordance. The payload is a private MIME type so that a
// drop landing anywhere but our pane handler stays inert — notably xterm's
// hidden textarea, which would happily type a text/plain payload.
function onRowDragStart(e: DragEvent, s: RemoteSession) {
  // Stash first: the drop reads this, not dataTransfer (see paneDrop.ts).
  setDraggingSession(s.session_id);
  if (!e.dataTransfer) return;
  e.dataTransfer.setData(SESSION_DND_MIME, s.session_id);
  e.dataTransfer.effectAllowed = "move";
}

function onRowDragEnd() {
  clearDraggingSession();
}

onBeforeUnmount(clearLongPressTimer);

function closeMenu() {
  menuState.value = { ...menuState.value, open: false, session: null };
}

const menuItems = computed<MenuItem[]>(() => {
  const s = menuState.value.session;
  if (!s) return [];
  const n = sel.size.value;
  const multi = n >= 2 && sel.isSelected(s.session_id);
  if (multi) {
    const openCount = Array.from(sel.selectedIds.value).filter((id) =>
      openSessionIdSet.value.has(id),
    ).length;
    return [
      {
        key: "merge",
        label: t("tasks.rowMenu.merge", { n }),
        disabled: n > 4,
      },
      {
        key: "close",
        label: t("tasks.rowMenu.closeSelected", { n: openCount }),
        disabled: openCount === 0,
      },
      {
        key: pins.isPinned(s.session_id) ? "unpin" : "pin",
        label: pins.isPinned(s.session_id)
          ? t("tasks.pinned.menuUnpin")
          : t("tasks.pinned.menuPin"),
      },
      {
        key: "details",
        label: t("tasks.rowMenu.details"),
        disabled: true,
      },
    ];
  }
  const items: MenuItem[] = [
    {
      key: pins.isPinned(s.session_id) ? "unpin" : "pin",
      label: pins.isPinned(s.session_id)
        ? t("tasks.pinned.menuUnpin")
        : t("tasks.pinned.menuPin"),
    },
  ];
  items.push({ key: "details", label: t("tasks.rowMenu.details") });
  // Last, and only when it would do something: a session that already owns its
  // tab, or is not open at all, has nothing to detach from.
  if (props.canDetachSession(s.session_id)) {
    items.push({ key: "detach", label: t("terminal.detachToTab") });
  }
  return items;
});

const detailsState = ref<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
}>({ open: false, x: 0, y: 0, session: null });

function onMenuSelect(key: string) {
  const s = menuState.value.session;
  if (!s) return;
  if (key === "pin" || key === "unpin") {
    pins.toggle(s.session_id);
  } else if (key === "details") {
    detailsState.value = {
      open: true,
      x: menuState.value.x,
      y: menuState.value.y,
      session: s,
    };
  } else if (key === "merge") {
    emit("merge-selected");
  } else if (key === "close") {
    emit("close-selected");
  } else if (key === "detach") {
    emit("detach-session", s.session_id);
  }
}

function closeDetails() {
  detailsState.value = { ...detailsState.value, open: false, session: null };
}

const coResidentMap = computed<Map<string, number>>(() =>
  coResidentIndex(props.byHost, props.localHost ?? ""),
);

function hostName(hostId: string): string {
  return hostNameWithIndex(
    hostId,
    groups.value[hostId],
    t("sessions.unknownHost"),
    coResidentMap.value.get(hostId),
  );
}

function groupHeader(key: string): string {
  if (props.groupBy === "state") return stateLabel(key);
  return hostName(key);
}

function groupPrimaryState(key: string): TaskState {
  if (props.groupBy === "state") return (key as TaskState);
  // Derive from the filtered (post-pin) list so the header icon matches
  // what's actually rendered underneath. The caller only invokes this for
  // groups whose filtered list is non-empty (see the `v-if` guarding the
  // `<section>` below), so `list` is always non-empty here.
  const list = filteredGroups.value[key] ?? [];
  let best = list[0];
  for (const s of list) {
    if (sessionUrgencyIndex(s.task_state) < sessionUrgencyIndex(best.task_state)) best = s;
  }
  return (best.task_state as TaskState | undefined) ?? "idle";
}

function unreadIdsForGroup(key: string): string[] {
  return (filteredGroups.value[key] ?? []).filter((s) => s.unread).map((s) => s.session_id);
}

function onMarkGroup(key: string) {
  emit("markSeen", { ids: unreadIdsForGroup(key) });
}
function onMarkFold() {
  emit("markSeen", { ids: props.completedSeen.map((s) => s.session_id) });
}

// Thin wrapper so existing template call sites `stateLabel(s.task_state)`
// stay terse — the actual i18n key switch lives in lib/sessionLabel.
function stateLabel(state: string | undefined): string {
  return taskStateLabel(state, t);
}
</script>

<template>
  <div class="task-grouped-list">
    <template v-if="pinnedSessions.length > 0">
      <div
        class="group-header pinned-group"
        data-test="pinned-group-header"
        role="button"
        tabindex="0"
        :aria-expanded="!isGroupCollapsed(PINNED_KEY)"
        @click="toggleGroupCollapsed(PINNED_KEY)"
        @keydown.enter.prevent="toggleGroupCollapsed(PINNED_KEY)"
        @keydown.space.prevent="toggleGroupCollapsed(PINNED_KEY)"
      >
        <span class="caret" aria-hidden="true">
          <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor">
            <path v-if="isGroupCollapsed(PINNED_KEY)" d="M3 1 L7 5 L3 9 Z" />
            <path v-else d="M1 3 L9 3 L5 8 Z" />
          </svg>
        </span>
        <span class="pin-icon" aria-hidden="true">
          <!-- Bookmark tag (V-notch bottom). Original 📌 emoji + complex
               diagonal-pin SVG both failed to render cleanly on iOS 26.3;
               a simple filled shape is unambiguous and font-independent. -->
          <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
            <path d="M4 2 h8 v12 l-4 -3 l-4 3 z" />
          </svg>
        </span>
        <span class="group-title">{{ t("tasks.pinned.title") }}</span>
        <span class="group-count">{{ pinnedSessions.length }}</span>
      </div>
      <template v-if="!isGroupCollapsed(PINNED_KEY)">
        <button
          v-for="s in pinnedSessions"
          :key="s.session_id"
          class="task-row"
          :class="{ active: s.session_id === activeSessionId, selected: sel.isSelected(s.session_id) }"
          data-test="task-row"
          :data-session-id="s.session_id"
          :data-active="s.session_id === activeSessionId ? 'true' : undefined"
          :data-selected="sel.isSelected(s.session_id) ? 'true' : undefined"
          @click="(e) => onRowClick(e, s)"
          @contextmenu="onRowMenu($event, s)"
          @pointerdown="onRowPointerDown($event, s)"
          @pointermove="onRowPointerMove($event)"
          @pointerup="onRowPointerEnd"
          @pointercancel="onRowPointerEnd"
          @pointerleave="onRowPointerEnd"
          draggable="true"
          @dragstart="onRowDragStart($event, s)"
          @dragend="onRowDragEnd"
        >
          <TaskRowInner
            :session="s"
            :show-state-label="showStateLabel"
            :home="home"
            :show-close="openSessionIdSet.has(s.session_id)"
            :now-ms="nowMs"
            @close="emit('close', s)"
          />
        </button>
      </template>
    </template>
    <template v-for="key in groupKeys" :key="key">
    <section
      v-if="(filteredGroups[key] ?? []).length > 0"
      class="host-group"
      :data-test="`host-group-${key}`"
    >
      <header
        class="host-header"
        data-test="host-header"
        role="button"
        tabindex="0"
        :aria-expanded="!isGroupCollapsed(key)"
        @click="toggleGroupCollapsed(key)"
        @keydown.enter.prevent="toggleGroupCollapsed(key)"
        @keydown.space.prevent="toggleGroupCollapsed(key)"
      >
        <span class="caret" aria-hidden="true">
          <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor">
            <path v-if="isGroupCollapsed(key)" d="M3 1 L7 5 L3 9 Z" />
            <path v-else d="M1 3 L9 3 L5 8 Z" />
          </svg>
        </span>
        <span class="host-name">{{ groupHeader(key) }}</span>
        <span
          v-if="groupBy === 'host' && localHostId && key === localHostId"
          class="local-chip"
          data-test="local-chip"
        >
          {{ t("sessions.thisMachine") }}
        </span>
        <span class="counts">
          <TaskStateIcon :state="groupPrimaryState(key)" :size="10" />
          <span class="count">{{ (filteredGroups[key] ?? []).length }}</span>
        </span>
        <span v-if="unreadIdsForGroup(key).length > 0" class="unread-badge">
          {{ t("tasks.unreadBadge", { count: unreadIdsForGroup(key).length }) }}
        </span>
        <button
          v-if="unreadIdsForGroup(key).length > 0"
          class="mark-all"
          data-test="host-mark-all"
          :title="t('tasks.markAllRead')"
          :aria-label="t('tasks.markAllRead')"
          @click.stop="onMarkGroup(key)"
        >
          <!-- ✓ (U+2713) renders as .notdef on iOS 26.3. -->
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M3 8 l3 3 l7 -7" />
          </svg>
        </button>
      </header>
      <button
        v-for="s in (isGroupCollapsed(key) ? [] : filteredGroups[key])"
        :key="s.session_id"
        class="task-row"
        :class="{ active: s.session_id === activeSessionId, selected: sel.isSelected(s.session_id) }"
        data-test="task-row"
        :data-session-id="s.session_id"
        :data-active="s.session_id === activeSessionId ? 'true' : undefined"
        :data-selected="sel.isSelected(s.session_id) ? 'true' : undefined"
        @click="(e) => onRowClick(e, s)"
        @contextmenu="onRowMenu($event, s)"
        @pointerdown="onRowPointerDown($event, s)"
        @pointermove="onRowPointerMove($event)"
        @pointerup="onRowPointerEnd"
        @pointercancel="onRowPointerEnd"
        @pointerleave="onRowPointerEnd"
        draggable="true"
        @dragstart="onRowDragStart($event, s)"
        @dragend="onRowDragEnd"
      >
        <TaskRowInner
          :session="s"
          :show-state-label="showStateLabel"
          :home="home"
          :show-close="openSessionIdSet.has(s.session_id)"
          :now-ms="nowMs"
          @close="emit('close', s)"
        />
      </button>
    </section>
    </template>
    <div
      v-if="q && !hasAnyMatch"
      class="empty-hint"
      data-test="search-empty"
    >
      {{ t('tasks.search.empty', { q: props.searchQuery }) }}
    </div>
    <section v-if="completedFiltered.length > 0" class="completed-fold">
      <button
        class="fold-toggle"
        data-test="completed-fold-toggle"
        @click="foldOpen = !foldOpen"
      >
        <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true">
          <path v-if="foldOpen" d="M1 3 L9 3 L5 8 Z" />
          <path v-else d="M3 1 L7 5 L3 9 Z" />
        </svg>
        {{ t("tasks.completedFold") }} ({{ completedFiltered.length }})
      </button>
      <template v-if="foldOpen">
        <div
          v-for="s in completedFiltered"
          :key="s.session_id"
          class="task-row dim"
          :class="{ active: s.session_id === activeSessionId, selected: sel.isSelected(s.session_id) }"
          data-test="completed-fold-row"
          :data-active="s.session_id === activeSessionId ? 'true' : undefined"
          :data-selected="sel.isSelected(s.session_id) ? 'true' : undefined"
          @click="(e) => onRowClick(e, s)"
          @contextmenu="onRowMenu($event, s)"
          @pointerdown="onRowPointerDown($event, s)"
          @pointermove="onRowPointerMove($event)"
          @pointerup="onRowPointerEnd"
          @pointercancel="onRowPointerEnd"
          @pointerleave="onRowPointerEnd"
        >
          <span class="row-main">
            <span class="row-top">
              <TaskStateIcon
                :state="(s.task_state as TaskState | undefined) ?? 'idle'"
                :unread="s.unread === true"
              />
              <span
                v-if="showStateLabel"
                class="state-label"
                data-test="state-label"
              >{{ stateLabel(s.task_state) }}</span>
              <span class="cmd-and-cwd" :title="rowTitle(s)">
                <span class="cmd">{{ titleOrCommand(s) }}</span>
              </span>
            </span>
            <span
              v-if="shortenCwd(s.cwd, home)"
              class="row-meta"
              data-test="row-meta"
            >
              <span class="cwd">{{ shortenCwd(s.cwd, home) }}</span>
            </span>
          </span>
          <!-- Same tail as TaskRowInner; this fold row is a hand-written
               reduced view (no unread / mark-read), so the two have to be
               kept in step by hand. -->
          <span
            class="row-tail"
            :class="{ 'time-only': s.type !== 'ai' }"
            data-test="row-tail"
          >
            <span v-if="s.type === 'ai'" class="kind" data-test="row-kind">{{ commandLabel(s) }}</span>
            <LastOutputIndicator
              :last-output-at="s.last_output_at"
              :task-state="s.task_state"
              :now-ms="nowMs"
            />
          </span>
          <button
            v-if="openSessionIdSet.has(s.session_id)"
            class="row-close completed-row-close"
            data-test="row-close"
            :title="t('common.close')"
            :aria-label="t('common.close')"
            @click.stop="emit('close', s)"
          >
            ×
          </button>
        </div>
        <button class="fold-mark-all" data-test="fold-mark-all" @click="onMarkFold">
          {{ t("tasks.markAllRead") }}
        </button>
      </template>
    </section>
    <SessionRowMenu
      :open="menuState.open"
      :x="menuState.x"
      :y="menuState.y"
      :items="menuItems"
      @close="closeMenu"
      @select="onMenuSelect"
    />
    <SessionDetailsPopover
      :open="detailsState.open"
      :x="detailsState.x"
      :y="detailsState.y"
      :session="detailsState.session"
      :pane-location="detailsState.session ? paneLocationFor(detailsState.session.session_id) : null"
      :tab-index-by-id="tabIndexById"
      @close="closeDetails"
    />
  </div>
</template>

<style scoped>
.task-grouped-list { display: flex; flex-direction: column; gap: 8px; font-size: 12px; }
.host-header { display: flex; align-items: center; gap: 6px; font-weight: 500; padding: 4px 6px; cursor: pointer; user-select: none; }
.host-header:hover { background: rgba(255, 255, 255, 0.04); border-radius: 4px; }
.host-header .caret { font-size: 9px; opacity: 0.7; width: 9px; display: inline-block; }
.host-name { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.counts { margin-left: auto; display: inline-flex; gap: 2px; align-items: center; flex-shrink: 0; }
.unread-badge { font-size: 10px; opacity: 0.8; background: rgba(255, 255, 255, 0.06); border-radius: 3px; padding: 1px 4px; white-space: nowrap; flex-shrink: 0; }
.local-chip { font-size: 10px; color: var(--accent); border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent); border-radius: 3px; padding: 0 4px; line-height: 1.3; white-space: nowrap; flex-shrink: 0; }
.mark-all { background: none; border: none; cursor: pointer; padding: 0 4px; color: inherit; }
.host-group { display: flex; flex-direction: column; gap: 4px; }
.group-header { display: flex; align-items: center; gap: 6px; font-weight: 500; padding: 4px 6px; cursor: pointer; user-select: none; }
.group-header:hover { background: rgba(255, 255, 255, 0.04); border-radius: 4px; }
.group-header .caret { font-size: 9px; opacity: 0.7; width: 9px; display: inline-block; }
.pinned-group .pin-icon { font-size: 11px; }
.group-title { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.group-count { margin-left: auto; font-size: 10px; opacity: 0.8; background: rgba(255, 255, 255, 0.06); border-radius: 3px; padding: 1px 4px; }
/* Row direction: the stacked title/cwd lines live in .row-main, and .row-tail
   holds the kind badge + last-output column on the right — same shape as the
   desk widget's row, so the two surfaces cannot drift. */
.task-row { display: flex; flex-direction: row; align-items: stretch; gap: 6px; padding: 5px 8px; border: 1px solid rgba(255, 255, 255, 0.08); background: rgba(255, 255, 255, 0.02); width: 100%; text-align: left; cursor: pointer; color: inherit; border-radius: 6px; user-select: none; -webkit-user-select: none; -webkit-touch-callout: none; touch-action: pan-y; position: relative; }
/* The close × is a per-row action, not status: keep it out of the scan path
   until the row is pointed at. Touch has no hover, so it stays visible there.
   `.active` deliberately does NOT show it — the highlighted row is the one the
   user is reading, and a × parked on it is both visual noise and the easiest
   one to hit by accident. focus-within stays: it is the keyboard's hover, and
   without it the control would be unreachable without a pointer. */
.task-row :deep(.row-close) { opacity: 0; transition: opacity 100ms; }
.task-row:hover :deep(.row-close),
.task-row:focus-within :deep(.row-close) { opacity: 1; }
/* × and the kind badge share one slot — whenever × shows, the badge yields.
   Keeps the close affordance at the row's right edge without spending a
   permanent column on it, and without × ever landing on top of the badge. */
.task-row:hover :deep(.kind),
.task-row:focus-within :deep(.kind) { opacity: 0; }
@media (hover: none) {
  /* No hover on touch means × is always visible, which under the swap would
     hide the kind badge forever. Give × its own column there instead. */
  .task-row :deep(.row-close) { position: static; align-self: flex-start; opacity: 1; }
  .task-row :deep(.kind) { opacity: 1; }
}
.task-row:hover { background: rgba(255, 255, 255, 0.06); border-color: rgba(255, 255, 255, 0.16); }
.task-row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); border-color: color-mix(in srgb, var(--accent) 28%, var(--border)); box-shadow: inset 2px 0 0 var(--accent); }
.task-row.active:hover { background: color-mix(in srgb, var(--accent) 14%, transparent); }
.task-row.selected {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.task-row.dim { opacity: 0.6; }
.task-row.dim.active { opacity: 0.9; }
.row-main { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.row-top { display: flex; align-items: center; gap: 6px; min-width: 0; }
.state-label { font-size: 11px; opacity: 0.85; white-space: nowrap; flex-shrink: 0; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); flex: 1 1 auto; min-width: 0; }
.row-meta { display: flex; align-items: center; gap: 6px; min-width: 0; padding-left: 18px; }
.cwd { color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; flex: 1 1 auto; min-width: 0; }
/* Mirrors TaskRowInner's tail — see the comment there for why min-width. */
.row-tail {
  flex: 0 0 auto;
  min-width: 46px;
  align-self: stretch;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  justify-content: space-between;
  gap: 3px;
}
.row-tail.time-only { justify-content: flex-end; }
.row-tail :deep(.last-output) { opacity: 0.65; }
.kind {
  font-size: 8.5px;
  line-height: 1.4;
  padding: 1px 4px;
  border-radius: 3px;
  border: 1px solid var(--border);
  color: var(--fg-dim);
  white-space: nowrap;
  transition: opacity 100ms;
}
.completed-row-close {
  position: absolute;
  top: 5px;
  right: 8px;
  border: none;
  background: transparent;
  color: var(--fg-dim);
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  line-height: 1;
  padding: 1px 4px;
}
.completed-row-close:hover { background: rgba(248, 81, 73, 0.18); color: var(--bad); }
.completed-fold { border-top: 1px solid rgba(255, 255, 255, 0.06); margin-top: 6px; padding-top: 4px; display: flex; flex-direction: column; gap: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 4px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.7; }
.fold-mark-all { background: none; border: 1px solid rgba(255, 255, 255, 0.12); cursor: pointer; padding: 4px 8px; margin: 4px 6px; color: inherit; border-radius: 3px; }
.empty-hint {
  padding: 24px 12px;
  text-align: center;
  color: var(--fg);
  opacity: 0.55;
  font-size: 12px;
}
</style>
