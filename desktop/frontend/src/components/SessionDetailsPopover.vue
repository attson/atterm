<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { RemoteSession } from "../platform/types";
import { useI18n } from "../i18n/useI18n";
import { useSessionPins } from "../composables/useSessionPins";
import { taskStateLabel } from "../lib/sessionLabel";

const props = withDefaults(defineProps<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
  paneLocation: { tabId: string; paneIdx: number } | null;
  tabIndexById: (tabId: string) => number;
}>(), { open: false });

const emit = defineEmits<{ (e: "close"): void }>();

const { t } = useI18n();
const pins = useSessionPins();
const popoverRef = ref<HTMLElement | null>(null);
const positionedX = ref(0);
const positionedY = ref(0);

function updatePosition() {
  if (!popoverRef.value) {
    positionedX.value = props.x;
    positionedY.value = props.y;
    return;
  }
  const rect = popoverRef.value.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  positionedX.value = props.x + rect.width > vw ? Math.max(0, props.x - rect.width) : props.x;
  positionedY.value = props.y + rect.height > vh ? Math.max(0, props.y - rect.height) : props.y;
}

const style = computed(() => ({
  left: positionedX.value + "px",
  top: positionedY.value + "px",
}));

// Compact ISO-ish local string; keep the seconds precision the wire format
// carries. `started_at` etc are Unix seconds.
function fmtTs(sec: number | undefined): string {
  if (!sec) return "";
  const d = new Date(sec * 1000);
  return d.toISOString().replace("T", " ").replace(/\.\d+Z$/, "Z");
}

function fmtDuration(ms: number | undefined): string {
  if (ms === undefined || ms === null) return "";
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rem = Math.round(s - m * 60);
  return `${m}m${rem}s`;
}

async function copy(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    /* clipboard unavailable — silently ignore; user can select-and-copy */
  }
}

// Compose the fields with `v-if` guards so an empty value collapses the row.
const rows = computed(() => {
  const s = props.session;
  if (!s) return [];
  const list: Array<{
    key: string;
    label: string;
    value: string;
    copy?: boolean;
  }> = [];
  list.push({ key: "sessionId", label: t("tasks.details.sessionId"), value: s.session_id, copy: true });
  if (s.type) list.push({ key: "type", label: t("tasks.details.type"), value: s.type });
  const hostLine = [s.user, s.host].filter(Boolean).join("@");
  if (hostLine || s.host_id) {
    list.push({
      key: "host",
      label: t("tasks.details.host"),
      value: hostLine || s.host_id,
      copy: true,
    });
  }
  if (s.cwd) list.push({ key: "cwd", label: t("tasks.details.cwd"), value: s.cwd, copy: true });
  const cmd = s.current_command || s.title;
  if (cmd) list.push({ key: "command", label: t("tasks.details.command"), value: cmd, copy: true });
  if (s.task_state) list.push({ key: "state", label: t("tasks.details.state"), value: taskStateLabel(s.task_state, t) });
  if (s.started_at) list.push({ key: "startedAt", label: t("tasks.details.startedAt"), value: fmtTs(s.started_at) });
  if (s.command_started_at) list.push({ key: "commandStartedAt", label: t("tasks.details.commandStartedAt"), value: fmtTs(s.command_started_at) });
  if (s.command_ended_at) list.push({ key: "commandEndedAt", label: t("tasks.details.commandEndedAt"), value: fmtTs(s.command_ended_at) });
  const dur = fmtDuration(s.command_duration_ms);
  if (dur) list.push({ key: "commandDuration", label: t("tasks.details.commandDuration"), value: dur });
  if (typeof s.command_exit_code === "number") list.push({ key: "commandExitCode", label: t("tasks.details.commandExitCode"), value: String(s.command_exit_code) });
  list.push({ key: "unread", label: t("tasks.details.unread"), value: s.unread ? t("common.yes") : t("common.no") });
  list.push({ key: "pinned", label: t("tasks.details.pinned"), value: pins.isPinned(s.session_id) ? t("common.yes") : t("common.no") });
  const loc = props.paneLocation;
  const paneLabel = loc
    ? t("tasks.details.paneAt", {
        tab: props.tabIndexById(loc.tabId),
        pane: loc.paneIdx + 1,
      })
    : t("tasks.details.paneNone");
  list.push({ key: "paneLocation", label: t("tasks.details.paneLocation"), value: paneLabel });
  return list;
});

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}

function onOutside(e: MouseEvent) {
  if (!popoverRef.value) return;
  if (!popoverRef.value.contains(e.target as Node)) emit("close");
}

function onFocusOut(e: FocusEvent) {
  if (!popoverRef.value) return;
  const related = e.relatedTarget as Node | null;
  if (!related || !popoverRef.value.contains(related)) emit("close");
}

watch(
  () => props.open,
  (v) => {
    if (v) {
      positionedX.value = props.x;
      positionedY.value = props.y;
      requestAnimationFrame(updatePosition);
      window.addEventListener("keydown", onKeydown);
      window.addEventListener("mousedown", onOutside);
    } else {
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("mousedown", onOutside);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (props.open) requestAnimationFrame(updatePosition);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("mousedown", onOutside);
});
</script>

<template>
  <div
    v-if="open && session"
    ref="popoverRef"
    class="session-details-popover"
    data-test="session-details-popover"
    role="dialog"
    :aria-label="t('tasks.details.title')"
    tabindex="-1"
    :style="style"
    @focusout.capture="onFocusOut"
    @contextmenu.prevent
  >
    <div class="popover-title">{{ t("tasks.details.title") }}</div>
    <div class="rows">
      <div
        v-for="row in rows"
        :key="row.key"
        class="row"
        :data-test="`details-field-${row.key}`"
      >
        <span class="label">{{ row.label }}</span>
        <span class="value" :title="row.value">{{ row.value }}</span>
        <button
          v-if="row.copy"
          class="copy"
          type="button"
          :title="t('common.copy')"
          :aria-label="t('common.copy')"
          @click.stop="copy(row.value)"
        >⧉</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.session-details-popover {
  position: fixed;
  z-index: 1000;
  min-width: 260px;
  max-width: 420px;
  padding: 8px 10px 10px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
  font-size: 12px;
}
.popover-title {
  font-weight: 600;
  padding-bottom: 6px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  margin-bottom: 6px;
}
.rows {
  display: flex;
  flex-direction: column;
  gap: 3px;
  max-height: 380px;
  overflow-y: auto;
}
.row {
  display: grid;
  grid-template-columns: 96px 1fr auto;
  gap: 8px;
  align-items: baseline;
}
.label {
  color: var(--fg-dim);
  white-space: nowrap;
}
.value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  min-width: 0;
}
.copy {
  border: none;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  padding: 0 3px;
  font-size: 12px;
  border-radius: 3px;
}
.copy:hover {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.08);
}
</style>
