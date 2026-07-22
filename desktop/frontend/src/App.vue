<script lang="ts" setup>
import { computed, onMounted, onUnmounted, provide, reactive, ref, watch } from "vue";
import { Terminal } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import TabBar from "./components/TabBar.vue";
import TitleBar from "./components/TitleBar.vue";
import TaskSidebar from "./components/TaskSidebar.vue";
import { useWindowMaximized } from "./composables/useWindowMaximized";
import PaneGrid from "./components/PaneGrid.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import ConfirmQuitDialog from "./components/ConfirmQuitDialog.vue";
import RecoveryDialog from "./components/RecoveryDialog.vue";
import ShortcutHints from "./components/ShortcutHints.vue";
import PasteImagePreviewHost from "./components/PasteImagePreviewHost.vue";
import PasteFilePreviewHost from "./components/PasteFilePreviewHost.vue";
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
  markSessionsSeen,
  listRemoteSessions,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  loadRecoverySnapshot,
  getRecoveryDialogEnabled,
  discardRecoverySnapshot,
  type MarkSessionsSeenOpts,
  type RecoverySnapshot,
  type RecoveryTabSnapshot,
} from "./lib/api";
import type { Endpoint, UpdateState } from "./lib/api";
import type { RemoteSession } from "./platform/types";
import { SessionListConnection, type SessionConnection, type SessionInfo } from "./lib/connection";
import { mergeLocalSessions } from "./lib/localListMerge";
import { pruneStaleRemoteTabs } from "./lib/remoteTabCleanup";
import { PANE_COUNT, type LayoutKind, type Pane, type Tab, type SplitDir } from "./lib/types";
import { RATIO_DEFAULT, closePane, findPaneLocation, focusNeighbor, transitionLayout } from "./lib/layout";
import { useTerminalShortcuts, type SplitMode } from "./composables/useTerminalShortcuts";
import { useSessions } from "./composables/useSessions";
import { useRecoverySnapshot } from "./composables/useRecoverySnapshot";
import {
  buildRestoreSessionReq,
  synthSessionInfoFromSnapshot,
} from "./lib/recoveryRestore";
import {
  DEFAULT_TERMINAL_THEME_ID,
  getTerminalTheme,
  type TerminalThemeID,
} from "./lib/terminalThemes";
import { useI18n } from "./i18n/useI18n";
import type { MessageKey } from "./i18n";

const { t: i18nT } = useI18n();

// Auth-error banner: set when the relay closes the uplink with a 4001-4003
// close code. Cleared when the user dismisses or fixes config.
const authError = ref<string | null>(null);
// Per-session remote viewer count, fed by the "relay:viewers" event.
const viewerCounts = reactive<Record<string, number>>({});
function viewerCountFor(sessionId: string): number {
  return viewerCounts[sessionId] ?? 0;
}
const authErrorBanners: Record<string, MessageKey> = {
  auth_invalid_token: "app.authInvalidToken",
  auth_user_disabled: "app.authUserDisabled",
  session_id_owner_mismatch: "app.sessionIdOwnerMismatch",
  forbidden: "app.forbidden",
};
const authErrorMessage = computed(() =>
  authError.value ? (authErrorBanners[authError.value] ? i18nT(authErrorBanners[authError.value]) : authError.value) : "",
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
const localHost = ref<string>("");

const localList = ref<SessionInfo[]>([]);
const remoteList = ref<SessionInfo[]>([]);
const remoteRawList = ref<SessionInfo[]>([]);
const REMOTE_STALE_TAB_GRACE_MS = 60_000;
const remoteMissingSince = new Map<string, number>();
// IDs of local sessions we just spawned (spawnLocalShell / executeRestore) but
// whose presence Go has not yet echoed back in a LIST_RESP. While here, the
// session is exempt from applyLocalSessions' overwrite and from sweep — see
// mergeLocalSessions. Drained as Go acknowledges each id, or when the pane
// holding it is closed.
const pendingLocalIds = new Set<string>();

// Adapt SessionInfo (uses `id`) → RemoteSession (uses `session_id`) for useSessions.
function adaptSession(s: SessionInfo): RemoteSession {
  return { ...s, session_id: s.id } as unknown as RemoteSession;
}
const localListAdapted = computed<RemoteSession[]>(() => localList.value.map(adaptSession));
const remoteListAdapted = computed<RemoteSession[]>(() => remoteList.value.map(adaptSession));

const sessions = useSessions(localListAdapted, remoteListAdapted);

const sidebarCollapsed = ref(false);

async function setSidebarCollapsedAndPersist(v: boolean) {
  sidebarCollapsed.value = v;
  try {
    await setTaskSidebarCollapsed(v);
  } catch {
    /* persistence best-effort */
  }
}

function onSidebarOpen(s: RemoteSession) {
  openRemoteAsTab(s.session_id);
}

function onSidebarClose(s: RemoteSession) {
  const loc = findPaneLocation(tabs.value, s.session_id);
  if (!loc) return;
  const t = tabs.value.find((tab) => tab.id === loc.tabId);
  if (!t) return;
  void closePaneAt(t, loc.paneIdx);
}

function openRemoteFromTitleBar() {
  void setSidebarCollapsedAndPersist(false);
}

async function onMarkSeen(payload: MarkSessionsSeenOpts) {
  try {
    await markSessionsSeen(payload);
  } catch {
    console.warn("markSessionsSeen failed", payload);
  }
}

const tabs = ref<Tab[]>([]);
const currentTabId = ref<string | null>(null);

const status = ref<"loading" | "ready" | "error">("loading");
const errorMsg = ref<string>("");
const starting = ref(false);
const showSettings = ref(false);
const toast = ref<string>("");

const quitDialogOpen = ref(false);
let quitListenerOff: (() => void) | null = null;
let notificationClickListenerOff: (() => void) | null = null;

function handleBeforeClose() {
  // Best-effort final persist so a clean quit always lands the latest state.
  // (Sleep / force-quit are covered by the composable's periodic safety flush.)
  recovery.flushNow();
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

// Recovery dialog state. Populated during boot from loadRecoverySnapshot()
// when a non-empty snapshot exists AND the recovery-dialog setting is on.
// Cleared when the user picks restore/discard.
const recoveryDialogState = ref<{ open: boolean; snapshot: RecoverySnapshot | null }>({
  open: false,
  snapshot: null,
});

let autoStarted = false;
let toastHandle: number | null = null;
let localSessionListConn: SessionListConnection | null = null;
let remotePollHandle: number | null = null;
// Dedup key for refreshRelayConfig: "connected|attachUrl|token". Avoids
// restarting the remote poll / re-setting the endpoint when nothing changed.
let lastRemoteKey = "";

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
const selectedPane = computed<Pane | null>(() => {
  const tab = currentTab.value;
  return tab?.panes[tab.activePaneIdx] ?? null;
});
watch(selectedPane, (pane) => {
  activePaneRef.value = pane;
}, { immediate: true });

// Drive the OS window title from the active tab's AI session OSC title.
// claude / codex prefix their OSC title with status glyphs (● / ✻) already,
// so we don't add our own — that would double-prefix. Falls back to "AT
// Term" for non-AI tabs and for AI tabs whose OSC title hasn't arrived
// yet. Active session is resolved through the same findSessionInfo path
// the TabBar uses (Tab itself only stores sessionId, not SessionInfo).
const currentActiveSession = computed<SessionInfo | null>(() => {
  const t = currentTab.value;
  if (!t) return null;
  const pane = t.panes[t.activePaneIdx];
  if (!pane?.sessionId) return pane?.lastSeenInfo ?? null;
  return findSessionInfo(pane.sessionId, pane.remote) ?? pane.lastSeenInfo ?? null;
});
watch(
  () => {
    const s = currentActiveSession.value;
    return [s?.type, s?.title] as const;
  },
  ([type, title]) => {
    const next = (type === 'ai' && title) ? title : 'AT Term';
    $platform.system.windowSetTitle?.(next).catch(() => { /* non-desktop platforms */ });
  },
  { immediate: true },
);

// Task state of the active session, surfaced to TitleBar so it can render a
// running indicator (green line at the bottom of the titlebar) when the
// foreground session has work in flight. Returns null when no session/state.
const currentTaskStateForBar = computed(() => currentActiveSession.value?.task_state ?? null);

// In-window title shown in TitleBar's center area. Mirrors the TabBar's
// per-tab title (AI OSC title for ai sessions, cwd basename otherwise) so
// users have a persistent label of the active session even when the tab
// row is scrolled.
const currentTitleForBar = computed<string>(() => {
  const s = currentActiveSession.value;
  if (!s) return '';
  if (s.type === 'ai' && s.title) return s.title;
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, '');
    if (stripped === '') return '/';
    const base = stripped.split('/').pop();
    if (base) return base;
  }
  const first = (s.command || '').split(/\s+/)[0] || '';
  return first.split('/').pop() || first;
});

// Each TerminalView registers a driver-side input sender keyed by sessionId.
// Plugins (Quick Input) route their send() through this map so input rides
// the existing driver SessionConnection — a freshly attached connection
// would be a viewer and the relay would drop its IN frames.
const pluginInputSenders = new Map<string, (text: string) => void>();
provide("atterm:pluginInputSenders", pluginInputSenders);

// TerminalView owns these connections. Plugins must reuse an entry instead of
// opening another /client attachment for the active session.
const pluginSessionConnections = reactive(new Map<string, SessionConnection>()) as Map<string, SessionConnection>;
provide("atterm:pluginSessionConnections", pluginSessionConnections);

const pluginContext = createPluginContext({
  activePane: activePaneRef,
  endpointForPane: endpointFor,
  sessionInfoForPane: paneSessionInfo,
  sessionConnectionForPane: (pane) =>
    pane.sessionId ? pluginSessionConnections.get(pane.sessionId) ?? null : null,
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

// Sessions visible across all current tabs (drives local sweep + remote-discover panel).
const allUsedSessionIds = computed(() => {
  const s = new Set<string>();
  for (const t of tabs.value) for (const p of t.panes) if (p.sessionId) s.add(p.sessionId);
  return s;
});
const openSessionIds = computed(() => Array.from(allUsedSessionIds.value));

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

// snapshotKnownSessions captures id → SessionInfo for everything we currently
// know about. Used before applying a new local session list so
// sweepMissingSessions can stash the last-known info on a pane right before
// nulling its sessionId, keeping the tab label meaningful after a local PTY
// exits or disappears.
function snapshotKnownSessions(): Map<string, SessionInfo> {
  const m = new Map<string, SessionInfo>();
  for (const s of localList.value) m.set(s.id, s);
  for (const s of remoteList.value) m.set(s.id, s);
  return m;
}

function sweepMissingSessions(snapshot?: Map<string, SessionInfo>) {
  const localIds = new Set(localList.value.map((s) => s.id));
  for (const t of tabs.value) {
    for (let i = 0; i < t.panes.length; i++) {
      const p = t.panes[i];
      if (!p.sessionId) continue;
      if (p.remote) continue;
      if (!localIds.has(p.sessionId)) {
        // Stash the SessionInfo we had a moment ago so the TabBar can still
        // show a useful title. Fall back to whatever the pane already cached
        // (covers the case where two consecutive sweeps fire before the host
        // comes back).
        const lastSeenInfo = snapshot?.get(p.sessionId) ?? p.lastSeenInfo;
        t.panes[i] = { sessionId: null, remote: p.remote, lastSeenInfo };
      }
    }
  }
}

function refreshVisibleRemoteSessions() {
  const localIds = new Set(localList.value.map((s) => s.id));
  remoteList.value = remoteRawList.value.filter((s) => !localIds.has(s.id));
}

function applyLocalSessions(sessions: SessionInfo[]) {
  const snap = snapshotKnownSessions();
  // Merge instead of overwrite so an early/stale LIST_RESP frame (e.g. the
  // first frame after executeRestore spawns N sessions but Go has only
  // broadcast the first one) doesn't drop seeded sessions that the sweep
  // would then null. mergeLocalSessions also drains pendingLocalIds for
  // ids Go has now confirmed.
  const merged = mergeLocalSessions(sessions, localList.value, pendingLocalIds);
  localList.value = merged.localList;
  pendingLocalIds.clear();
  for (const id of merged.pending) pendingLocalIds.add(id);
  refreshVisibleRemoteSessions();
  sweepMissingSessions(snap);
  if (status.value !== "ready") status.value = "ready";
}

function applyRemoteSessions(sessions: SessionInfo[]) {
  remoteRawList.value = sessions;
  refreshVisibleRemoteSessions();
  const pruned = pruneStaleRemoteTabs({
    tabs: tabs.value,
    remoteSessions: remoteList.value,
    missingSince: remoteMissingSince,
    nowMs: Date.now(),
    graceMs: REMOTE_STALE_TAB_GRACE_MS,
  });
  if (pruned.removedTabIds.length > 0) {
    tabs.value = pruned.tabs;
    if (currentTabId.value && pruned.removedTabIds.includes(currentTabId.value)) {
      if (tabs.value.length > 0) gotoTab(tabs.value[0].id);
      else location.hash = "";
    }
  }
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

function stopRemotePoll() {
  if (remotePollHandle !== null) {
    window.clearInterval(remotePollHandle);
    remotePollHandle = null;
  }
}

async function pollRemoteSessions() {
  try {
    applyRemoteSessions(await listRemoteSessions());
  } catch {
    // Transient relay/network error — keep the last known list rather than
    // flashing it empty on a single failed poll.
  }
}

// connectRemoteSessionList (re)starts the remote-session list poll and sets the
// attach endpoint. Two independent concerns:
//   - The LIST is read through the Go backend (App.ListRemoteSessions), polled
//     whenever the relay is connected — some networks fingerprint-RST the
//     WKWebView TLS handshake to the relay while Go's TLS passes.
//   - The ATTACH endpoint is the Go loopback proxy (remoteProxy). When it's
//     unavailable the list still shows; you just can't open a remote pane.
function connectRemoteSessionList(relayConnected: boolean, attachEndpoint: Endpoint | null) {
  stopRemotePoll();
  remoteRawList.value = [];
  remoteList.value = [];
  remoteMissingSince.clear();
  remoteEndpoint.value = attachEndpoint;
  if (!relayConnected) return;
  void pollRemoteSessions();
  remotePollHandle = window.setInterval(pollRemoteSessions, 2000);
}

async function refreshRelayConfig() {
  let cfg: { url: string; token: string; connected: boolean; remote_proxy_url?: string } = {
    url: "",
    token: "",
    connected: false,
  };
  try {
    cfg = await getRelayConfig();
  } catch {
    /* keep last known */
  }
  // Attach remote sessions through the Go loopback proxy (remote_proxy_url),
  // not the relay URL directly: the WebView can't TLS-dial the relay on some
  // networks. The token still authenticates the proxied /client stream. List
  // polling is gated only on the relay being connected, independent of the
  // proxy.
  const relayConnected = !!(cfg.connected && cfg.url);
  const proxyUrl = cfg.remote_proxy_url ?? "";
  const attachEndpoint: Endpoint | null = relayConnected && proxyUrl
    ? { url: proxyUrl, session_token: cfg.token }
    : null;
  const key = `${relayConnected}|${attachEndpoint?.url ?? ""}|${attachEndpoint?.session_token ?? ""}`;
  if (key === lastRemoteKey) return;
  lastRemoteKey = key;
  connectRemoteSessionList(relayConnected, attachEndpoint);
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
  if (shells.length === 0) throw new Error(i18nT("app.noShellsFound"));
  const resp = await newSession({
    command: shells[0],
    cwd,
    cols: dims.cols,
    rows: dims.rows,
  });
  // Reflect immediately so PaneGrid finds the endpoint without poll lag.
  // pendingLocalIds keeps this seed alive across an early/stale LIST_RESP
  // frame from Go (see mergeLocalSessions).
  pendingLocalIds.add(resp.session_id);
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
      colRatio: RATIO_DEFAULT,
      rowRatio: RATIO_DEFAULT,
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
          ? i18nT("app.noOtherSessions")
          : i18nT("app.noOtherSessionsWithRelayHint"),
      );
      return;
    }
  }

  const result = transitionLayout(t.layout, t.panes, t.activePaneIdx, dir, t.colRatio, t.rowRatio);
  if (result.noop) {
    showToast(i18nT("app.paneFull"));
    return;
  }

  t.layout = result.layout;
  t.panes = result.panes;
  t.activePaneIdx = result.activePaneIdx;
  t.colRatio = result.colRatio;
  t.rowRatio = result.rowRatio;

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
    showToast(i18nT("app.splitFailed", { message: e?.message ?? String(e) }));
  }
}

function onPickerPick(payload: { sessionId: string; remote: boolean }) {
  const ctx = pickerCtx.value;
  pickerCtx.value = null;
  if (!ctx) return;
  const t = tabs.value.find((tt) => tt.id === ctx.tabId);
  if (!t) return;
  if (t.panes.some((p, i) => i !== ctx.paneIdx && p.sessionId === payload.sessionId)) {
    showToast(i18nT("app.sessionAlreadyInTab"));
    return;
  }
  t.panes[ctx.paneIdx] = { sessionId: payload.sessionId, remote: payload.remote };
}

function onPickerClose() {
  pickerCtx.value = null;
}

async function onRecoveryRestore(picks: RecoveryTabSnapshot[]) {
  const savedActive = recoveryDialogState.value.snapshot?.active_tab_id ?? "";
  recoveryDialogState.value = { open: false, snapshot: null };
  if (picks.length === 0) {
    await startNewTab();
    return;
  }
  await executeRestore(picks, savedActive);
}

async function onRecoveryDiscard() {
  recoveryDialogState.value = { open: false, snapshot: null };
  try {
    await discardRecoverySnapshot();
  } catch (e) {
    console.warn("[recovery] discard failed", e);
  }
  await startNewTab();
}

// executeRestore rebuilds tabs/panes from a snapshot. Serial per tab (and
// per pane): the spawn must finish before we push the Tab so the spawn
// failure paths (pane = empty) land in the correct slot. Parallel would
// race against snapshot mutation and complicate error placement.
async function executeRestore(picks: RecoveryTabSnapshot[], savedActiveTabId: string) {
  const newIds: string[] = [];
  let savedActiveIdx = -1;
  // Resolve the user's real default shell once. Panes whose snapshot has an
  // empty shell restore against this instead of /bin/sh — see
  // buildRestoreSessionReq.
  const defaultShell = (await listShells())[0] ?? "";
  for (let pickIdx = 0; pickIdx < picks.length; pickIdx++) {
    const tab = picks[pickIdx];
    const t: Tab = {
      id: newId(),
      layout: tab.layout,
      activePaneIdx: tab.active_pane_idx,
      colRatio: tab.col_ratio,
      rowRatio: tab.row_ratio,
      panes: [],
    };
    if (tab.id === savedActiveTabId) savedActiveIdx = pickIdx;
    const want = PANE_COUNT[tab.layout];
    for (let i = 0; i < want; i++) {
      const snap = tab.panes.find((p) => p.slot === i);
      if (!snap) {
        t.panes[i] = { sessionId: null, remote: false };
        continue;
      }
      // Remote panes: do NOT fork a new local shell. The session is still
      // alive on the remote host (or will be when the relay reconnects);
      // re-bind the pane to the same session_id and let the remote list
      // push resolve SessionInfo. Until the relay catches up — or if the
      // session is gone — lastSeenInfo keeps the tab label meaningful
      // (matches the local-sweep "disconnected" display).
      if (snap.remote && snap.session_id) {
        t.panes[i] = {
          sessionId: snap.session_id,
          remote: true,
          lastSeenInfo: synthSessionInfoFromSnapshot(snap),
        };
        continue;
      }
      try {
        const dims = predictCellDims(tab.layout);
        const req = buildRestoreSessionReq(snap, dims.cols, dims.rows, defaultShell);
        const resp = await newSession(req);
        t.panes[i] = { sessionId: resp.session_id, remote: false };
        // Seed localList immediately (mirrors spawnLocalShell) so the recovery
        // snapshot can resolve this pane's SessionInfo right away. Without this,
        // the window before the relay's session-list push arrives would persist
        // shell:"" / cwd:"" — corrupting recovery.json and making the NEXT
        // restore fall back to /bin/sh (sh-3.2$). pendingLocalIds protects the
        // seed from being dropped by the FIRST (still-stale) LIST_RESP frame
        // that arrives after a multi-spawn restore.
        pendingLocalIds.add(resp.session_id);
        localList.value = [
          ...localList.value,
          {
            id: resp.session_id,
            command: req.command,
            cwd: req.cwd || "",
            title: snap.title || req.command,
            type: snap.session_type || "",
            cols: dims.cols,
            rows: dims.rows,
            started_at: Math.floor(Date.now() / 1000),
            host_id: localHostID.value,
          },
        ];
        // Resume injection is handled Go-side on the shell's first prompt
        // (see relay_host SetOnFirstPrompt) — reliable, no task-state poll.
      } catch (e) {
        console.warn("[recovery] pane spawn failed", e);
        t.panes[i] = { sessionId: null, remote: false };
      }
    }
    tabs.value.push(t);
    newIds.push(t.id);
  }
  if (savedActiveIdx >= 0 && newIds[savedActiveIdx]) {
    gotoTab(newIds[savedActiveIdx]);
  } else if (newIds.length > 0) {
    gotoTab(newIds[0]);
  }
}

async function onClosePane() {
  const t = currentTab.value;
  if (!t) return;
  closePaneAt(t, t.activePaneIdx);
}

async function closePaneAt(t: Tab, idx: number) {
  const target = t.panes[idx];
  if (target?.sessionId && !target.remote) {
    pendingLocalIds.delete(target.sessionId);
    try { await closeSession(target.sessionId); } catch { /* sweep cleans up */ }
  }
  const r = closePane(t.layout, t.panes, idx, t.colRatio, t.rowRatio);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  t.colRatio = r.colRatio;
  t.rowRatio = r.rowRatio;
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
      pendingLocalIds.delete(p.sessionId);
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
  // If any tab already holds a pane for this session, switch to it and
  // focus the EXACT pane — sidebar clicks on a session in a multi-pane
  // tab should land focus on that pane, not on whichever pane was active
  // in the tab before.
  const loc = findPaneLocation(tabs.value, sessionId);
  if (loc) {
    const t = tabs.value.find((tab) => tab.id === loc.tabId);
    if (t && t.activePaneIdx !== loc.paneIdx) {
      t.activePaneIdx = loc.paneIdx;
    }
    gotoTab(loc.tabId);
    return;
  }
  const id = newId();
  tabs.value.push({
    id,
    layout: "single",
    panes: [{ sessionId, remote: true }],
    activePaneIdx: 0,
    colRatio: RATIO_DEFAULT,
    rowRatio: RATIO_DEFAULT,
  });
  gotoTab(id);
}

function focusSessionFromNotification(data: unknown) {
  const payload = data as { session_id?: unknown; sessionId?: unknown } | null;
  const sessionId =
    typeof payload?.session_id === "string"
      ? payload.session_id
      : typeof payload?.sessionId === "string"
        ? payload.sessionId
        : "";
  if (!sessionId) return;
  const loc = findPaneLocation(tabs.value, sessionId);
  if (!loc) return;
  const t = tabs.value.find((tab) => tab.id === loc.tabId);
  if (!t) return;
  t.activePaneIdx = loc.paneIdx;
  gotoTab(loc.tabId);
  void $platform.system.windowUnminimize?.();
  void $platform.system.windowShow?.();
}

const tabSummaries = computed(() =>
  tabs.value.map((t) => {
    const active = t.panes[t.activePaneIdx];
    const info = active?.sessionId ? findSessionInfo(active.sessionId, active.remote) ?? null : null;
    // Local sessions can disappear before the tab is closed; surface the
    // stashed lastSeenInfo so the label stays meaningful after the sweep.
    const fallback = !info && active?.lastSeenInfo ? active.lastSeenInfo : null;
    return {
      id: t.id,
      layout: t.layout,
      activeSession: info ?? fallback,
      activeRemote: !!active?.remote,
      paneCount: t.panes.length,
      disconnected: !info && !!fallback,
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
    onToggleTaskSidebar: () => setSidebarCollapsedAndPersist(!sidebarCollapsed.value),
  },
  { bindings: shortcutBindings },
);

// Persists tab/pane structure (+ AI sid captures) so a crash or unclean exit
// can rebuild the workspace on next launch. cwd/title heartbeat is a
// follow-up; the structural watch covers the v1 use case.
const recovery = useRecoverySnapshot({
  tabs,
  currentTabId,
  localHostID,
  // Look up both lists — restricting to local would null out the host_id
  // / title / cwd we need to persist for remote panes (so the next launch
  // can re-bind them instead of forking a fresh local shell).
  sessionInfoFor: (sid: string) =>
    findSessionInfo(sid, false) ?? findSessionInfo(sid, true),
});

watch([tabs, currentTabId], () => {
  if (tabs.value.length === 0) return;
  if (currentTabId.value && tabs.value.find((t) => t.id === currentTabId.value)) return;
  gotoTab(tabs.value[0].id);
});

onMounted(async () => {
  try {
    sidebarCollapsed.value = await getTaskSidebarCollapsed();
  } catch {
    /* default = expanded (false) */
  }
  quitListenerOff = $platform.events.on('before-close', handleBeforeClose);
  notificationClickListenerOff = $platform.events.on('notification:click', focusSessionFromNotification);
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
  // Track which boot step is running so a thrown error pins down the call
  // site for the user (and the logs) — without this, the catch below collapses
  // five independent calls into one opaque "<DOMException msg>" in the title
  // bar with no way to tell which one fired.
  let bootStage = "";
  let recoverySnap: RecoverySnapshot = {
    version: 1,
    host_id: "",
    clean_shutdown: false,
    saved_at_unix: 0,
    tabs: [],
  };
  let recoveryEnabled = true;
  try {
    bootStage = "refreshTerminalTheme";
    await refreshTerminalTheme();
    bootStage = "getEndpoint";
    localEndpoint.value = await getEndpoint();
    bootStage = "getHostInfo";
    const info = await getHostInfo();
    localHostID.value = info.host_id;
    localHost.value = info.host;
    bootStage = "loadRecoverySnapshot";
    recoverySnap = await loadRecoverySnapshot();
    recoveryEnabled = await getRecoveryDialogEnabled();
    bootStage = "connectLocalSessionList";
    connectLocalSessionList(localEndpoint.value);
    bootStage = "refreshRelayConfig";
    await refreshRelayConfig();
  } catch (e: any) {
    const name = e?.name ?? "Error";
    const msg = e?.message ?? String(e);
    console.error(`[boot] step "${bootStage}" failed`, {
      name,
      message: msg,
      stack: e?.stack,
    });
    status.value = "error";
    errorMsg.value = `${bootStage}: ${name}: ${msg}` || i18nT("app.wailsBindingsUnavailable");
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

  // Auto-start guarded by try/catch: a stray throw here (e.g. a Wails-side
  // shape regression making `recoverySnap.tabs` null) used to escape onMounted
  // and leave the app stuck on the "starting first session" message with no
  // surfaced error. Funnel any failure to errorMsg so the TitleBar shows it
  // instead of failing silently.
  try {
    if (!autoStarted && tabs.value.length === 0) {
      autoStarted = true;
      const hasRecovery = recoveryEnabled && (recoverySnap?.tabs?.length ?? 0) > 0;
      if (hasRecovery) {
        // Dialog handlers (onRecoveryRestore / onRecoveryDiscard) decide
        // whether to spawn restored panes or fall back to startNewTab — so
        // we deliberately do NOT call startNewTab() here.
        recoveryDialogState.value = { open: true, snapshot: recoverySnap };
      } else {
        startNewTab();
      }
    }
  } catch (e: any) {
    const name = e?.name ?? "Error";
    const msg = e?.message ?? String(e);
    console.error("[boot] auto-start failed", { name, message: msg, stack: e?.stack });
    status.value = "error";
    errorMsg.value = `autoStart: ${name}: ${msg}`;
  }
});

onUnmounted(() => {
  quitListenerOff?.();
  quitListenerOff = null;
  notificationClickListenerOff?.();
  notificationClickListenerOff = null;
  window.removeEventListener("hashchange", syncRoute);
  localSessionListConn?.detach();
  stopRemotePoll();
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
      :current-title="currentTitleForBar"
      :current-task-state="currentTaskStateForBar"
      @open-remote="openRemoteFromTitleBar"
      @open-settings="showSettings = true"
    />
    <PasteImagePreviewHost />
    <PasteFilePreviewHost />

    <div v-if="authError" class="auth-error-banner" role="alert">
      <span class="auth-error-msg">{{ authErrorMessage }}</span>
      <button class="auth-error-action" @click="openSettingsRelay">{{ i18nT("app.openSettings") }}</button>
      <button class="auth-error-dismiss" @click="authError = null" :aria-label="i18nT('app.dismiss')">×</button>
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
      <TaskSidebar
        :collapsed="sidebarCollapsed"
        :by-host="sessions.byHost.value"
        :primary-state-for-host="sessions.primaryStateForHost"
        :completed-seen="sessions.completedSeen.value"
        :total-unread="sessions.totalUnread.value"
        :by-state-groups="sessions.byState.value"
        :active-session-id="activePaneRef?.sessionId ?? null"
        :open-session-ids="openSessionIds"
        :local-host-id="localHostID"
        :local-host="localHost"
        @update:collapsed="setSidebarCollapsedAndPersist"
        @open="onSidebarOpen"
        @close="onSidebarClose"
        @markSeen="onMarkSeen"
      />
      <main class="main">
        <template v-if="localEndpoint">
          <div v-if="tabs.length === 0" class="empty">
            {{ i18nT("app.startingFirstSession") }}
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
            @update:col-ratio="(r) => { t.colRatio = r; }"
            @update:row-ratio="(r) => { t.rowRatio = r; }"
          />
        </template>
        <div v-if="toast" class="toast">{{ toast }}</div>
      </main>
      <template v-if="rightPanelHasPlugin">
        <button class="panel-toggle" @click="togglePanel" :title="panelCollapsed ? i18nT('app.showPanel') : i18nT('app.hidePanel')">
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
    <RecoveryDialog
      v-if="recoveryDialogState.snapshot"
      :open="recoveryDialogState.open"
      :snapshot="recoveryDialogState.snapshot"
      @restore="onRecoveryRestore"
      @discard="onRecoveryDiscard"
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
