<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { getUserHomeDir } from "../lib/api";
import { useTaskPreset } from "../composables/useTaskPreset";
import { aiTitleOrCommand, rowTitle, hostName as hostNameHelper, taskStateLabel } from "../lib/sessionLabel";

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
}>(), {
  groupBy: "host",
  byState: () => ({}),
  unreadByState: () => ({}),
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
  return Object.keys(groups.value).sort();
});
const foldOpen = ref(false);
const home = ref("");

onMounted(async () => {
  try {
    home.value = await getUserHomeDir();
  } catch { /* leave empty — helper still truncates */ }
});

function hostName(hostId: string): string {
  return hostNameHelper(hostId, groups.value[hostId], t("sessions.unknownHost"));
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
      <header class="host-header" data-test="host-header">
        <span class="caret">▼</span>
        <span class="host-name">{{ groupHeader(key) }}</span>
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
          @click="onMarkGroup(key)"
        >
          ✓
        </button>
      </header>
      <button
        v-for="s in groups[key]"
        :key="s.session_id"
        class="task-row"
        data-test="task-row"
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
          data-test="completed-fold-row"
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
.host-header { display: flex; align-items: center; gap: 6px; font-weight: 500; padding: 4px 6px; }
.host-name { flex: 0 0 auto; }
.counts { margin-left: auto; display: inline-flex; gap: 2px; align-items: center; }
.unread-badge { font-size: 10px; opacity: 0.8; background: rgba(255, 255, 255, 0.06); border-radius: 3px; padding: 1px 4px; }
.mark-all { background: none; border: none; cursor: pointer; padding: 0 4px; color: inherit; }
.host-group { display: flex; flex-direction: column; gap: 4px; }
.task-row { display: flex; flex-direction: column; gap: 1px; padding: 5px 8px; border: 1px solid rgba(255, 255, 255, 0.08); background: rgba(255, 255, 255, 0.02); width: 100%; text-align: left; cursor: pointer; color: inherit; border-radius: 6px; }
.task-row:hover { background: rgba(255, 255, 255, 0.06); border-color: rgba(255, 255, 255, 0.16); }
.task-row.dim { opacity: 0.6; }
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
