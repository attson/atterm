<script setup lang="ts">
// Shared inner markup for a `.task-row` button — the state icon, optional
// state label, command + cwd tooltip, the cwd line, and the right-hand tail
// (kind badge + last output). Extracted so the host/state groups and the
// pinned virtual group (TaskGroupedList.vue) render byte-identical rows
// instead of drifting.
//
// There is no per-row unread marker or mark-read control. Unread already
// surfaces three other ways — the state icon turns into a star for the two
// attention states, unread rows sort to the top of their group, and the group
// header carries the count plus a "mark all read". A fourth, per-row copy of
// the same fact cost ~22px of a ~224px row's title. Unread also clears itself
// the moment a client attaches (relay: attention_at > seen_at AND no
// subscriber), so the manual per-row dismissal was only ever a shortcut.
//
// NOTE: the completed-fold row in TaskGroupedList.vue intentionally does
// NOT use this component — it's a reduced view since folded sessions are
// already seen.
import { computed } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import LastOutputIndicator from "./LastOutputIndicator.vue";
import { useI18n } from "../i18n/useI18n";
import { shortenCwd } from "../lib/shortenCwd";
import { formatLastOutput } from "../lib/lastOutput";
import { titleOrCommand, rowTitle, taskStateLabel, commandLabel } from "../lib/sessionLabel";

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
  (e: "close"): void;
}>();

const { t } = useI18n();
const cwd = computed(() => shortenCwd(props.session.cwd, props.home));
const hasLastOutput = computed(() =>
  formatLastOutput(props.session.last_output_at, props.session.task_state, props.nowMs) !== null,
);
// Same rule as the desk widget's row: name the CLI only for sessions we
// classified as ai, so an ordinary shell does not grow a "zsh" badge.
const kind = computed(() =>
  props.session.type === "ai" ? commandLabel(props.session) : "",
);

function stateLabel(state: string | undefined): string {
  return taskStateLabel(state, t);
}
</script>

<template>
  <span class="row-main">
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
  </span>
  <span v-if="cwd" class="row-meta" data-test="row-meta">
    <span class="cwd" data-test="row-cwd">{{ cwd }}</span>
  </span>
  </span>
  <!-- Right-hand tail, same shape as the desk widget's row: kind badge on the
       title line, last-output on the cwd line, both right-aligned. It renders
       unconditionally and reserves a minimum width so a session with neither a
       kind nor a timestamp still holds the column open and every row's right
       edge lines up. min-width rather than a fixed width: a fixed one would
       take space from an already-truncating title on rows whose tail is
       narrower than the widest case. -->
  <span class="row-tail" :class="{ 'time-only': !kind }" data-test="row-tail">
    <span v-if="kind" class="kind" data-test="row-kind">{{ kind }}</span>
    <LastOutputIndicator
      :last-output-at="props.session.last_output_at"
      :task-state="props.session.task_state"
      :now-ms="props.nowMs"
    />
  </span>
  <!-- The close × takes over the kind badge's slot while the row is pointed at
       (see TaskGroupedList's hover rules) rather than claiming a column of its
       own — the same one-slot-two-glyphs trade the unread dot makes above. -->
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
</template>

<style scoped>
.row-main { flex: 1 1 auto; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.row-top { display: flex; align-items: center; gap: 6px; min-width: 0; }
.state-label { font-size: 11px; opacity: 0.85; white-space: nowrap; flex-shrink: 0; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); flex: 1 1 auto; min-width: 0; }
.row-meta { display: flex; align-items: center; gap: 6px; min-width: 0; padding-left: 18px; }
.cwd { color: var(--fg-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; flex: 1 1 auto; min-width: 0; }

/* Tail column. `align-self: stretch` + space-between is what pins the kind
   badge to the title line and the timestamp to the cwd line without either
   knowing the other's height. */
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
/* No badge to pin to the top, so the timestamp stays on the cwd line instead
   of floating up to the title. */
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
/* Absolute so it can sit *on* the kind badge's slot: the badge fades out
   whenever × fades in, so the two never occupy the row at the same time and
   the layout never reflows. */
.row-close {
  position: absolute;
  top: 4px;
  right: 6px;
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
