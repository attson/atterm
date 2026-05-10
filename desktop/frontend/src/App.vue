<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import TabBar from "./components/TabBar.vue";
import PaneGrid from "./components/PaneGrid.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import RemoteSessionsDialog from "./components/RemoteSessionsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import {
  closeSession,
  getEndpoint,
  getHostInfo,
  getRelayConfig,
  listShells,
  newSession,
} from "./lib/api";
import type { Endpoint } from "./lib/api";
import { fetchSessions, type SessionInfo } from "./lib/connection";
import type { Pane, Tab, SplitDir } from "./lib/types";
import { closePane, focusNeighbor, transitionLayout } from "./lib/layout";
import { useTerminalShortcuts, type SplitMode } from "./composables/useTerminalShortcuts";

const localEndpoint = ref<Endpoint | null>(null);
const remoteEndpoint = ref<Endpoint | null>(null);
const localHostID = ref<string>("");

const localList = ref<SessionInfo[]>([]);
const remoteList = ref<SessionInfo[]>([]);

const tabs = ref<Tab[]>([]);
const currentTabId = ref<string | null>(null);

const status = ref<"loading" | "ready" | "error">("loading");
const errorMsg = ref<string>("");
const starting = ref(false);
const showSettings = ref(false);
const showRemote = ref(false);
const toast = ref<string>("");

// Picker state. When non-null, dialog is open and the resolved pick will go
// into tabs[*].panes[paneIdx] of the indicated tab (always the current tab).
const pickerCtx = ref<{ tabId: string; paneIdx: number } | null>(null);

let autoStarted = false;
let pollHandle: number | null = null;
let toastHandle: number | null = null;

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : "tab-" + Math.random().toString(36).slice(2);

const currentTab = computed<Tab | null>(
  () => tabs.value.find((t) => t.id === currentTabId.value) ?? null,
);

// Sessions visible across all current tabs (drives sweep + remote-discover panel).
const allUsedSessionIds = computed(() => {
  const s = new Set<string>();
  for (const t of tabs.value) for (const p of t.panes) if (p.sessionId) s.add(p.sessionId);
  return s;
});

const availableRemote = computed<SessionInfo[]>(() =>
  remoteList.value.filter((r) => !allUsedSessionIds.value.has(r.id)),
);

function endpointFor(pane: Pane): Endpoint | null {
  return pane.remote ? remoteEndpoint.value : localEndpoint.value;
}

function paneSessionInfo(pane: Pane): SessionInfo | null {
  if (!pane.sessionId) return null;
  return findSessionInfo(pane.sessionId, pane.remote) ?? null;
}

function showToast(msg: string) {
  toast.value = msg;
  if (toastHandle !== null) window.clearTimeout(toastHandle);
  toastHandle = window.setTimeout(() => {
    toast.value = "";
    toastHandle = null;
  }, 2000);
}

async function pollSessions() {
  if (!localEndpoint.value) return;
  let cfg = { url: "", token: "", connected: false };
  try {
    cfg = await getRelayConfig();
  } catch {
    /* keep last known */
  }
  remoteEndpoint.value = cfg.connected && cfg.url
    ? { url: cfg.url, token: cfg.token }
    : null;

  const local = await fetchSessions(localEndpoint.value).catch(() => [] as SessionInfo[]);
  const remote: SessionInfo[] = remoteEndpoint.value
    ? await fetchSessions(remoteEndpoint.value).catch(() => [] as SessionInfo[])
    : [];

  const localIds = new Set(local.map((s) => s.id));
  const filteredRemote = remote.filter((s) => !localIds.has(s.id));

  localList.value = local;
  remoteList.value = filteredRemote;

  // Sweep: if any pane references a session id no longer reported, null it.
  const remoteIds = new Set(filteredRemote.map((s) => s.id));
  for (const t of tabs.value) {
    for (let i = 0; i < t.panes.length; i++) {
      const p = t.panes[i];
      if (!p.sessionId) continue;
      if (p.remote ? !remoteIds.has(p.sessionId) : !localIds.has(p.sessionId)) {
        t.panes[i] = { sessionId: null, remote: p.remote };
      }
    }
  }

  if (status.value !== "ready") status.value = "ready";
}

function parseHash(): string | null {
  const m = location.hash.match(/^#\/t\/([\w-]+)$/);
  return m ? m[1] : null;
}
function syncRoute() {
  const id = parseHash();
  if (id && tabs.value.some((t) => t.id === id)) currentTabId.value = id;
}
function gotoTab(id: string) {
  if (location.hash !== "#/t/" + id) {
    location.hash = "#/t/" + id;
  } else {
    currentTabId.value = id;
  }
}

function findSessionInfo(sid: string, remote: boolean): SessionInfo | undefined {
  return (remote ? remoteList.value : localList.value).find((s) => s.id === sid);
}

async function spawnLocalShell(cwd: string): Promise<string> {
  const shells = await listShells();
  if (shells.length === 0) throw new Error("no shells found on this machine");
  const resp = await newSession({ command: shells[0], cwd });
  // Reflect immediately so PaneGrid finds the endpoint without poll lag.
  localList.value = [
    ...localList.value,
    {
      id: resp.session_id,
      command: shells[0],
      cwd: cwd || "",
      title: shells[0],
      cols: 80,
      rows: 24,
      started_at: Math.floor(Date.now() / 1000),
      host_id: localHostID.value,
    },
  ];
  return resp.session_id;
}

async function startNewTab() {
  if (starting.value) return;
  starting.value = true;
  errorMsg.value = "";
  try {
    const sid = await spawnLocalShell("");
    const id = newId();
    tabs.value.push({
      id,
      layout: "single",
      panes: [{ sessionId: sid, remote: false }],
      activePaneIdx: 0,
    });
    gotoTab(id);
    pollSessions();
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? String(e);
  } finally {
    starting.value = false;
  }
}

async function onSplit(dir: SplitDir, mode: SplitMode) {
  const t = currentTab.value;
  if (!t) return;

  // Pick mode: bail before mutating layout if there's nothing the picker
  // could populate. Otherwise the user gets a permanently empty quadrant
  // they have to manually close after canceling.
  if (mode === "pick") {
    const usedIds = new Set(
      t.panes.map((p) => p.sessionId).filter((id): id is string => !!id),
    );
    const eligible =
      localList.value.filter((s) => !usedIds.has(s.id)).length +
      remoteList.value.filter((s) => !usedIds.has(s.id)).length;
    if (eligible === 0) {
      showToast(
        remoteEndpoint.value
          ? "no other sessions to attach"
          : "no other sessions — connect a relay or start one locally",
      );
      return;
    }
  }

  const result = transitionLayout(t.layout, t.panes, t.activePaneIdx, dir);
  if (result.noop) {
    showToast("pane full — close one first");
    return;
  }

  t.layout = result.layout;
  t.panes = result.panes;
  t.activePaneIdx = result.activePaneIdx;

  if (mode === "pick") {
    pickerCtx.value = { tabId: t.id, paneIdx: result.newPaneIdx };
    return;
  }

  // New shell starts in the default directory (HOME) — matches iTerm's
  // out-of-the-box behavior. Inheriting the parent pane's cwd would also
  // surface zsh frameworks' async-git prompt redraws (PROMPT_EOL_MARK '%')
  // that don't fire in HOME.
  try {
    const sid = await spawnLocalShell("");
    t.panes[result.newPaneIdx] = { sessionId: sid, remote: false };
  } catch (e: any) {
    showToast("split failed: " + (e?.message ?? e));
  }
}

function onPickerPick(payload: { sessionId: string; remote: boolean }) {
  const ctx = pickerCtx.value;
  pickerCtx.value = null;
  if (!ctx) return;
  const t = tabs.value.find((tt) => tt.id === ctx.tabId);
  if (!t) return;
  if (t.panes.some((p, i) => i !== ctx.paneIdx && p.sessionId === payload.sessionId)) {
    showToast("that session is already in this tab");
    return;
  }
  t.panes[ctx.paneIdx] = { sessionId: payload.sessionId, remote: payload.remote };
}

function onPickerClose() {
  pickerCtx.value = null;
}

async function onClosePane() {
  const t = currentTab.value;
  if (!t) return;
  closePaneAt(t, t.activePaneIdx);
}

async function closePaneAt(t: Tab, idx: number) {
  const target = t.panes[idx];
  if (target?.sessionId && !target.remote) {
    try { await closeSession(target.sessionId); } catch { /* sweep cleans up */ }
  }
  const r = closePane(t.layout, t.panes, idx);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  if (r.closeTab) {
    closeTab(t.id);
  }
}

async function closeTab(id: string) {
  const t = tabs.value.find((tt) => tt.id === id);
  if (!t) return;
  const closures: Promise<void>[] = [];
  for (const p of t.panes) {
    if (p.sessionId && !p.remote) {
      closures.push(closeSession(p.sessionId).catch(() => undefined));
    }
  }
  await Promise.all(closures);
  tabs.value = tabs.value.filter((tt) => tt.id !== id);
  if (currentTabId.value === id) {
    if (tabs.value.length > 0) gotoTab(tabs.value[0].id);
    else location.hash = "";
  }
}

function onFocusPane(dir: "left" | "right" | "up" | "down") {
  const t = currentTab.value;
  if (!t) return;
  const next = focusNeighbor(t.layout, t.activePaneIdx, dir);
  if (next !== null) t.activePaneIdx = next;
}

function onSwitchTab(delta: number) {
  if (tabs.value.length === 0) return;
  const idx = tabs.value.findIndex((t) => t.id === currentTabId.value);
  if (idx === -1) return;
  const next = (idx + delta + tabs.value.length) % tabs.value.length;
  gotoTab(tabs.value[next].id);
}

function openRemoteAsTab(sessionId: string) {
  const id = newId();
  tabs.value.push({
    id,
    layout: "single",
    panes: [{ sessionId, remote: true }],
    activePaneIdx: 0,
  });
  showRemote.value = false;
  gotoTab(id);
}

const tabSummaries = computed(() =>
  tabs.value.map((t) => {
    const active = t.panes[t.activePaneIdx];
    const info = active?.sessionId ? findSessionInfo(active.sessionId, active.remote) ?? null : null;
    return {
      id: t.id,
      layout: t.layout,
      activeSession: info,
      activeRemote: !!active?.remote,
      paneCount: t.panes.length,
    };
  }),
);

const sessionCount = computed(() => allUsedSessionIds.value.size);

useTerminalShortcuts({
  onSplitVertical: (mode) => onSplit("vertical", mode),
  onSplitHorizontal: (mode) => onSplit("horizontal", mode),
  onClosePane,
  onFocusPane,
  onNewTab: startNewTab,
  onSwitchTab,
});

watch([tabs, currentTabId], () => {
  if (tabs.value.length === 0) return;
  if (currentTabId.value && tabs.value.find((t) => t.id === currentTabId.value)) return;
  gotoTab(tabs.value[0].id);
});

onMounted(async () => {
  syncRoute();
  window.addEventListener("hashchange", syncRoute);
  try {
    localEndpoint.value = await getEndpoint();
    const info = await getHostInfo();
    localHostID.value = info.host_id;
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? "Wails bindings unavailable";
    return;
  }
  await pollSessions();
  pollHandle = window.setInterval(pollSessions, 2000);

  if (!autoStarted && tabs.value.length === 0) {
    autoStarted = true;
    startNewTab();
  }
});

onUnmounted(() => {
  window.removeEventListener("hashchange", syncRoute);
  if (pollHandle !== null) window.clearInterval(pollHandle);
  if (toastHandle !== null) window.clearTimeout(toastHandle);
});
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="brand">atterm</div>
      <div class="status">
        <template v-if="status === 'loading'">starting…</template>
        <template v-else-if="status === 'error'">
          <span class="bad">{{ errorMsg }}</span>
        </template>
        <template v-else>
          {{ sessionCount }} session{{ sessionCount === 1 ? "" : "s" }}
          <span v-if="remoteEndpoint" class="dim"> · uplink on</span>
        </template>
      </div>
      <button
        class="icon-btn"
        :title="remoteEndpoint
          ? `${availableRemote.length} remote session(s) available`
          : 'connect to a relay to see remote sessions'"
        :disabled="!remoteEndpoint"
        @click="showRemote = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M2 16.1A5 5 0 0 1 5.9 20" />
          <path d="M2 12.05A9 9 0 0 1 9.95 20" />
          <path d="M2 8V6a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-6" />
          <line x1="2" y1="20" x2="2.01" y2="20" />
        </svg>
        <span v-if="availableRemote.length > 0" class="badge">{{ availableRemote.length }}</span>
      </button>
      <button
        class="icon-btn"
        title="relay settings"
        @click="showSettings = true"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="16" height="16"
          viewBox="0 0 24 24"
          fill="none" stroke="currentColor"
          stroke-width="2" stroke-linecap="round" stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      </button>
    </header>

    <TabBar
      :tabs="tabSummaries"
      :current-id="currentTabId"
      :starting="starting"
      @activate="gotoTab"
      @close="closeTab"
      @new="startNewTab"
    />

    <main class="main">
      <template v-if="localEndpoint">
        <div v-if="tabs.length === 0" class="empty">
          starting first session…
        </div>
        <PaneGrid
          v-for="t in tabs"
          v-show="t.id === currentTabId"
          :key="t.id"
          :tab="t"
          :endpoint-for="endpointFor"
          :session-info-for="paneSessionInfo"
          :active="t.id === currentTabId"
          @set-active-pane="(idx) => (t.activePaneIdx = idx)"
          @close-pane="(idx) => closePaneAt(t, idx)"
        />
      </template>
      <div v-if="toast" class="toast">{{ toast }}</div>
    </main>

    <SettingsDialog
      v-if="showSettings"
      @close="showSettings = false"
    />
    <RemoteSessionsDialog
      v-if="showRemote"
      :sessions="availableRemote"
      @open="openRemoteAsTab"
      @close="showRemote = false"
    />
    <SessionPickerDialog
      v-if="pickerCtx"
      :exclude-session-ids="currentTab ? currentTab.panes.map((p) => p.sessionId).filter((id): id is string => !!id) : []"
      :local-sessions="localList"
      :remote-sessions="remoteList"
      @pick="onPickerPick"
      @close="onPickerClose"
    />
  </div>
</template>

<style scoped>
.app { display: flex; flex-direction: column; height: 100vh; }
.topbar {
  display: flex; align-items: center; gap: 12px; padding: 10px 16px;
  background: var(--panel); border-bottom: 1px solid var(--border); flex: 0 0 auto;
}
.brand { font-weight: 600; letter-spacing: 0.06em; }
.status { margin-left: auto; font-size: 12px; color: var(--fg-dim); }
.status .bad { color: var(--bad); }
.status .dim { color: var(--good); }

.icon-btn {
  position: relative; display: inline-flex; align-items: center; justify-content: center;
  border: none; background: transparent; color: var(--fg-dim); line-height: 1;
  padding: 6px 8px; border-radius: 6px; cursor: pointer;
  transition: color 120ms, background 120ms;
}
.icon-btn svg { display: block; }
.icon-btn:hover:not(:disabled) { color: var(--accent); background: rgba(88, 166, 255, 0.08); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.icon-btn .badge {
  position: absolute; top: -2px; right: -2px;
  background: #d29922; color: #0d1117; font-size: 9px; font-weight: 700;
  border-radius: 10px; padding: 1px 5px; line-height: 1.3;
  min-width: 16px; text-align: center;
}

.main { flex: 1 1 auto; position: relative; background: #000; overflow: hidden; }
.empty {
  position: absolute; inset: 0; display: flex; align-items: center;
  justify-content: center; color: var(--fg-dim); font-size: 13px;
}
.toast {
  position: absolute; bottom: 12px; left: 50%; transform: translateX(-50%);
  background: rgba(13, 17, 23, 0.92); border: 1px solid var(--border);
  color: var(--fg); padding: 6px 12px; border-radius: 6px; font-size: 12px;
  pointer-events: none;
}
</style>
