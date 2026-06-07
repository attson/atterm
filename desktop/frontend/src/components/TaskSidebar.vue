<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskGroupedList from "./TaskGroupedList.vue";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { getTaskSidebarWidth, setTaskSidebarWidth } from "../lib/api";

const { t } = useI18n();

const props = defineProps<{
  collapsed: boolean;
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  totalUnread: number;
}>();

const emit = defineEmits<{
  (e: "update:collapsed", v: boolean): void;
  (e: "open", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
}>();

const widthPx = ref(240);
const minWidth = 180;
const maxWidth = 480;
let dragOriginX = 0;
let dragOriginWidth = 0;
let dragging = false;

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

const railIcons = computed(() => {
  const all: RemoteSession[] = [];
  for (const list of Object.values(props.byHost)) all.push(...list);
  all.sort((a, b) => urgencyIndex(a.task_state) - urgencyIndex(b.task_state));
  return all.slice(0, 20);
});
</script>

<template>
  <aside
    class="task-sidebar"
    :class="{ collapsed }"
    :style="!collapsed ? { width: widthPx + 'px' } : undefined"
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
        @click="emit('update:collapsed', false)"
      >
        »
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
        <span class="title">{{ t("tasks.sidebar.title") }}</span>
        <button
          class="collapse-button"
          data-test="collapse-button"
          :title="t('tasks.sidebar.collapse')"
          @click="emit('update:collapsed', true)"
        >
          «
        </button>
      </header>
      <div class="list-wrap" data-test="task-grouped-list">
        <TaskGroupedList
          :by-host="byHost"
          :unread-by-host="unreadByHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          @open="(s) => emit('open', s)"
          @markSeen="(p) => emit('markSeen', p)"
        />
      </div>
      <footer v-if="totalUnread > 0">
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
</template>

<style scoped>
.task-sidebar {
  position: relative;
  background: var(--bg-elev, #0e1116);
  border-right: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  flex-direction: column;
  height: 100%;
}
.task-sidebar.collapsed { width: 32px; }
/* expanded width comes from inline :style="{ width: widthPx + 'px' }" */
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
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.title { flex: 1; font-weight: 500; }
.collapse-button,
.expand-button {
  background: none;
  border: none;
  cursor: pointer;
  color: inherit;
  font-size: 14px;
}
.list-wrap { flex: 1 1 auto; overflow: auto; padding: 4px; }
footer { padding: 8px; border-top: 1px solid rgba(255, 255, 255, 0.05); }
.mark-all {
  background: none;
  border: 1px solid rgba(255, 255, 255, 0.12);
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
</style>
