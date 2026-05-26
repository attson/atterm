<script lang="ts" setup>
import { computed } from "vue";

import type { SessionInfo } from "../lib/connection";
import { groupSessionsByHost } from "../lib/sessions";
import { useI18n } from "../i18n/useI18n";

const props = defineProps<{
  sessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "open", sessionId: string): void;
  (e: "close"): void;
}>();

const groups = computed(() => groupSessionsByHost(props.sessions));
const { t } = useI18n();
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>{{ t("sessions.remoteSessions") }}</h2>

      <div v-if="sessions.length === 0" class="empty">
        {{ t("sessions.noRemoteSessions") }}
      </div>

      <div v-else class="groups">
        <section v-for="g in groups" :key="g.key" class="host-group">
          <header>
            <span class="hostname">{{ g.hostname }}</span>
            <span
              v-if="g.hostId"
              class="hostid"
              :title="t('sessions.hostIdTitle', { hostId: g.hostId })"
            >{{ g.hostId.slice(0, 8) }}</span>
            <span class="count">{{ g.sessions.length === 1 ? t("common.countSessionsOne") : t("common.countSessions", { count: g.sessions.length }) }}</span>
          </header>
          <div class="grid">
            <div
              v-for="s in g.sessions"
              :key="s.id"
              class="card"
              @click="emit('open', s.id)"
            >
              <div class="cmd">{{ s.command || t("common.unknown") }}</div>
              <div class="meta">
                <span class="id">{{ s.id.slice(0, 8) }}</span>
                <span class="size">{{ s.cols }}×{{ s.rows }}</span>
                <span class="cwd">{{ s.cwd }}</span>
              </div>
            </div>
          </div>
        </section>
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
.dialog h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
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
.host-group > header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  padding: 4px 0 6px;
}
.host-group > header .hostname { color: var(--fg); }
.host-group > header .hostid { color: var(--fg-dim); font-size: 11px; cursor: help; }
.host-group > header .count { color: var(--fg-dim); font-size: 11px; margin-left: auto; }
.host-group > .grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}
.card {
  background: #0d1117;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  cursor: pointer;
  transition: border-color 120ms;
}
.card:hover { border-color: var(--accent); }
.card .cmd {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  color: var(--fg);
  margin-bottom: 4px;
  word-break: break-all;
}
.card .meta {
  font-size: 11px;
  color: var(--fg-dim);
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.card .meta .id { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.card .meta .cwd { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.row {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}
</style>
