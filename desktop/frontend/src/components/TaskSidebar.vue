<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskGroupedList from "./TaskGroupedList.vue";
import TaskStateIcon from "./TaskStateIcon.vue";
import BulkActionBar from "./BulkActionBar.vue";
import { useI18n } from "../i18n/useI18n";
import { getTaskSidebarWidth, setTaskSidebarWidth } from "../lib/api";
import { useTaskGroupBy } from "../composables/useTaskGroupBy";
import { useSessionSelection } from "../composables/useSessionSelection";

const { t } = useI18n();
const groupByState = useTaskGroupBy();

const props = withDefaults(defineProps<{
  collapsed: boolean;
  byHost: Record<string, RemoteSession[]>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  totalUnread: number;
  byStateGroups?: Record<string, RemoteSession[]>;
  activeSessionId?: string | null;
  openSessionIds?: string[];
  // Pinned to the top of the host group list and tagged with a "本机"
  // chip in the header. Forwarded as-is to TaskGroupedList.
  localHostId?: string;
  // Local OS hostname; forwarded so TaskGroupedList can render "#N"
  // suffixes when multiple atterm instances on this machine share it.
  localHost?: string;
  paneLocationFor?: (id: string) => { tabId: string; paneIdx: number } | null;
  tabIndexById?: (tabId: string) => number;
}>(), {
  byStateGroups: () => ({}),
  activeSessionId: null,
  openSessionIds: () => [],
  localHostId: "",
  localHost: "",
  paneLocationFor: () => null,
  tabIndexById: () => 0,
});

const emit = defineEmits<{
  (e: "update:collapsed", v: boolean): void;
  (e: "open", session: RemoteSession): void;
  (e: "close", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "merge-selected"): void;
  (e: "close-selected"): void;
}>();

const sel = useSessionSelection();
const openCount = computed(() => {
  const opens = new Set(props.openSessionIds ?? []);
  let n = 0;
  for (const id of sel.selectedIds.value) if (opens.has(id)) n++;
  return n;
});
const canMerge = computed(() => sel.size.value >= 1 && sel.size.value <= 4);

function onSidebarKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && sel.size.value > 0) {
    e.preventDefault();
    sel.clear();
  }
}

function onSidebarBlankClick(e: MouseEvent) {
  if (sel.size.value === 0) return;
  const el = e.target as HTMLElement | null;
  if (!el) return;
  // Don't clear if the click landed inside a row, menu, popover, or the
  // bulk bar itself (those handle their own selection semantics).
  if (
    el.closest(
      ".task-row, .host-header, .group-header, .bulk-bar, .session-row-menu, .session-details-popover, .resize-handle",
    )
  ) {
    return;
  }
  sel.clear();
}

const widthPx = ref(240);
const minWidth = 180;
const maxWidth = 480;
let dragOriginX = 0;
let dragOriginWidth = 0;
let dragging = false;

const query = ref("");
const searchEl = ref<HTMLInputElement | null>(null);
const searchOpen = ref(false);

async function openSearch(): Promise<void> {
  searchOpen.value = true;
  await nextTick();
  searchEl.value?.focus();
  searchEl.value?.select();
}

function onSearchBlur() {
  if (query.value === "") searchOpen.value = false;
}

function onSearchEsc() {
  if (query.value !== "") {
    query.value = "";
    return;
  }
  searchOpen.value = false;
}

async function focusSearch(): Promise<void> {
  if (props.collapsed) {
    emit("update:collapsed", false);
    await nextTick();
  }
  await openSearch();
}

defineExpose({ focusSearch });

onMounted(async () => {
  try {
    const stored = await getTaskSidebarWidth();
    if (stored > 0) widthPx.value = clampWidth(stored);
  } catch {
    /* default 240 */
  }
});

function clampWidth(px: number): number {
  return Math.max(minWidth, Math.min(maxWidth, px));
}

// Below 768px the sidebar becomes an overlay drawer (position:fixed,
// slid off-screen until toggled via the hamburger button) rather than
// an inline column. Resizing back to wide always resets the drawer to
// closed so it doesn't linger open once it's no longer an overlay.
const isNarrow = ref(false);
const drawerOpen = ref(false);

function updateIsNarrow() {
  const wasNarrow = isNarrow.value;
  isNarrow.value = window.innerWidth < 768;
  if (wasNarrow && !isNarrow.value) drawerOpen.value = false;
}

// ESC-to-close: when the drawer is open, pressing Escape anywhere in the
// window collapses it back to hidden. Keeps the drawer feeling modal-like
// without a full backdrop overlay. Gated on drawerOpen so it doesn't
// intercept ESC on the desktop layout (where the sidebar is always
// inline).
function onWindowKeydown(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  if (!drawerOpen.value) return;
  drawerOpen.value = false;
  e.preventDefault();
}

onMounted(() => {
  updateIsNarrow();
  window.addEventListener("resize", updateIsNarrow);
  window.addEventListener("keydown", onWindowKeydown);
});
onBeforeUnmount(() => {
  window.removeEventListener("resize", updateIsNarrow);
  window.removeEventListener("keydown", onWindowKeydown);
});

function onDragStart(e: PointerEvent) {
  if (props.collapsed) return;
  dragging = true;
  dragOriginX = e.clientX;
  dragOriginWidth = widthPx.value;
  try {
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  } catch {
    /* JSDOM may not implement pointer capture */
  }
}

function onDragMove(e: PointerEvent) {
  if (!dragging) return;
  widthPx.value = clampWidth(dragOriginWidth + (e.clientX - dragOriginX));
}

async function onDragEnd(e: PointerEvent) {
  if (!dragging) return;
  dragging = false;
  try {
    (e.target as HTMLElement).releasePointerCapture(e.pointerId);
  } catch {
    /* element may have been re-rendered or capture unsupported */
  }
  try {
    await setTaskSidebarWidth(widthPx.value);
  } catch {
    /* persistence is best-effort */
  }
}

const URGENCY: TaskState[] = [
  "waiting_input",
  "failed",
  "running",
  "completed",
  "idle",
  "disconnected",
  "closed",
];
function urgencyIndex(s?: TaskState | string): number {
  if (!s) return URGENCY.length;
  const i = URGENCY.indexOf(s as TaskState);
  return i === -1 ? URGENCY.length : i;
}

function onToggleGroupBy() {
  const next = groupByState.activeId.value === "state" ? "host" : "state";
  void groupByState.setGroupBy(next);
}

const railIcons = computed(() => {
  const all: RemoteSession[] = [];
  for (const list of Object.values(props.byHost)) all.push(...list);
  all.sort((a, b) => urgencyIndex(a.task_state) - urgencyIndex(b.task_state));
  return all.slice(0, 20);
});
</script>

<template>
  <!-- `display: contents` keeps this a single Vue template root (so
       non-declared attrs from the parent still fall through onto the
       `<aside>` without a "renders fragment root nodes" warning) while
       the hamburger stays a *sibling* of `.task-sidebar`, not a
       descendant — it must not inherit the drawer's translateX, or it
       would slide off-screen along with the closed drawer. -->
  <div class="task-sidebar-shell">
    <button
      v-if="isNarrow"
      data-test="sidebar-hamburger"
      class="sidebar-hamburger"
      :aria-label="drawerOpen ? t('tasks.sidebar.closeDrawer') : t('tasks.sidebar.openDrawer')"
      @click="drawerOpen = !drawerOpen"
    >
      <!-- ☰ (U+2630) renders as .notdef "?" on iOS 26.3; SVG hamburger. -->
      <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true">
        <line x1="3" y1="4.5" x2="13" y2="4.5" />
        <line x1="3" y1="8" x2="13" y2="8" />
        <line x1="3" y1="11.5" x2="13" y2="11.5" />
      </svg>
    </button>
    <aside
      class="task-sidebar"
      :class="{ collapsed, drawer: isNarrow, open: isNarrow && drawerOpen }"
      :style="!collapsed ? { width: widthPx + 'px' } : undefined"
      tabindex="-1"
      @keydown="onSidebarKeydown"
      @click.capture="onSidebarBlankClick"
    >
      <div
        v-if="!collapsed"
        class="resize-handle"
        data-test="sidebar-resize-handle"
        @pointerdown="onDragStart"
        @pointermove="onDragMove"
        @pointerup="onDragEnd"
        @pointercancel="onDragEnd"
      />
      <div v-if="collapsed" class="rail" data-test="sidebar-rail">
        <button
          class="expand-button"
          :title="t('tasks.sidebar.expand')"
          :aria-label="t('tasks.sidebar.expand')"
          @click="emit('update:collapsed', false)"
        >
          <!-- » (U+00BB) renders on iOS 26.3 but keep parity with «. -->
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polyline points="3 2 7 6 3 10" />
            <polyline points="6 2 10 6 6 10" />
          </svg>
        </button>
        <span v-if="totalUnread > 0" class="rail-badge" data-test="sidebar-rail-badge">
          {{ totalUnread }}
        </span>
        <span
          v-for="s in railIcons"
          :key="s.session_id"
          class="rail-icon"
          data-test="sidebar-rail-icon"
          role="button"
          tabindex="0"
          :aria-label="s.current_command || s.title || s.session_id.slice(0, 8)"
          @click="emit('open', s)"
          @keydown.enter.prevent="emit('open', s)"
          @keydown.space.prevent="emit('open', s)"
        >
          <TaskStateIcon
            :state="(s.task_state as TaskState | undefined) ?? 'idle'"
            :size="14"
          />
        </span>
      </div>
      <div v-else class="expanded">
        <header class="sidebar-header">
          <span v-if="!(searchOpen || query)" class="title">{{ t("tasks.sidebar.title") }}</span>
          <button
            v-if="!(searchOpen || query)"
            class="search-icon-btn"
            data-test="sidebar-search-toggle"
            :title="t('tasks.sidebar.searchPlaceholder')"
            :aria-label="t('tasks.sidebar.searchPlaceholder')"
            @click="openSearch"
          >
            <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
              <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" stroke-width="1.5" />
              <line x1="10.5" y1="10.5" x2="14" y2="14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </button>
          <input
            v-else
            ref="searchEl"
            v-model="query"
            type="search"
            class="sidebar-search"
            data-test="sidebar-search"
            :placeholder="t('tasks.sidebar.searchPlaceholder')"
            :aria-label="t('tasks.sidebar.searchPlaceholder')"
            @keydown.esc.prevent="onSearchEsc"
            @blur="onSearchBlur"
          />
          <button
            v-if="!(searchOpen || query)"
            class="group-toggle"
            data-test="group-toggle"
            :title="t('tasks.settings.groupBy')"
            @click="onToggleGroupBy"
          >
            {{ groupByState.activeId.value === 'state'
              ? t('tasks.settings.groupByState')
              : t('tasks.settings.groupByHost') }}
          </button>
          <button
            class="collapse-button"
            data-test="collapse-button"
            :title="t('tasks.sidebar.collapse')"
            :aria-label="t('tasks.sidebar.collapse')"
            @click="emit('update:collapsed', true)"
          >
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <polyline points="9 2 5 6 9 10" />
              <polyline points="6 2 2 6 6 10" />
            </svg>
          </button>
        </header>
        <div class="list-wrap" data-test="task-grouped-list">
          <TaskGroupedList
            :by-host="byHost"
            :primary-state-for-host="primaryStateForHost"
            :completed-seen="completedSeen"
            :group-by="groupByState.activeId.value"
            :by-state="byStateGroups"
            :active-session-id="activeSessionId"
            :open-session-ids="openSessionIds"
            :local-host-id="localHostId"
            :local-host="localHost"
            :search-query="query"
            :pane-location-for="paneLocationFor"
            :tab-index-by-id="tabIndexById"
            @open="(s) => emit('open', s)"
            @close="(s) => emit('close', s)"
            @markSeen="(p) => emit('markSeen', p)"
            @merge-selected="emit('merge-selected')"
            @close-selected="emit('close-selected')"
          />
        </div>
        <footer v-if="sel.size.value >= 1">
          <BulkActionBar
            :count="sel.size.value"
            :open-count="openCount"
            :can-merge="canMerge"
            @merge="emit('merge-selected')"
            @close-selected="emit('close-selected')"
            @clear="sel.clear()"
          />
        </footer>
        <footer v-else-if="totalUnread > 0">
          <button
            class="mark-all"
            data-test="sidebar-mark-all"
            @click="emit('markSeen', { all: true })"
          >
            {{ t("tasks.markAllRead") }}
          </button>
        </footer>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.task-sidebar-shell {
  /* Not itself a layout box — its children (hamburger + aside) participate
     directly in the parent's flex/grid as if this wrapper weren't there. */
  display: contents;
}
.task-sidebar {
  position: relative;
  background: var(--panel);
  border-right: 1px solid var(--border);
  color: var(--fg);
  display: flex;
  flex-direction: column;
  height: 100%;
}
.task-sidebar.collapsed { width: 32px; }
.sidebar-hamburger {
  /* Position below TabBar (height 28px in components/TabBar.vue) so the
     first tab's label stays clickable at narrow widths. Was `top: 12px`,
     which overlapped the leftmost tab and hid its content. */
  position: fixed;
  top: 34px;
  left: 8px;
  z-index: 20;
  padding: 4px 8px;
  background: var(--panel);
  border: 1px solid var(--border);
  color: inherit;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
}
.task-sidebar.drawer {
  position: fixed;
  top: 0;
  bottom: 0;
  left: 0;
  z-index: 15;
  transform: translateX(-100%);
  transition: transform 0.2s ease;
  box-shadow: 2px 0 8px rgba(0, 0, 0, 0.3);
}
.task-sidebar.drawer.open { transform: translateX(0); }
/* expanded width comes from inline :style="{ width: widthPx + 'px' }" */
/* .expanded must itself be a flex column so .list-wrap's `flex: 1 1 auto;
   overflow-y: auto` actually engages. Without this, .list-wrap's flex props
   no-op (parent isn't flex), and the session list grows to its content size,
   overflowing the sidebar and pulling the window taller than the viewport. */
.expanded {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.resize-handle {
  position: absolute;
  top: 0;
  right: -2px;
  width: 4px;
  height: 100%;
  cursor: ew-resize;
  user-select: none;
  z-index: 1;
}
.resize-handle:hover { background: rgba(255, 255, 255, 0.06); }
.sidebar-header {
  display: flex;
  align-items: center;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}
.title {
  flex: 1 1 auto;
  min-width: 0;
  font-weight: 500;
  margin-right: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.search-icon-btn {
  background: none;
  border: none;
  color: inherit;
  cursor: pointer;
  padding: 2px 4px;
  margin-right: 6px;
  opacity: 0.7;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 3px;
}
.search-icon-btn:hover { opacity: 1; background: rgba(255, 255, 255, 0.05); }
.collapse-button,
.expand-button {
  background: none;
  border: none;
  cursor: pointer;
  color: inherit;
  font-size: 14px;
}
.group-toggle {
  background: none;
  border: 1px solid var(--border);
  color: inherit;
  cursor: pointer;
  border-radius: 3px;
  font-size: 11px;
  padding: 1px 6px;
  margin-right: 6px;
  opacity: 0.8;
}
.group-toggle:hover { opacity: 1; background: rgba(255, 255, 255, 0.05); }
.sidebar-search {
  flex: 1 1 auto;
  min-width: 60px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid transparent;
  color: inherit;
  border-radius: 3px;
  padding: 1px 6px;
  font-size: 12px;
  font-family: inherit;
  line-height: 20px;
  margin-right: 6px;
  outline: none;
}
.sidebar-search:focus { border-color: var(--border); background: rgba(255, 255, 255, 0.05); }
.sidebar-search::placeholder { opacity: 0.5; }
/* Chromium/WebKit render a native × clear button for type="search"; leave it
   alone (colors match via `color: inherit`). */
.list-wrap {
  flex: 1 1 auto;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px;
  /* Hide the WebKit scrollbar gutter without disabling scroll — the sidebar
     already has a clear "more below" affordance via the trailing rows. */
  scrollbar-width: none;
}
.list-wrap::-webkit-scrollbar { display: none; }
footer { padding: 8px; border-top: 1px solid var(--border); }
.mark-all {
  background: none;
  border: 1px solid var(--border);
  cursor: pointer;
  padding: 6px 10px;
  color: inherit;
  border-radius: 3px;
  width: 100%;
}
.rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 6px 0;
  gap: 4px;
}
.rail-badge {
  font-size: 10px;
  background: #ef4444;
  color: white;
  border-radius: 8px;
  padding: 1px 5px;
}
.rail-icon { cursor: pointer; padding: 2px; }

@media (max-width: 767px) {
  .sidebar-hamburger {
    top: calc(env(safe-area-inset-top) + 42px);
    left: calc(env(safe-area-inset-left) + 8px);
    min-height: 32px;
  }
  .task-sidebar.drawer {
    top: env(safe-area-inset-top);
    bottom: env(safe-area-inset-bottom);
    max-width: min(86vw, 360px);
  }
}
</style>
