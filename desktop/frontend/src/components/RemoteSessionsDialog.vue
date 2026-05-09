<script lang="ts" setup>
import type { SessionInfo } from "../lib/connection";

const props = defineProps<{
  sessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "open", sessionId: string): void;
  (e: "close"): void;
}>();

function formatHost(s: SessionInfo): string {
  const u = s.user || "";
  const h = s.host || "";
  if (u && h) return `${u}@${h}`;
  return u || h || "unknown host";
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>remote sessions</h2>

      <div v-if="sessions.length === 0" class="empty">
        no remote sessions visible. start one in another atterm app connected
        to the same relay.
      </div>

      <div v-else class="grid">
        <div
          v-for="s in sessions"
          :key="s.id"
          class="card"
          @click="emit('open', s.id)"
        >
          <div class="host">
            <span class="who">{{ formatHost(s) }}</span>
            <span
              v-if="s.host_id"
              class="hostid"
              :title="'host_id ' + s.host_id"
            >{{ s.host_id.slice(0, 8) }}</span>
          </div>
          <div class="cmd">{{ s.command || "(unknown)" }}</div>
          <div class="meta">
            <span class="id">{{ s.id.slice(0, 8) }}</span>
            <span class="size">{{ s.cols }}×{{ s.rows }}</span>
            <span class="cwd">{{ s.cwd }}</span>
          </div>
        </div>
      </div>

      <div class="row">
        <button @click="emit('close')">close</button>
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
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
  overflow-y: auto;
  max-height: 50vh;
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
.card .host {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  margin-bottom: 4px;
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.card .host .who { color: #d29922; } /* amber, same as remote tab dots */
.card .host .hostid { color: var(--fg-dim); font-size: 11px; cursor: help; }
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
