<script lang="ts" setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import TabBar from "./components/TabBar.vue";
import PaneGrid from "./components/PaneGrid.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import RemoteSessionsDialog from "./components/RemoteSessionsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import ConfirmQuitDialog from "./components/ConfirmQuitDialog.vue";
import PluginHost from "./plugins/PluginHost.vue";
import { createPluginContext } from "./plugins/usePluginContext";
import { useResizer } from "./plugins/useResizer";
import { usePluginConfigStore } from "./plugins/configStore";
import { sendInputToSession } from "./lib/sendInput";
import { EventsOn } from "../wailsjs/runtime/runtime";
import {
  closeSession,
  confirmQuit,
  getEndpoint,
  getHostInfo,
  getRelayConfig,
  getCommandNotifyThresholdSeconds,
  getTerminalThemePreference,
  getUpdateState,
  listShells,
  newSession,
} from "./lib/api";
import type { Endpoint, UpdateState } from "./lib/api";
import { SessionListConnection, type SessionInfo } from "./lib/connection";
import type { LayoutKind, Pane, Tab, SplitDir } from "./lib/types";
import { closePane, focusNeighbor, transitionLayout } from "./lib/layout";
import { useTerminalShortcuts, type SplitMode } from "./composables/useTerminalShortcuts";
import {
  DEFAULT_TERMINAL_THEME_ID,
  getTerminalTheme,
  type TerminalThemeID,
} from "./lib/terminalThemes";

const localEndpoint = ref<Endpoint | null>(null);
const remoteEndpoint = ref<Endpoint | null>(null);
const localHostID = ref<string>("");

const localList = ref<SessionInfo[]>([]);
const remoteList = ref<SessionInfo[]>([]);
const remoteRawList = ref<SessionInfo[]>([]);

const tabs = ref<Tab[]>([]);
const currentTabId = ref<string | null>(null);

const status = ref<"loading" | "ready" | "error">("loading");
const errorMsg = ref<string>("");
const starting = ref(false);
const showSettings = ref(false);
const showRemote = ref(false);
const toast = ref<string>("");

const quitDialogOpen = ref(false);
let quitListenerOff: (() => void) | null = null;

function handleBeforeClose() {
  if (localSessionCount.value === 0 && remoteSessionCount.value === 0) {
    void confirmQuit();
    return;
  }
  quitDialogOpen.value = true;
}

function onConfirmQuit() {
  quitDialogOpen.value = false;
  void confirmQuit();
}

function onCancelQuit() {
  quitDialogOpen.value = false;
}

const updateBadge = ref(false);
let updatePollHandle: number | null = null;

const currentTerminalThemeID = ref<TerminalThemeID>(DEFAULT_TERMINAL_THEME_ID);
const currentTerminalTheme = computed(() => getTerminalTheme(currentTerminalThemeID.value));
const themeStyle = computed(() => currentTerminalTheme.value.appVars);

const commandNotifyThresholdSec = ref<number>(10);

// Picker state. When non-null, dialog is open and the resolved pick will go
// into tabs[*].panes[paneIdx] of the indicated tab (always the current tab).
const pickerCtx = ref<{ tabId: string; paneIdx: number } | null>(null);

let autoStarted = false;
let toastHandle: number | null = null;
let localSessionListConn: SessionListConnection | null = null;
let remoteSessionListConn: SessionListConnection | null = null;

// One-shot off-screen Terminal+FitAddon used as a measure probe. We resize
// its parent div to a target cell size, call FitAddon.proposeDimensions(),
// and use the result to spawn the PTY at the same cols/rows xterm.js would
// pick on the real cell. Goal: avoid the SIGWINCH between fork and first
// prompt that triggers zsh's PROMPT_EOL_MARK ('%') for some prompt themes.
//
// Probe layout: a 0x0 overflow:hidden host pinned at the top-left of the
// viewport, with the actual measure div (400x300, position:absolute) inside
// it. The host's clip prevents the probe from extending document scroll
// area — earlier `position:absolute; left:-99999px` placement leaked into
// body.scrollWidth and made WKWebView paint root-level scrollbars on the
// real terminal.
let measureTerm: Terminal | null = null;
let measureFit: FitAddon | null = null;
let measureDiv: HTMLDivElement | null = null;
let measureHost: HTMLDivElement | null = null;

function setupMeasureProbe(): Promise<void> {
  return new Promise((resolve) => {
    measureHost = document.createElement("div");
    measureHost.style.cssText =
      "position:fixed;top:0;left:0;width:0;height:0;overflow:hidden;pointer-events:none;visibility:hidden;";
    measureDiv = document.createElement("div");
    measureDiv.style.cssText = "position:absolute;top:0;left:0;width:400px;height:300px;";
    measureHost.appendChild(measureDiv);
    document.body.appendChild(measureHost);
    measureTerm = new Terminal({
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      fontSize: 13,
      allowProposedApi: true,
    });
    measureFit = new FitAddon();
    measureTerm.loadAddon(measureFit);
    measureTerm.open(measureDiv);
    // Renderer needs a frame to compute the css cell size.
    requestAnimationFrame(() => resolve());
  });
}

function teardownMeasureProbe() {
  measureTerm?.dispose();
  if (measureHost && measureHost.parentElement) {
    measureHost.parentElement.removeChild(measureHost);
  }
  measureTerm = null;
  measureFit = null;
  measureDiv = null;
  measureHost = null;
}

// Predict what xterm.js's FitAddon will pick for a cell of the given px
// dimensions. Routes through the probe so the math is the same as the real
// fit() call — within a column.
function predictCellDimsForSize(width: number, height: number): { cols: number; rows: number } {
  if (!measureFit || !measureDiv) return { cols: 80, rows: 24 };
  measureDiv.style.width = `${Math.max(40, Math.floor(width))}px`;
  measureDiv.style.height = `${Math.max(40, Math.floor(height))}px`;
  // Force layout so proposeDimensions reads the new size.
  void measureDiv.offsetWidth;
  const dims = measureFit.proposeDimensions();
  if (!dims || !dims.cols || !dims.rows) return { cols: 80, rows: 24 };
  return { cols: dims.cols, rows: dims.rows };
}

function predictCellDims(layout: LayoutKind): { cols: number; rows: number } {
  const main = document.querySelector(".main") as HTMLElement | null;
  if (!main || main.clientWidth < 100 || main.clientHeight < 100) {
    return { cols: 80, rows: 24 };
  }
  const colsDiv = layout === "vertical" || layout === "grid2x2" ? 2 : 1;
  const rowsDiv = layout === "horizontal" || layout === "grid2x2" ? 2 : 1;
  // PaneGrid has gap:2px between cells.
  const cellW = (main.clientWidth - (colsDiv - 1) * 2) / colsDiv;
  const cellH = (main.clientHeight - (rowsDiv - 1) * 2) / rowsDiv;
  return predictCellDimsForSize(cellW, cellH);
}

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : "tab-" + Math.random().toString(36).slice(2);

const currentTab = computed<Tab | null>(
  () => tabs.value.find((t) => t.id === currentTabId.value) ?? null,
);

// Keep a Ref (not ComputedRef) so it satisfies PluginContextInputs.activePane.
const activePaneRef = ref<Pane | null>(null);
watch(
  [() => currentTab.value, () => currentTab.value?.activePaneIdx],
  () => {
    const t = currentTab.value;
    activePaneRef.value = t ? t.panes[t.activePaneIdx] ?? null : null;
  },
  { immediate: true, deep: false },
);

const pluginContext = createPluginContext({
  activePane: activePaneRef,
  endpointForPane: endpointFor,
  sessionInfoForPane: paneSessionInfo,
  sendToSession: (sessionId, endpoint, text) => sendInputToSession(endpoint, sessionId, text),
  showToast,
});

const pluginStore = usePluginConfigStore();

const persistedPanelWidth = computed(() => pluginStore.cfg?.fileExplorer.panelWidthPx ?? 380);
const dragPanelWidth = ref<number | null>(null);
const panelWidth = computed(() => dragPanelWidth.value ?? persistedPanelWidth.value);

const panelCollapsed = computed({
  get: () => pluginStore.cfg?.fileExplorer.panelCollapsed ?? true,
  set: (v: boolean) => {
    if (!pluginStore.cfg) return;
    const next = JSON.parse(JSON.stringify(pluginStore.cfg));
    next.fileExplorer.panelCollapsed = v;
    void pluginStore.save(next);
  },
});

function togglePanel() { panelCollapsed.value = !panelCollapsed.value; }

const { onMouseDown: onPanelResizeDown } = useResizer({
  onDrag: (deltaX) => {
    const current = dragPanelWidth.value ?? persistedPanelWidth.value;
    const next = Math.max(240, Math.min(current - deltaX, window.innerWidth * 0.7));
    dragPanelWidth.value = next;
  },
  onEnd: () => {
    if (dragPanelWidth.value === null || !pluginStore.cfg) {
      dragPanelWidth.value = null;
      return;
    }
    const next = JSON.parse(JSON.stringify(pluginStore.cfg));
    next.fileExplorer.panelWidthPx = dragPanelWidth.value;
    void pluginStore.save(next);
    dragPanelWidth.value = null;
  },
});

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

function sweepMissingSessions() {
  const localIds = new Set(localList.value.map((s) => s.id));
  const remoteIds = new Set(remoteList.value.map((s) => s.id));
  for (const t of tabs.value) {
    for (let i = 0; i < t.panes.length; i++) {
      const p = t.panes[i];
      if (!p.sessionId) continue;
      if (p.remote ? !remoteIds.has(p.sessionId) : !localIds.has(p.sessionId)) {
        t.panes[i] = { sessionId: null, remote: p.remote };
      }
    }
  }
}

function refreshVisibleRemoteSessions() {
  const localIds = new Set(localList.value.map((s) => s.id));
  remoteList.value = remoteRawList.value.filter((s) => !localIds.has(s.id));
}

function applyLocalSessions(sessions: SessionInfo[]) {
  localList.value = sessions;
  refreshVisibleRemoteSessions();
  sweepMissingSessions();
  if (status.value !== "ready") status.value = "ready";
}

function applyRemoteSessions(sessions: SessionInfo[]) {
  remoteRawList.value = sessions;
  refreshVisibleRemoteSessions();
  sweepMissingSessions();
}

function connectLocalSessionList(endpoint: Endpoint) {
  localSessionListConn?.detach();
  localSessionListConn = new SessionListConnection(endpoint, {
    onSessions: applyLocalSessions,
    onStatus: (s) => {
      if (s === "error") status.value = "error";
    },
  });
  localSessionListConn.attach();
}

function connectRemoteSessionList(endpoint: Endpoint | null) {
  remoteSessionListConn?.detach();
  remoteSessionListConn = null;
  remoteRawList.value = [];
  remoteList.value = [];
  remoteEndpoint.value = endpoint;
  sweepMissingSessions();
  if (!endpoint) return;
  remoteSessionListConn = new SessionListConnection(endpoint, {
    onSessions: applyRemoteSessions,
  });
  remoteSessionListConn.attach();
}

async function refreshRelayConfig() {
  let cfg = { url: "", token: "", connected: false };
  try {
    cfg = await getRelayConfig();
  } catch {
    /* keep last known */
  }
  const next = cfg.connected && cfg.url
    ? { url: cfg.url, token: cfg.token }
    : null;
  if (
    remoteEndpoint.value?.url === next?.url &&
    remoteEndpoint.value?.token === next?.token
  ) {
    return;
  }
  connectRemoteSessionList(next);
}

async function refreshTerminalTheme() {
  const themeID = await getTerminalThemePreference();
  currentTerminalThemeID.value = getTerminalTheme(themeID).id;
}

function onTerminalThemeChanged(themeID: string) {
  currentTerminalThemeID.value = getTerminalTheme(themeID).id;
}

function onCommandNotifyThresholdChanged(seconds: number) {
  commandNotifyThresholdSec.value = seconds;
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
  currentTabId.value = id;
  if (location.hash !== "#/t/" + id) {
    location.hash = "#/t/" + id;
  }
}

function findSessionInfo(sid: string, remote: boolean): SessionInfo | undefined {
  return (remote ? remoteList.value : localList.value).find((s) => s.id === sid);
}

async function spawnLocalShell(
  cwd: string,
  dims: { cols: number; rows: number },
): Promise<string> {
  const shells = await listShells();
  if (shells.length === 0) throw new Error("no shells found on this machine");
  const resp = await newSession({
    command: shells[0],
    cwd,
    cols: dims.cols,
    rows: dims.rows,
  });
  // Reflect immediately so PaneGrid finds the endpoint without poll lag.
  localList.value = [
    ...localList.value,
    {
      id: resp.session_id,
      command: shells[0],
      cwd: cwd || "",
      title: shells[0],
      cols: dims.cols,
      rows: dims.rows,
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
    const sid = await spawnLocalShell("", predictCellDims("single"));
    const id = newId();
    tabs.value.push({
      id,
      layout: "single",
      panes: [{ sessionId: sid, remote: false }],
      activePaneIdx: 0,
    });
    gotoTab(id);
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

  // New shell starts in HOME (cwd="") — matches iTerm's default behavior.
  // Cols/rows are predicted via the FitAddon probe so the PTY is born at
  // the same dimensions xterm.js will land on after fit(); without this
  // there's a SIGWINCH between fork and first prompt that some zsh themes
  // turn into a stray PROMPT_EOL_MARK ('%').
  try {
    const sid = await spawnLocalShell("", predictCellDims(result.layout));
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

const localSessionCount = computed(() => {
  let n = 0;
  for (const t of tabs.value) {
    for (const p of t.panes) {
      if (p.sessionId && !p.remote) n++;
    }
  }
  return n;
});
const remoteSessionCount = computed(() => {
  let n = 0;
  for (const t of tabs.value) {
    for (const p of t.panes) {
      if (p.sessionId && p.remote) n++;
    }
  }
  return n;
});

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
  quitListenerOff = EventsOn("before-close", handleBeforeClose);
  syncRoute();
  window.addEventListener("hashchange", syncRoute);
  // Set up the size-prediction probe before anything spawns a PTY — the
  // probe must be ready by the time auto-startNewTab fires.
  await setupMeasureProbe();
  try {
    commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
  } catch (e) {
    console.warn("[AT Term] failed to load command-notify threshold", e);
  }
  try {
    await refreshTerminalTheme();
    localEndpoint.value = await getEndpoint();
    const info = await getHostInfo();
    localHostID.value = info.host_id;
    connectLocalSessionList(localEndpoint.value);
    await refreshRelayConfig();
  } catch (e: any) {
    status.value = "error";
    errorMsg.value = e?.message ?? "Wails bindings unavailable";
    return;
  }

  // Auto-update poll: every 5s pull state.available || ready and toggle the
  // ⚙ badge dot. Lower frequency than session poll because update state
  // changes are rare (boot check + 24h ticker).
  updatePollHandle = window.setInterval(async () => {
    try {
      const st: UpdateState = await getUpdateState();
      updateBadge.value = !!(st.available || st.ready);
    } catch {
      /* ignore — never block UI on updater failures */
    }
  }, 5000);

  if (!autoStarted && tabs.value.length === 0) {
    autoStarted = true;
    startNewTab();
  }
});

onUnmounted(() => {
  quitListenerOff?.();
  quitListenerOff = null;
  window.removeEventListener("hashchange", syncRoute);
  localSessionListConn?.detach();
  remoteSessionListConn?.detach();
  if (toastHandle !== null) window.clearTimeout(toastHandle);
  if (updatePollHandle !== null) window.clearInterval(updatePollHandle);
  teardownMeasureProbe();
});
</script>

<template>
  <div class="app" :style="themeStyle">
    <header class="topbar">
      <div class="brand">AT Term</div>
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
        <span v-if="updateBadge" class="dot"></span>
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

    <div class="main-row">
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
            :terminal-theme="currentTerminalTheme.xtermTheme"
            :command-notify-threshold-sec="commandNotifyThresholdSec"
            @set-active-pane="(idx) => (t.activePaneIdx = idx)"
            @close-pane="(idx) => closePaneAt(t, idx)"
            @toast="showToast"
          />
        </template>
        <div v-if="toast" class="toast">{{ toast }}</div>
      </main>
      <button class="panel-toggle" @click="togglePanel" :title="panelCollapsed ? 'Show panel' : 'Hide panel'">
        {{ panelCollapsed ? '‹' : '›' }}
      </button>
      <template v-if="!panelCollapsed">
        <div class="right-resizer" @mousedown="onPanelResizeDown" />
        <PluginHost slot-id="right-panel" :context="pluginContext" class="right-panel"
                    :style="{ flex: '0 0 ' + panelWidth + 'px' }" />
      </template>
    </div>
    <PluginHost slot-id="bottom-toolbar" :context="pluginContext" class="bottom-toolbar" />

    <SettingsDialog
      v-if="showSettings"
      :local-session-count="localSessionCount"
      :remote-session-count="remoteSessionCount"
      :terminal-theme-id="currentTerminalThemeID"
      @terminal-theme-changed="onTerminalThemeChanged"
      @command-notify-threshold-changed="onCommandNotifyThresholdChanged"
      @relay-config-changed="refreshRelayConfig"
      @close="showSettings = false; refreshRelayConfig()"
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
    <ConfirmQuitDialog
      v-if="quitDialogOpen"
      :local-count="localSessionCount"
      :remote-count="remoteSessionCount"
      @confirm="onConfirmQuit"
      @cancel="onCancelQuit"
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
.icon-btn .dot {
  position: absolute; top: 2px; right: 2px;
  width: 6px; height: 6px;
  background: #d29922;
  border-radius: 50%;
}

.main-row {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
.main { flex: 1 1 auto; display: flex; flex-direction: column; position: relative; background: #000; overflow: hidden; min-width: 0; }
.right-resizer { width: 4px; cursor: col-resize; background: transparent; flex: 0 0 4px; }
.right-resizer:hover { background: #2d333b; }
.panel-toggle { background: #21262d; border: 1px solid #2d333b; color: #c9d1d9; cursor: pointer; padding: 0 4px; font-size: 11px; align-self: stretch; flex: 0 0 auto; }
.panel-toggle:hover { background: #30363d; }
.right-panel:empty {
  display: none;
}
.right-panel {
  border-left: 1px solid #2d333b;
  overflow: hidden;
}
.bottom-toolbar:empty {
  display: none;
}
.bottom-toolbar {
  flex: 0 0 32px;
  height: 32px;
  border-top: 1px solid #2d333b;
}
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
