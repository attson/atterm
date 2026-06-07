<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { getUserHomeDir } from "../lib/api";

const props = defineProps<{
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
}>();

const emit = defineEmits<{
  (e: "open", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
}>();

const { t } = useI18n();

const hostKeys = computed(() => Object.keys(props.byHost).sort());
const foldOpen = ref(false);
const home = ref("");

onMounted(async () => {
  try {
    home.value = await getUserHomeDir();
  } catch { /* leave empty — helper still truncates */ }
});

function hostName(hostId: string): string {
  const first = props.byHost[hostId]?.[0];
  return first?.host || hostId || t("sessions.unknownHost");
}

function unreadIdsFor(hostId: string): string[] {
  return (props.byHost[hostId] ?? []).filter((s) => s.unread).map((s) => s.session_id);
}

function onMarkRead(s: RemoteSession) {
  emit("markSeen", { ids: [s.session_id] });
}
function onMarkHost(hostId: string) {
  emit("markSeen", { ids: unreadIdsFor(hostId) });
}
function onMarkFold() {
  emit("markSeen", { ids: props.completedSeen.map((s) => s.session_id) });
}

function fullCommand(s: { current_command?: string; title?: string; session_id: string }): string {
  return s.current_command || s.title || s.session_id.slice(0, 8);
}

// commandLabel is the SHORT row display: only the executable name
// (first whitespace-separated token, with any leading path stripped).
// `/usr/local/bin/claude --permission-mode bypassPermissions` → `claude`.
// The full string with args (and cwd) is exposed via the row's title
// tooltip so nothing is lost.
function commandLabel(s: { current_command?: string; title?: string; session_id: string }): string {
  const raw = fullCommand(s);
  const firstToken = raw.split(/\s+/)[0] || raw;
  return firstToken.split("/").pop() || firstToken;
}

function rowTitle(s: { cwd?: string; current_command?: string; title?: string; session_id: string }): string {
  const cmd = fullCommand(s);
  return s.cwd ? `${cmd}\n${s.cwd}` : cmd;
}
</script>

<template>
  <div class="task-grouped-list">
    <section
      v-for="hostId in hostKeys"
      :key="hostId"
      class="host-group"
      :data-test="`host-group-${hostId}`"
    >
      <header class="host-header" data-test="host-header">
        <span class="caret">▼</span>
        <span class="host-name">{{ hostName(hostId) }}</span>
        <span class="counts">
          <TaskStateIcon :state="primaryStateForHost(hostId)" :size="10" />
          <span class="count">{{ byHost[hostId].length }}</span>
        </span>
        <span v-if="unreadByHost[hostId] > 0" class="unread-badge">
          {{ t("tasks.unreadBadge", { count: unreadByHost[hostId] }) }}
        </span>
        <button
          v-if="unreadByHost[hostId] > 0"
          class="mark-all"
          data-test="host-mark-all"
          :title="t('tasks.markAllRead')"
          @click="onMarkHost(hostId)"
        >
          ✓
        </button>
      </header>
      <button
        v-for="s in byHost[hostId]"
        :key="s.session_id"
        class="task-row"
        data-test="task-row"
        @click="emit('open', s)"
      >
        <TaskStateIcon
          :state="(s.task_state as TaskState | undefined) ?? 'idle'"
        />
        <span class="cmd-and-cwd" :title="rowTitle(s)">
          <span class="cmd">{{ commandLabel(s) }}</span>
          <span v-if="shortenCwd(s.cwd, home)" class="cwd">·&nbsp;{{ shortenCwd(s.cwd, home) }}</span>
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
          <TaskStateIcon :state="(s.task_state as TaskState | undefined) ?? 'idle'" />
          <span class="cmd-and-cwd" :title="rowTitle(s)">
            <span class="cmd">{{ commandLabel(s) }}</span>
            <span v-if="shortenCwd(s.cwd, home)" class="cwd">·&nbsp;{{ shortenCwd(s.cwd, home) }}</span>
          </span>
        </div>
        <button class="fold-mark-all" data-test="fold-mark-all" @click="onMarkFold">
          {{ t("tasks.markAllRead") }}
        </button>
      </template>
    </section>
  </div>
</template>

<style scoped>
.task-grouped-list { display: flex; flex-direction: column; gap: 6px; font-size: 12px; }
.host-header { display: flex; align-items: center; gap: 6px; font-weight: 500; padding: 4px 6px; }
.host-name { flex: 0 0 auto; }
.counts { margin-left: auto; display: inline-flex; gap: 2px; align-items: center; }
.unread-badge { font-size: 10px; opacity: 0.8; background: rgba(255, 255, 255, 0.06); border-radius: 3px; padding: 1px 4px; }
.mark-all { background: none; border: none; cursor: pointer; padding: 0 4px; color: inherit; }
.task-row { display: flex; align-items: center; gap: 6px; padding: 3px 8px; border: none; background: none; width: 100%; text-align: left; cursor: pointer; color: inherit; border-radius: 3px; }
.task-row:hover { background: rgba(255, 255, 255, 0.05); }
.task-row.dim { opacity: 0.6; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); }
.cwd { color: var(--fg-dim); white-space: nowrap; flex-shrink: 1; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { font-size: 11px; padding: 0 4px; cursor: pointer; }
.completed-fold { border-top: 1px solid rgba(255, 255, 255, 0.06); margin-top: 6px; padding-top: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 4px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.7; }
.fold-mark-all { background: none; border: 1px solid rgba(255, 255, 255, 0.12); cursor: pointer; padding: 4px 8px; margin: 4px 6px; color: inherit; border-radius: 3px; }
</style>
