<script lang="ts" setup>
import { computed, onMounted, onUnmounted, provide, reactive, ref, watch } from "vue";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import TabBar from "./components/TabBar.vue";
import TitleBar from "./components/TitleBar.vue";
import { useWindowMaximized } from "./composables/useWindowMaximized";
import PaneGrid from "./components/PaneGrid.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import RemoteSessionsDialog from "./components/RemoteSessionsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import ConfirmQuitDialog from "./components/ConfirmQuitDialog.vue";
import ShortcutHints from "./components/ShortcutHints.vue";
import PluginHost from "./plugins/PluginHost.vue";
import TranslatePanelHost from "./plugins/translate/TranslatePanelHost.vue";
import { createPluginContext } from "./plugins/usePluginContext";
import { useResizer } from "./plugins/useResizer";
import { usePluginConfigStore } from "./plugins/configStore";
import { sendInputToSession } from "./lib/sendInput";
// Plugin theme palettes (CSS vars). Loaded in main bundle so the panel
// toggle and Quick Input toolbar can read --ed-* vars even when the
// file-explorer chunk is not yet loaded.
import "./plugins/fileExplorer/theme.css";
import { isLightTerminalTheme } from "./lib/terminalThemes";
import { usePlatform } from './platform'
const $platform = usePlatform()
const caps = $platform.caps
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

// Auth-error banner: set when the relay closes the uplink with a 4001-4003
// close code. Cleared when the user dismisses or fixes config.
const authError = ref<string | null>(null);
// Per-session remote viewer count, fed by the "relay:viewers" event.
const viewerCounts = reactive<Record<string, number>>({});
function viewerCountFor(sessionId: string): number {
  return viewerCounts[sessionId] ?? 0;
}
const authErrorBanners: Record<string, string> = {
  auth_invalid_token: "Invalid or revoked API token. Generate a new one in web settings.",
  auth_user_disabled: "Account disabled. Contact your relay admin.",
  session_id_owner_mismatch: "Session id collision. Restart the desktop app.",
  forbidden: "Not authorized to connect to this relay.",
};
const authErrorMessage = computed(() =>
  authError.value ? (authErrorBanners[authError.value] ?? authError.value) : "",
);

function openSettingsRelay() {
  settingsInitialTab.value = "relay";
  showSettings.value = true;
}

// Track which tab to open when Settings is opened programmatically.
const settingsInitialTab = ref<"general" | "relay" | "logging" | "updates" | undefined>(undefined);

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

const isMaximized = useWindowMaximized();
const platform = ref<string>("");
const showMaximizedInset = computed(() => isMaximized.value && platform.value !== "darwin");

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

// Each TerminalView registers a driver-side input sender keyed by sessionId.
// Plugins (Quick Input) route their send() through this map so input rides
// the existing driver SessionConnection — a freshly attached connection
// would be a viewer and the relay would drop its IN frames.
const pluginInputSenders = new Map<string, (text: string) => void>();
provide("atterm:pluginInputSenders", pluginInputSenders);

const pluginContext = createPluginContext({
  activePane: activePaneRef,
  endpointForPane: endpointFor,
  sessionInfoForPane: paneSessionInfo,
  sendToSession: (sessionId, endpoint, text) => {
    const sender = pluginInputSenders.get(sessionId);
    if (sender) {
      sender(text);
      return;
    }
    // Fallback for sessions without a mounted TerminalView. Will likely be
    // a viewer-only connection; relay may drop the IN, but it's better than
    // no attempt.
    sendInputToSession(endpoint, sessionId, text);
  },
  showToast,
  terminalThemeId: currentTerminalThemeID,
});
provide("atterm:pluginContext", pluginContext);

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

// True when at least one right-panel plugin is enabled. Suppresses the
// collapse handle entirely when the slot has nothing to host.
const rightPanelHasPlugin = computed(() => pluginStore.isPluginEnabled("file-explorer"));

// Derive a plugin-side theme name from the active terminal theme so the
// global --ed-* CSS vars on .app can paint the panel toggle, Quick Input
// bar, and the file explorer in matching dimmed/light skins.
const fileExplorerTheme = computed<"dimmed" | "light">(() =>
  isLightTerminalTheme(currentTerminalThemeID.value) ? "light" : "dimmed",
);

const { onMouseDown: onPanelResizeDown } = useResizer({
  onDrag: (deltaX) => {
    // useResizer reports deltaX = -mouseMovementX (right drag → negative).
    // The resizer sits on the panel's left edge, so dragging right shrinks
    // the panel: panelWidth + deltaX = panelWidth - movement.
    const current = dragPanelWidth.value ?? persistedPanelWidth.value;
    const next = Math.max(240, Math.min(current + deltaX, window.innerWidth * 0.7));
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

const shortcutBindings = computed<Record<string, string>>(() => {
  return pluginStore.cfg?.shortcuts?.bindings ?? {};
});

useTerminalShortcuts(
  {
    onSplitVertical: (mode) => onSplit("vertical", mode),
    onSplitHorizontal: (mode) => onSplit("horizontal", mode),
    onClosePane,
    onFocusPane,
    onNewTab: startNewTab,
    onSwitchTab,
  },
  { bindings: shortcutBindings },
);

watch([tabs, currentTabId], () => {
  if (tabs.value.length === 0) return;
  if (currentTabId.value && tabs.value.find((t) => t.id === currentTabId.value)) return;
  gotoTab(tabs.value[0].id);
});

onMounted(async () => {
  quitListenerOff = $platform.events.on('before-close', handleBeforeClose);
  try {
    const info = await $platform.system.getEnvironment();
    if (info !== null) {
      platform.value = (info.platform ?? "").toLowerCase();
    }
  } catch {
    /* keep default empty; .is-maximized stays off on darwin-bug-side */
  }
  $platform.events.on('relay:auth-error', (data) => {
    const d = data as { reason: string };
    authError.value = d?.reason ?? null;
  });
  $platform.events.on('relay:viewers', (data) => {
    const d = data as { session_id: string; count: number };
    if (d && typeof d.session_id === 'string') {
      viewerCounts[d.session_id] = d.count ?? 0;
    }
  });
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
  <div class="app" :class="[`fe-theme-${fileExplorerTheme}`, { 'is-maximized': showMaximizedInset }]" :style="themeStyle">
    <TitleBar
      v-if="caps.windowControls"
      :status="status"
      :error-msg="errorMsg"
      :session-count="sessionCount"
      :remote-endpoint="remoteEndpoint"
      :available-remote-count="availableRemote.length"
      :update-badge="updateBadge"
      @open-remote="showRemote = true"
      @open-settings="showSettings = true"
    />

    <div v-if="authError" class="auth-error-banner" role="alert">
      <span class="auth-error-msg">{{ authErrorMessage }}</span>
      <button class="auth-error-action" @click="openSettingsRelay">Open settings</button>
      <button class="auth-error-dismiss" @click="authError = null" aria-label="Dismiss">×</button>
    </div>

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
            :viewer-count-for="viewerCountFor"
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
      <template v-if="rightPanelHasPlugin">
        <button class="panel-toggle" @click="togglePanel" :title="panelCollapsed ? 'Show panel' : 'Hide panel'">
          {{ panelCollapsed ? '‹' : '›' }}
        </button>
        <template v-if="!panelCollapsed">
          <div class="right-resizer" @mousedown="onPanelResizeDown" />
          <PluginHost slot-id="right-panel" :context="pluginContext" class="right-panel"
                      :style="{ flex: '0 0 ' + panelWidth + 'px' }" />
        </template>
      </template>
    </div>
    <!-- Bottom-toolbar plugins (Quick Input) live at the app root so a single
         instance persists across tab/pane switches. They target the active
         pane via pluginContext.activePane (a reactive ref). -->
    <PluginHost
      slot-id="bottom-toolbar"
      :context="pluginContext"
      class="bottom-toolbar"
    />
    <TranslatePanelHost />

    <SettingsDialog
      v-if="showSettings"
      :local-session-count="localSessionCount"
      :remote-session-count="remoteSessionCount"
      :terminal-theme-id="currentTerminalThemeID"
      :initial-tab="settingsInitialTab"
      @terminal-theme-changed="onTerminalThemeChanged"
      @command-notify-threshold-changed="onCommandNotifyThresholdChanged"
      @relay-config-changed="refreshRelayConfig"
      @close="showSettings = false; settingsInitialTab = undefined; refreshRelayConfig()"
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
    <ShortcutHints />
  </div>
</template>

<style scoped>
.app { display: flex; flex-direction: column; height: 100vh; }
.app.is-maximized { padding: 8px; }
.auth-error-banner {
  display: flex; align-items: center; gap: 8px;
  padding: 7px 16px; background: #5a1e1e; border-bottom: 1px solid #8b2e2e;
  color: #ffb3b3; font-size: 12px; flex: 0 0 auto;
}
.auth-error-msg { flex: 1 1 auto; }
.auth-error-action {
  flex: 0 0 auto; border: 1px solid #8b2e2e; background: transparent;
  color: #ffb3b3; border-radius: 4px; padding: 2px 8px; font-size: 12px;
  cursor: pointer;
}
.auth-error-action:hover { background: rgba(255, 179, 179, 0.1); }
.auth-error-dismiss {
  flex: 0 0 auto; border: none; background: transparent;
  color: #ffb3b3; font-size: 16px; line-height: 1; cursor: pointer; padding: 0 4px;
}
.auth-error-dismiss:hover { color: #fff; }

.main-row {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
}
.main { flex: 1 1 auto; display: flex; flex-direction: column; position: relative; background: #000; overflow: hidden; min-width: 0; }
.right-resizer { width: 4px; cursor: col-resize; background: transparent; flex: 0 0 4px; }
.right-resizer:hover { background: var(--ed-border, #2d333b); }
.panel-toggle {
  background: var(--ed-tab-bg, #21262d);
  border: 1px solid var(--ed-border, #2d333b);
  color: var(--ed-row-fg, #c9d1d9);
  cursor: pointer;
  padding: 0 4px;
  font-size: 11px;
  align-self: stretch;
  flex: 0 0 auto;
}
.panel-toggle:hover {
  background: var(--ed-row-hover, #30363d);
  color: var(--ed-tab-hover-fg, #ffffff);
}
.right-panel:empty {
  display: none;
}
.right-panel {
  border-left: 1px solid var(--ed-border, #2d333b);
  overflow: hidden;
}
.bottom-toolbar:empty {
  display: none;
}
.bottom-toolbar {
  flex: 0 0 24px;
  height: 24px;
  background: var(--ed-tab-bg, var(--terminal-bg));
  border-top: 1px solid var(--ed-border, rgba(255, 255, 255, 0.08));
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
