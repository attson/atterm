<script lang="ts" setup>
import type { SessionInfo } from "../lib/connection";

interface TabSession extends SessionInfo {
  remote?: boolean;
}

const props = defineProps<{
  sessions: TabSession[];
  currentId: string | null;
  starting: boolean;
}>();

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close", id: string): void;
  (e: "new"): void;
}>();

function shortTitle(s: SessionInfo): string {
  // Prefer the cwd's basename so users can tell sessions apart at a glance —
  // the working directory is more meaningful than "bash" repeated five times.
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped === "") return "/";
    const base = stripped.split("/").pop();
    if (base) return base;
  }
  // Fall back to the binary name when no cwd is reported (e.g. non-Linux,
  // or before the first /proc readlink).
  const first = (s.command || "").split(/\s+/)[0] || "shell";
  return first.split("/").pop() || first;
}

function onClose(e: MouseEvent, id: string) {
  e.stopPropagation();
  emit("close", id);
}
</script>

<template>
  <div class="tabbar">
    <div class="tabs">
      <div
        v-for="(s, idx) in sessions"
        :key="s.id"
        class="tab"
        :class="{ active: s.id === currentId, remote: s.remote }"
        :title="(s.remote ? '[remote] ' : '') + s.command + (s.host ? '\n' + (s.user || '') + '@' + s.host : '')"
        @click="emit('activate', s.id)"
      >
        <span class="num">{{ idx + 1 }}:</span>
        <span v-if="s.remote" class="icon remote-icon" aria-label="remote session">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="12" height="12"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M2 16.1A5 5 0 0 1 5.9 20" />
            <path d="M2 12.05A9 9 0 0 1 9.95 20" />
            <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
            <line x1="2" y1="20" x2="2.01" y2="20" />
          </svg>
        </span>
        <span v-else class="dot">●</span>
        <span class="title">{{ shortTitle(s) }}</span>
        <button class="close" @click="onClose($event, s.id)">×</button>
      </div>
    </div>
    <button
      class="plus"
      :disabled="starting"
      :title="starting ? 'starting…' : 'new session'"
      @click="emit('new')"
    >+</button>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex;
  align-items: stretch;
  background: var(--panel);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
  height: 34px;
  overflow: hidden;
}
.tabs {
  display: flex;
  flex: 1 1 auto;
  overflow-x: auto;
  scrollbar-width: thin;
}
.tabs::-webkit-scrollbar { height: 4px; }
.tabs::-webkit-scrollbar-thumb { background: var(--border); }

.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 8px 0 12px;
  border-right: 1px solid var(--border);
  font-size: 12px;
  color: var(--fg-dim);
  cursor: pointer;
  user-select: none;
  white-space: nowrap;
  min-width: 110px;
  max-width: 220px;
  transition: background 120ms;
}
.tab:hover { background: rgba(255, 255, 255, 0.04); }
.tab.active {
  background: var(--bg);
  color: var(--fg);
  box-shadow: inset 0 -2px 0 var(--accent);
}
.tab .num {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  color: var(--fg-dim);
}
.tab.active .num { color: var(--accent); }
.tab .dot {
  font-size: 9px;
  color: var(--good);
}
.tab .remote-icon {
  display: inline-flex;
  align-items: center;
  color: #d29922; /* amber to mirror the topbar cast button's badge */
}
.tab .remote-icon svg { display: block; }
.tab .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tab .close {
  border: none;
  background: transparent;
  padding: 0 4px;
  font-size: 14px;
  line-height: 1;
  color: var(--fg-dim);
  border-radius: 4px;
  opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.tab:hover .close,
.tab.active .close { opacity: 1; }
.tab .close:hover {
  background: rgba(248, 81, 73, 0.18);
  color: var(--bad);
}

.plus {
  border: none;
  background: transparent;
  color: var(--fg-dim);
  font-size: 18px;
  line-height: 1;
  padding: 0 14px;
  cursor: pointer;
  border-left: 1px solid var(--border);
  transition: color 120ms, background 120ms;
}
.plus:hover:not(:disabled) {
  color: var(--accent);
  background: rgba(88, 166, 255, 0.08);
}
.plus:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
