<script setup lang="ts">
// Shared inner markup for a `.task-row` button — the state icon, optional
// state label, command + cwd tooltip, unread affordances, and the cwd
// line. Extracted so the host/state groups and the pinned virtual group
// (TaskGroupedList.vue) render byte-identical rows instead of drifting.
//
// NOTE: the completed-fold row in TaskGroupedList.vue intentionally does
// NOT use this component — it's a reduced view (no unread dot / mark-read
// affordance) since folded sessions are already seen.
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { aiTitleOrCommand, rowTitle, taskStateLabel } from "../lib/sessionLabel";

const props = withDefaults(defineProps<{
  session: RemoteSession;
  showStateLabel?: boolean;
  home?: string;
}>(), {
  showStateLabel: false,
  home: "",
});

const emit = defineEmits<{
  (e: "markRead"): void;
}>();

const { t } = useI18n();

function stateLabel(state: string | undefined): string {
  return taskStateLabel(state, t);
}
</script>

<template>
  <span class="row-top">
    <TaskStateIcon
      :state="(props.session.task_state as TaskState | undefined) ?? 'idle'"
    />
    <span
      v-if="showStateLabel"
      class="state-label"
      data-test="state-label"
    >{{ stateLabel(props.session.task_state) }}</span>
    <span class="cmd-and-cwd" :title="rowTitle(props.session)">
      <span class="cmd">{{ aiTitleOrCommand(props.session) }}</span>
    </span>
    <span v-if="props.session.unread" class="unread-dot" data-test="unread-dot">●</span>
    <span
      v-if="props.session.unread"
      class="row-mark-read"
      data-test="row-mark-read"
      role="button"
      tabindex="0"
      :title="t('tasks.markRead')"
      :aria-label="t('tasks.markRead')"
      @click.stop="emit('markRead')"
      @keydown.enter.stop.prevent="emit('markRead')"
      @keydown.space.stop.prevent="emit('markRead')"
    >
      ✓
    </span>
  </span>
  <span
    v-if="shortenCwd(props.session.cwd, home)"
    class="cwd"
    data-test="row-cwd"
  >{{ shortenCwd(props.session.cwd, home) }}</span>
</template>

<style scoped>
.row-top { display: flex; align-items: center; gap: 6px; min-width: 0; }
.state-label { font-size: 11px; opacity: 0.85; white-space: nowrap; flex-shrink: 0; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); flex: 1 1 auto; min-width: 0; }
.cwd { color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; padding-left: 18px; }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { font-size: 11px; padding: 0 4px; cursor: pointer; }
</style>
