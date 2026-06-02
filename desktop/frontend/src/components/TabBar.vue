<script lang="ts" setup>
import type { SessionInfo } from "../lib/connection";
import type { Tab } from "../lib/types";
import { useI18n } from "../i18n/useI18n";

interface TabSummary {
  id: string;
  layout: Tab["layout"];
  activeSession: SessionInfo | null;
  activeRemote: boolean;
  paneCount: number;
  disconnected?: boolean;
}

defineProps<{
  tabs: TabSummary[];
  currentId: string | null;
  starting: boolean;
}>();

const emit = defineEmits<{
  (e: "activate", id: string): void;
  (e: "close", id: string): void;
  (e: "new"): void;
}>();

const { t: i18nT } = useI18n();

function shortTitle(s: SessionInfo | null): string {
  if (!s) return i18nT("terminal.emptyTab");
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped === "") return "/";
    const base = stripped.split("/").pop();
    if (base) return base;
  }
  const first = (s.command || "").split(/\s+/)[0] || i18nT("terminal.shellFallback");
  return first.split("/").pop() || first;
}

function layoutLabel(t: TabSummary): string {
  switch (t.layout) {
    case "single": return "";
    case "vertical": return "▮▮";
    case "horizontal": return "▬\n▬";
    case "grid2x2": return "▦";
  }
}

function layoutTitle(t: TabSummary): string {
  switch (t.layout) {
    case "single": return "";
    case "vertical": return i18nT("terminal.layoutVertical");
    case "horizontal": return i18nT("terminal.layoutHorizontal");
    case "grid2x2": return i18nT("terminal.layoutGrid2x2");
  }
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
        v-for="(t, idx) in tabs"
        :key="t.id"
        class="tab"
        :class="{ active: t.id === currentId, remote: t.activeRemote, disconnected: t.disconnected }"
        :title="(t.activeRemote ? i18nT('terminal.remotePrefix') : '') + (t.disconnected ? i18nT('terminal.tabDisconnectedSuffix') + ' ' : '') + (t.activeSession?.command ?? '')"
        @click="emit('activate', t.id)"
      >
        <span class="num">{{ idx + 1 }}:</span>
        <span v-if="t.layout !== 'single'" class="layout-icon" :title="layoutTitle(t)">{{ layoutLabel(t) }}</span>
        <span v-else-if="t.activeRemote" class="dot remote-dot" :class="{ disconnected: t.disconnected }">●</span>
        <span v-else class="dot">●</span>
        <span class="title">{{ shortTitle(t.activeSession) }}</span>
        <button class="close" @click="onClose($event, t.id)">×</button>
      </div>
    </div>
    <button
      class="plus"
      :disabled="starting"
      :title="starting ? i18nT('terminal.starting') : i18nT('terminal.newTab')"
      @click="emit('new')"
    >+</button>
  </div>
</template>

<style scoped>
.tabbar {
  display: flex; align-items: stretch; background: var(--panel);
  border-bottom: 1px solid var(--border); flex: 0 0 auto; height: 34px;
  overflow: hidden;
}
.tabs {
  display: flex; flex: 1 1 auto; overflow-x: auto;
  scrollbar-width: none;
  -ms-overflow-style: none;
}
.tabs::-webkit-scrollbar { display: none; }

.tab {
  display: flex; align-items: center; gap: 6px; padding: 0 8px 0 12px;
  border-right: 1px solid var(--border); font-size: 12px;
  color: var(--fg-dim); cursor: pointer; user-select: none;
  white-space: nowrap; min-width: 110px; max-width: 220px;
  transition: background 120ms;
}
.tab:hover { background: rgba(255, 255, 255, 0.04); }
.tab.active {
  background: var(--bg); color: var(--fg);
  box-shadow: inset 0 -2px 0 var(--accent);
}
.tab .num {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px; color: var(--fg-dim);
}
.tab.active .num { color: var(--accent); }
.tab .dot { font-size: 9px; color: var(--good); }
.tab .remote-dot { color: #d29922; }
.tab .remote-dot.disconnected { color: var(--fg-dim); }
.tab.disconnected .title { color: var(--fg-dim); font-style: italic; }
.tab .layout-icon {
  font-size: 11px; color: var(--fg-dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  letter-spacing: -1px;
}
.tab .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  flex: 1 1 auto; overflow: hidden; text-overflow: ellipsis;
}
.tab .close {
  border: none; background: transparent; padding: 0 4px; font-size: 14px;
  line-height: 1; color: var(--fg-dim); border-radius: 4px; opacity: 0;
  transition: opacity 120ms, background 120ms, color 120ms;
}
.tab:hover .close, .tab.active .close { opacity: 1; }
.tab .close:hover { background: rgba(248, 81, 73, 0.18); color: var(--bad); }
.plus {
  border: none; background: transparent; color: var(--fg-dim);
  font-size: 18px; line-height: 1; padding: 0 14px; cursor: pointer;
  border-left: 1px solid var(--border); transition: color 120ms, background 120ms;
}
.plus:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.plus:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
