<script setup lang="ts">
// Shared inner markup for a `.task-row` button — the state icon, optional
// state label, command + cwd tooltip, unread affordances, and the cwd
// line. Extracted so the host/state groups and the pinned virtual group
// (TaskGroupedList.vue) render byte-identical rows instead of drifting.
//
// NOTE: the completed-fold row in TaskGroupedList.vue intentionally does
// NOT use this component — it's a reduced view (no unread dot / mark-read
// affordance) since folded sessions are already seen.
import { computed } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import LastOutputIndicator from "./LastOutputIndicator.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { formatLastOutput } from "../lib/lastOutput";
import { titleOrCommand, rowTitle, taskStateLabel } from "../lib/sessionLabel";

const props = withDefaults(defineProps<{
  session: RemoteSession;
  showStateLabel?: boolean;
  home?: string;
  showClose?: boolean;
  nowMs: number;
}>(), {
  showStateLabel: false,
  home: "",
  showClose: false,
});

const emit = defineEmits<{
  (e: "markRead"): void;
  (e: "close"): void;
}>();

const { t } = useI18n();
const cwd = computed(() => shortenCwd(props.session.cwd, props.home));
const hasLastOutput = computed(() =>
  formatLastOutput(props.session.last_output_at, props.session.task_state, props.nowMs) !== null,
);

function stateLabel(state: string | undefined): string {
  return taskStateLabel(state, t);
}
</script>

<template>
  <span class="row-top">
    <TaskStateIcon
      :state="(props.session.task_state as TaskState | undefined) ?? 'idle'"
      :unread="props.session.unread === true"
    />
    <span
      v-if="showStateLabel"
      class="state-label"
      data-test="state-label"
    >{{ stateLabel(props.session.task_state) }}</span>
    <span class="cmd-and-cwd" :title="rowTitle(props.session)">
      <span class="cmd">{{ titleOrCommand(props.session) }}</span>
    </span>
    <!-- Unread dot and "mark read" are one control: the dot IS the button,
         and it swaps to a check on hover/focus. Two separate affordances ate
         ~45px of a ~224px row and left the title barely half the width. -->
    <span
      v-if="props.session.unread && props.session.task_state !== 'waiting_input' && props.session.task_state !== 'completed'"
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
      <span class="unread-dot" data-test="unread-dot" aria-hidden="true">●</span>
      <!-- ✓ (U+2713) renders as .notdef on iOS 26.3. -->
      <svg class="read-check" width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M3 8 l3 3 l7 -7" />
      </svg>
    </span>
    <span
      v-if="props.showClose"
      class="row-close"
      data-test="row-close"
      role="button"
      tabindex="0"
      :title="t('common.close')"
      :aria-label="t('common.close')"
      @click.stop="emit('close')"
      @keydown.enter.stop.prevent="emit('close')"
      @keydown.space.stop.prevent="emit('close')"
    >
      ×
    </span>
  </span>
  <span v-if="cwd || hasLastOutput" class="row-meta" data-test="row-meta">
    <span v-if="cwd" class="cwd" data-test="row-cwd">{{ cwd }}</span>
    <LastOutputIndicator
      :last-output-at="props.session.last_output_at"
      :task-state="props.session.task_state"
      :now-ms="props.nowMs"
    />
  </span>
</template>

<style scoped>
.row-top { display: flex; align-items: center; gap: 6px; min-width: 0; }
.state-label { font-size: 11px; opacity: 0.85; white-space: nowrap; flex-shrink: 0; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); flex: 1 1 auto; min-width: 0; }
.row-meta { display: flex; align-items: center; gap: 6px; min-width: 0; padding-left: 18px; }
.cwd { color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; flex: 1 1 auto; min-width: 0; }
.row-meta :deep(.last-output) { margin-left: auto; opacity: 0.65; }
/* One 16px slot holds either glyph, so swapping them never reflows the row. */
.row-mark-read {
  flex: none;
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px;
  cursor: pointer; border-radius: 4px; color: currentColor;
}
.unread-dot { font-size: 9px; line-height: 1; }
.read-check { display: none; }
.row-mark-read:hover, .row-mark-read:focus-visible { background: rgba(255, 255, 255, 0.12); outline: none; }
.row-mark-read:hover .unread-dot, .row-mark-read:focus-visible .unread-dot { display: none; }
.row-mark-read:hover .read-check, .row-mark-read:focus-visible .read-check { display: block; }

.row-close {
  flex: none;
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px;
  font-size: 13px; line-height: 1;
  cursor: pointer;
  color: var(--fg-dim);
  border-radius: 4px;
}
.row-close:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}
</style>
