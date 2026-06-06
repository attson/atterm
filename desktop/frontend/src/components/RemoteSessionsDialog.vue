<script lang="ts" setup>
import { ref, computed } from "vue";

import type { SessionInfo } from "../lib/connection";
import type { RemoteSession } from "../platform/types";
import TaskGroupedList from "./TaskGroupedList.vue";
import { useSessions } from "../composables/useSessions";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  sessions: SessionInfo[];
  open?: boolean;
}>();

const emit = defineEmits<{
  (e: "open", sessionId: string): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "close"): void;
}>();

const { t } = useI18n();

// Adapt SessionInfo (lib/connection.ts uses `id`) → RemoteSession (platform/types.ts uses `session_id`).
// All other field names already match between the two types.
function adapt(s: SessionInfo): RemoteSession {
  return {
    ...s,
    session_id: s.id,
  } as unknown as RemoteSession;
}

const remoteList = computed<RemoteSession[]>(() => props.sessions.map(adapt));
const emptyLocal = ref<RemoteSession[]>([]);
const { byHost, unreadByHost, primaryStateForHost, completedSeen, totalUnread } =
  useSessions(emptyLocal, remoteList);

function onListOpen(s: RemoteSession) {
  emit("open", s.session_id);
}
function onListMarkSeen(payload: { ids: string[] } | { all: true }) {
  emit("markSeen", payload);
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <header class="dialog-head">
        <h2>{{ t("sessions.remoteSessions") }}</h2>
        <button
          v-if="totalUnread > 0"
          class="mark-all"
          data-test="dialog-mark-all"
          @click="emit('markSeen', { all: true })"
        >
          {{ t("tasks.markAllRead") }}
        </button>
      </header>

      <div v-if="sessions.length === 0" class="empty">
        {{ t("sessions.noRemoteSessions") }}
      </div>

      <div v-else class="groups">
        <TaskGroupedList
          :by-host="byHost"
          :unread-by-host="unreadByHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          @open="onListOpen"
          @markSeen="onListMarkSeen"
        />
      </div>

      <div class="row">
        <button @click="emit('close')">{{ t("common.close") }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.dialog {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 720px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dialog-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dialog-head h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
  flex: 1;
}
.mark-all {
  background: none;
  border: 1px solid rgba(255, 255, 255, 0.12);
  cursor: pointer;
  padding: 2px 8px;
  color: inherit;
  font-size: 12px;
  border-radius: 3px;
}
.empty {
  color: var(--fg-dim);
  font-size: 13px;
  text-align: center;
  padding: 40px 0;
}
.groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
  overflow-y: auto;
  max-height: 50vh;
}
.row {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}
</style>
