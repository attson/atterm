<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { getUserHomeDir } from "../lib/api";
import { useTaskPreset } from "../composables/useTaskPreset";
import {
  aiTitleOrCommand,
  rowTitle,
  hostNameWithIndex,
  coResidentIndex,
  taskStateLabel,
} from "../lib/sessionLabel";

const preset = useTaskPreset();
const showStateLabel = computed(() => preset.active.value.showLabel);

const props = withDefaults(defineProps<{
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  groupBy?: "host" | "state";
  byState?: Record<string, RemoteSession[]>;
  unreadByState?: Record<string, number>;
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
}>(), {
  groupBy: "host",
  byState: () => ({}),
  unreadByState: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
});

const emit = defineEmits<{
  (e: "open", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
}>();

const { t } = useI18n();

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

const groups = computed<Record<string, RemoteSession[]>>(() =>
  props.groupBy === "state" ? props.byState : props.byHost,
);
const unreadByGroup = computed<Record<string, number>>(() =>
  props.groupBy === "state" ? props.unreadByState : props.unreadByHost,
);
const groupKeys = computed<string[]>(() => {
  if (props.groupBy === "state") {
    return STATE_ORDER.filter((s) => (groups.value[s] ?? []).length > 0);
  }
  // Pin the local host to the top so the user's own machine is the first
  // thing visible — the rest stays alphabetical for stability across
  // relay churn (a remote dropping in/out shouldn't reshuffle the list).
  const keys = Object.keys(groups.value).sort();
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

onMounted(async () => {
  try {
    home.value = await getUserHomeDir();
  } catch { /* leave empty — helper still truncates */ }
});

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
  return props.primaryStateForHost(key);
}

function unreadIdsForGroup(key: string): string[] {
  return (groups.value[key] ?? []).filter((s) => s.unread).map((s) => s.session_id);
}

function onMarkRead(s: RemoteSession) {
  emit("markSeen", { ids: [s.session_id] });
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
    <section
      v-for="key in groupKeys"
      :key="key"
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
        <span class="caret">{{ isGroupCollapsed(key) ? '▶' : '▼' }}</span>
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
          <span class="count">{{ (groups[key] ?? []).length }}</span>
        </span>
        <span v-if="unreadByGroup[key] > 0" class="unread-badge">
          {{ t("tasks.unreadBadge", { count: unreadByGroup[key] }) }}
        </span>
        <button
          v-if="unreadByGroup[key] > 0"
          class="mark-all"
          data-test="host-mark-all"
          :title="t('tasks.markAllRead')"
          @click.stop="onMarkGroup(key)"
        >
          ✓
        </button>
      </header>
      <button
        v-for="s in (isGroupCollapsed(key) ? [] : groups[key])"
        :key="s.session_id"
        class="task-row"
        :class="{ active: s.session_id === activeSessionId }"
        data-test="task-row"
        :data-active="s.session_id === activeSessionId ? 'true' : undefined"
        @click="emit('open', s)"
      >
        <span class="row-top">
          <TaskStateIcon
            :state="(s.task_state as TaskState | undefined) ?? 'idle'"
          />
          <span
            v-if="showStateLabel"
            class="state-label"
            data-test="state-label"
          >{{ stateLabel(s.task_state) }}</span>
          <span class="cmd-and-cwd" :title="rowTitle(s)">
            <span class="cmd">{{ aiTitleOrCommand(s) }}</span>
          </span>
          <span v-if="s.unread" class="unread-dot" data-test="unread-dot">●</span>
          <span
            v-if="s.unread"
            class="row-mark-read"
            data-test="row-mark-read"
            role="button"
            tabindex="0"
            :title="t('tasks.markRead')"
            :aria-label="t('tasks.markRead')"
            @click.stop="onMarkRead(s)"
            @keydown.enter.stop.prevent="onMarkRead(s)"
            @keydown.space.stop.prevent="onMarkRead(s)"
          >
            ✓
          </span>
        </span>
        <span v-if="shortenCwd(s.cwd, home)" class="cwd" data-test="row-cwd">{{ shortenCwd(s.cwd, home) }}</span>
      </button>
    </section>
    <section v-if="completedSeen.length > 0" class="completed-fold">
      <button
        class="fold-toggle"
        data-test="completed-fold-toggle"
        @click="foldOpen = !foldOpen"
      >
        {{ foldOpen ? "▼" : "▶" }} {{ t("tasks.completedFold") }} ·
        {{ completedSeen.length }}
      </button>
      <template v-if="foldOpen">
        <div
          v-for="s in completedSeen"
          :key="s.session_id"
          class="task-row dim"
          :class="{ active: s.session_id === activeSessionId }"
          data-test="completed-fold-row"
          :data-active="s.session_id === activeSessionId ? 'true' : undefined"
          @click="emit('open', s)"
        >
          <span class="row-top">
            <TaskStateIcon :state="(s.task_state as TaskState | undefined) ?? 'idle'" />
            <span
              v-if="showStateLabel"
              class="state-label"
              data-test="state-label"
            >{{ stateLabel(s.task_state) }}</span>
            <span class="cmd-and-cwd" :title="rowTitle(s)">
              <span class="cmd">{{ aiTitleOrCommand(s) }}</span>
            </span>
          </span>
          <span v-if="shortenCwd(s.cwd, home)" class="cwd">{{ shortenCwd(s.cwd, home) }}</span>
        </div>
        <button class="fold-mark-all" data-test="fold-mark-all" @click="onMarkFold">
          {{ t("tasks.markAllRead") }}
        </button>
      </template>
    </section>
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
.task-row { display: flex; flex-direction: column; gap: 1px; padding: 5px 8px; border: 1px solid rgba(255, 255, 255, 0.08); background: rgba(255, 255, 255, 0.02); width: 100%; text-align: left; cursor: pointer; color: inherit; border-radius: 6px; }
.task-row:hover { background: rgba(255, 255, 255, 0.06); border-color: rgba(255, 255, 255, 0.16); }
.task-row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); border-color: color-mix(in srgb, var(--accent) 28%, var(--border)); box-shadow: inset 2px 0 0 var(--accent); }
.task-row.active:hover { background: color-mix(in srgb, var(--accent) 14%, transparent); }
.task-row.dim { opacity: 0.6; }
.task-row.dim.active { opacity: 0.9; }
.row-top { display: flex; align-items: center; gap: 6px; min-width: 0; }
.state-label { font-size: 11px; opacity: 0.85; white-space: nowrap; flex-shrink: 0; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); flex: 1 1 auto; min-width: 0; }
.cwd { color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; padding-left: 18px; }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { font-size: 11px; padding: 0 4px; cursor: pointer; }
.completed-fold { border-top: 1px solid rgba(255, 255, 255, 0.06); margin-top: 6px; padding-top: 4px; display: flex; flex-direction: column; gap: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 4px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.7; }
.fold-mark-all { background: none; border: 1px solid rgba(255, 255, 255, 0.12); cursor: pointer; padding: 4px 8px; margin: 4px 6px; color: inherit; border-radius: 3px; }
</style>
