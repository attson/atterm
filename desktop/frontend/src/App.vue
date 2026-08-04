<script lang="ts" setup>
import { computed, onMounted, onUnmounted, provide, reactive, ref, watch } from "vue";
import TabBar from "./components/TabBar.vue";
import TitleBar from "./components/TitleBar.vue";
import TaskSidebar from "./components/TaskSidebar.vue";
import { useWindowMaximized } from "./composables/useWindowMaximized";
import PaneGrid from "./components/PaneGrid.vue";
import AdminPanel from "./components/AdminPanel.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import SessionPickerDialog from "./components/SessionPickerDialog.vue";
import NewSshDialog from "./components/NewSshDialog.vue";
import SshHostsPanel from "./components/SshHostsPanel.vue";
import ConfirmQuitDialog from "./components/ConfirmQuitDialog.vue";
import StartupFatalPanel from "./components/StartupFatalPanel.vue";
import ConfirmCloseSessionDialog from "./components/ConfirmCloseSessionDialog.vue";
import RecoveryDialog from "./components/RecoveryDialog.vue";
import ShortcutHints from "./components/ShortcutHints.vue";
import PasteImagePreviewHost from "./components/PasteImagePreviewHost.vue";
import PasteFilePreviewHost from "./components/PasteFilePreviewHost.vue";
import PluginHost from "./plugins/PluginHost.vue";
import TranslatePanelHost from "./plugins/translate/TranslatePanelHost.vue";
import { createPluginContext } from "./plugins/usePluginContext";
import { usePluginConfigStore } from "./plugins/configStore";
import { sendInputToSession } from "./lib/sendInput";
import { applyTabReorder } from "./lib/tabReorder";
import { computeCloseTabState } from "./lib/closeTabOptimistic";
import { tabsToAutoCloseOnExit } from "./lib/autoCloseTab";
import { classifySSHRestore } from "./lib/sshRestore";
// Plugin theme palettes (CSS vars). Loaded in main bundle so the panel
// toggle and Quick Input toolbar can read --ed-* vars even when the
// file-explorer chunk is not yet loaded.
import "./plugins/fileExplorer/theme.css";
import { setupMeasureProbe, teardownMeasureProbe, predictCellDims } from "./lib/cellDimsProbe";
import { usePluginPanel } from "./composables/usePluginPanel";
import { usePlatform } from './platform'
const $platform = usePlatform()
const caps = $platform.caps
import {
  closeSession,
  confirmQuit,
  getEndpoint,
  getStartupError,
  getHostInfo,
  getRelayConfig,
  getCommandNotifyThresholdSeconds,
  getTerminalThemePreference,
  getUpdateState,
  listShells,
  newSession,
  newSshSessionByID,
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
import type { Endpoint, RelayConfig, RelayMe, StartupError, UpdateState } from "./lib/api";
import type { RemoteSession } from "./platform/types";
import { SessionListConnection, type SessionConnection, type SessionInfo } from "./lib/connection";
import { mergeLocalSessions } from "./lib/localListMerge";
import { pruneStaleRemoteTabs } from "./lib/remoteTabCleanup";
import { PANE_COUNT, type LayoutKind, type Pane, type Tab, type SplitDir } from "./lib/types";
import { RATIO_DEFAULT, closePane, findPaneLocation, focusNeighbor, transitionLayout } from "./lib/layout";
import { useTerminalShortcuts, type SplitMode } from "./composables/useTerminalShortcuts";
import { useSessions } from "./composables/useSessions";
import { useRecoverySnapshot } from "./composables/useRecoverySnapshot";
import { useSessionPins } from "./composables/useSessionPins";
import { useSessionSelection } from "./composables/useSessionSelection";
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
import { loadSnapshot, saveSnapshot, parseHashSid, formatHash, type WebTabsSnapshot } from "./lib/webTabsSnapshot";

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
const emptyMainMessage = computed(() =>
  caps.localPty ? i18nT("app.startingFirstSession") : i18nT("app.waitingForSessionSelection"),
);

function openSettingsRelay() {
  settingsInitialTab.value = "relay";
  showSettings.value = true;
}

function openSettingsAccount() {
  settingsInitialTab.value = "account";
  showSettings.value = true;
}

// Track which tab to open when Settings is opened programmatically.
const settingsInitialTab = ref<"general" | "account" | "relay" | "logging" | "updates" | undefined>(undefined);

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

// Used by executeRestore for pin migration (rename oldSid -> new session_id
// on respawn) and by the sidebar pin toggle. Hoisted to setup scope like the
// other composables here — pinnedIds is module-scoped internally, so calling
// useSessionPins() from inside executeRestore would have been functionally
// equivalent, but this matches the rest of the file's style.
const pins = useSessionPins();

// Multi-select state for the sidebar's merge / batch-close actions
// (module-scoped internally, same pattern as useSessionPins above).
const sel = useSessionSelection();

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

function requestCloseSession(s: RemoteSession) {
  if (!shouldConfirmCloseSession(s)) {
    onSidebarClose(s);
    return;
  }
  openCloseSessionConfirm([s], () => onSidebarClose(s));
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
const startupFatal = ref<StartupError | null>(null);
const starting = ref(false);
const showSettings = ref(false);
const toast = ref<string>("");

// Current relay identity, fetched once at boot (see onMounted). Exposed via
// defineExpose so tests can drive the isAdmin-transition watcher below
// without a second live fetchMe() call.
const me = ref<RelayMe | null>(null);
const isAdmin = computed(() => me.value?.is_admin === true);
// Whether the main area shows AdminPanel instead of the pane-grid tabs.
// Not persisted (see spec §4.10) — always starts closed.
const adminViewOpen = ref(false);
// If admin rights are revoked mid-session, drop out of the admin view
// automatically rather than leaving a non-admin staring at a stale panel.
watch(isAdmin, (v) => {
  if (!v) adminViewOpen.value = false;
});

const quitDialogOpen = ref(false);
const showSshDialog = ref(false);
const showSshHosts = ref(false);
const pendingCloseSession = ref<RemoteSession | null>(null);
const pendingCloseSessions = ref<RemoteSession[]>([]);
let pendingCloseAction: (() => void | Promise<void>) | null = null;
let quitListenerOff: (() => void) | null = null;
let notificationClickListenerOff: (() => void) | null = null;

function handleBeforeClose() {
  // Best-effort final persist so a clean quit always lands the latest state.
  // (Sleep / force-quit are covered by the composable's periodic safety flush.)
  recovery?.flushNow();
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

function isCloseRiskySession(s: RemoteSession): boolean {
  return s.type === "ai" || s.task_state === "running";
}

function shouldConfirmCloseSession(s: RemoteSession): boolean {
  return !isOpenRemoteSession(s.session_id) && isCloseRiskySession(s);
}

function sessionCloseTitle(s: RemoteSession): string {
  return s.current_command || s.title || s.session_id.slice(0, 8);
}

const pendingCloseTitle = computed(() => {
  if (pendingCloseSessions.value.length > 1) return "";
  const s = pendingCloseSession.value;
  return s ? sessionCloseTitle(s) : "";
});

const pendingCloseIsAi = computed(() =>
  pendingCloseSessions.value.some((s) => s.type === "ai"),
);
const pendingCloseIsRunning = computed(() =>
  pendingCloseSessions.value.some((s) => s.task_state === "running"),
);
const pendingCloseIsRemote = computed(() =>
  pendingCloseSessions.value.length > 0 && pendingCloseSessions.value.every((s) => isOpenRemoteSession(s.session_id)),
);

function clearPendingCloseSession() {
  pendingCloseSession.value = null;
  pendingCloseSessions.value = [];
  pendingCloseAction = null;
}

function openCloseSessionConfirm(sessionsToClose: RemoteSession[], action: () => void | Promise<void>) {
  pendingCloseSessions.value = sessionsToClose;
  pendingCloseSession.value = sessionsToClose[0] ?? null;
  pendingCloseAction = action;
}

function confirmCloseSession() {
  const action = pendingCloseAction;
  clearPendingCloseSession();
  void action?.();
}

function cancelCloseSession() {
  clearPendingCloseSession();
}

function isOpenRemoteSession(sessionId: string): boolean {
  const loc = findPaneLocation(tabs.value, sessionId);
  if (!loc) return false;
  const t = tabs.value.find((tab) => tab.id === loc.tabId);
  return t?.panes[loc.paneIdx]?.remote === true;
}

function closeCandidateForPane(pane: Pane): RemoteSession | null {
  const id = pane.sessionId;
  if (!id) return pane.lastSeenInfo ? adaptSession(pane.lastSeenInfo) : null;
  const info = findSessionInfo(id, pane.remote) ?? findSessionInfo(id, !pane.remote) ?? pane.lastSeenInfo;
  return info ? adaptSession(info) : null;
}

function riskyCloseCandidatesForPanes(panes: Pane[]): RemoteSession[] {
  return panes
    .filter((pane) => !pane.remote)
    .map(closeCandidateForPane)
    .filter((s): s is RemoteSession => !!s && isCloseRiskySession(s));
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
let remoteSessionListConn: SessionListConnection | null = null;
let remotePollHandle: number | null = null;
// Dedup key for refreshRelayConfig: "connected|attachUrl|token". Avoids
// restarting the remote poll / re-setting the endpoint when nothing changed.
let lastRemoteKey = "";

// One-shot off-screen Terminal+FitAddon used as a measure probe. We resize
// its parent div to a target cell size, call FitAddon.proposeDimensions(),
// and use the result to spawn the PTY at the same cols/rows xterm.js would
// pick on the real cell. Goal: avoid the SIGWINCH between fork and first
// prompt that triggers zsh's PROMPT_EOL_MARK ('%') for some prompt themes.
// Setup/teardown + predictCellDims live in lib/cellDimsProbe.ts so this SFC
// stays focused on tab/pane orchestration.

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : "tab-" + Math.random().toString(36).slice(2);

const currentTab = computed<Tab | null>(
  () => tabs.value.find((t) => t.id === currentTabId.value) ?? null,
);

// Keep a Ref (not ComputedRef) so it satisfies PluginContextInputs.activePane.
const activePaneRef = ref<Pane | null>(null);
const taskSidebarRef = ref<InstanceType<typeof TaskSidebar> | null>(null);
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

const {
  panelWidth,
  panelCollapsed,
  togglePanel,
  onPanelResizeDown,
  rightPanelHasPlugin,
  fileExplorerTheme,
} = usePluginPanel({ terminalThemeId: currentTerminalThemeID });

// Sessions visible across all current tabs (drives local sweep + remote-discover panel).
const allUsedSessionIds = computed(() => {
  const s = new Set<string>();
  for (const t of tabs.value) for (const p of t.panes) if (p.sessionId) s.add(p.sessionId);
  return s;
});
const openSessionIds = computed(() => Array.from(allUsedSessionIds.value));

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
  // Tabs where a local session just disappeared this sweep — candidates for
  // auto-close once we confirm they hold no other live session.
  const clearedTabIds: string[] = [];
  for (const t of tabs.value) {
    let clearedHere = false;
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
        clearedHere = true;
      }
    }
    if (clearedHere) clearedTabIds.push(t.id);
  }

  // Auto-close on exit: a terminal (local shell or SSH) that exited leaves its
  // pane empty above. If that leaves a tab with no live session in ANY pane
  // (local or remote), close the whole tab instead of stranding an empty
  // "[empty pane]" placeholder. Only tabs that just lost a session this sweep
  // are considered, so freshly-opened empty tabs and mid-restore tabs are not
  // swept away.
  for (const tabId of tabsToAutoCloseOnExit(tabs.value, localIds, clearedTabIds)) {
    closeTab(tabId);
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
  remoteSessionListConn?.detach();
  remoteSessionListConn = null;
  if (remotePollHandle !== null) {
    window.clearInterval(remotePollHandle);
    remotePollHandle = null;
  }
}

function onRemotePrefsChanged() {
  $platform.events.emit("prefs:remote-changed", undefined);
}

function connectRemoteSessionListWS(endpoint: Endpoint) {
  stopRemotePoll();
  remoteSessionListConn = new SessionListConnection(endpoint, {
    onSessions: applyRemoteSessions,
    onPrefsChanged: onRemotePrefsChanged,
    onStatus: (s) => {
      if (s === "error") status.value = "error";
    },
  });
  remoteSessionListConn.attach();
}

async function pollRemoteSessions() {
  try {
    applyRemoteSessions(await listRemoteSessions());
  } catch {
    // Transient relay/network error — keep the last known list rather than
    // flashing it empty on a single failed poll.
  }
}

// HTTP fallback for non-Wails clients when the `/client-sessions` WS cannot
// be established. Skips the Wails-specific listRemoteSessions() and hits the
// platform bridge's HTTP `/api/sessions` directly. The web
// platform returns RemoteSession[] with `session_id` alias, but the raw
// SessionInfo `id` field is preserved from /api/sessions — remap it so
// applyRemoteSessions (which reads `.id`) sees a consistent shape.
async function pollRemoteSessionsViaPlatform() {
  const rs = await $platform.sessions.listRemoteSessions();
  // The web platform bridge (desktop/frontend/src/platform/web.ts) preserves
  // the original `id` field from /api/sessions and adds a `session_id`
  // alias for the RemoteSession shape. Recover the SessionInfo view by
  // aliasing session_id back to id (harmless when both are already
  // present).
  const asSessionInfo: SessionInfo[] = rs.map((s) => {
    const raw = s as unknown as SessionInfo & { session_id?: string };
    return { ...raw, id: raw.id ?? raw.session_id ?? "" };
  });
  applyRemoteSessions(asSessionInfo);
}

async function refreshPlatformRelayState(): Promise<boolean> {
  const relayCfg = await $platform.relay.load();
  const endpoint = relayCfg ? buildWebRemoteEndpoint(relayCfg) : null;
  remoteEndpoint.value = endpoint;
  if (!endpoint) {
    stopRemotePoll();
    remoteRawList.value = [];
    remoteList.value = [];
    return false;
  }
  connectRemoteSessionListWS(endpoint);
  return true;
}

function startPlatformRemotePoll(): void {
  if (remoteSessionListConn) return;
  stopRemotePoll();
  remotePollHandle = window.setInterval(() => {
    void pollRemoteSessionsViaPlatform();
  }, 3000);
}

// connectRemoteSessionList (re)starts the remote-session list stream and sets the
// attach endpoint. Two independent concerns:
//   - The LIST is read through /client-sessions via the Go loopback proxy when
//     available, with App.ListRemoteSessions polling as fallback.
//   - The ATTACH endpoint is the Go loopback proxy (remoteProxy). When it's
//     unavailable the list still shows; you just can't open a remote pane.
function connectRemoteSessionList(relayConnected: boolean, attachEndpoint: Endpoint | null) {
  stopRemotePoll();
  remoteRawList.value = [];
  remoteList.value = [];
  remoteMissingSince.clear();
  remoteEndpoint.value = attachEndpoint;
  if (!relayConnected) return;
  if (attachEndpoint) {
    connectRemoteSessionListWS(attachEndpoint);
    return;
  }
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

function refreshDesktopRelayConfig() {
  if (caps.wailsBindings) {
    void refreshRelayConfig();
  }
}

function onSettingsClose() {
  showSettings.value = false;
  settingsInitialTab.value = undefined;
  refreshDesktopRelayConfig();
}

// buildWebRemoteEndpoint adapts the RelayConfig the web platform bridge
// returns (platform/web.ts relay.load) into the ws(s) Endpoint that
// SessionConnection dials directly. Unlike desktop's connectRemoteSessionList,
// there is no Go loopback proxy on web — the browser attaches straight to
// the relay. `cfg.url` (RelayConfig.baseURL) is normally empty for the common
// case of the web app being served by the relay itself (same-origin login):
// apiFetch treats an empty baseURL as "relative to location.origin" (see
// web/src/shared/api/client.ts), so mirror that here by falling back to the
// current page's origin with http(s) mapped to ws(s). A non-empty cfg.url
// (cross-origin pairing case) is converted the same way. homeInstanceURL
// takes priority when set (multi-instance realm routing), matching
// web/src/shared/ws/client-conn.ts's wsUrl() precedence.
function buildWebRemoteEndpoint(cfg: RelayConfig): Endpoint | null {
  if (!cfg.token) return null;
  const httpBase = cfg.homeInstanceURL || cfg.url;
  if (!httpBase) {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    return { url: `${proto}//${location.host}`, session_token: cfg.token };
  }
  try {
    const u = new URL(httpBase);
    const proto = u.protocol === "https:" ? "wss:" : "ws:";
    return { url: `${proto}//${u.host}`, session_token: cfg.token };
  } catch {
    return null;
  }
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
  // Clicking a session tab always means "I want my terminal back" — close
  // the admin view without touching tabs/currentTabId/hash otherwise.
  adminViewOpen.value = false;
}

function onTabReorder(fromId: string, targetId: string, position: "before" | "after") {
  tabs.value = applyTabReorder(tabs.value, fromId, targetId, position);
}

function findSessionInfo(sid: string, remote: boolean): SessionInfo | undefined {
  return (remote ? remoteList.value : localList.value).find((s) => s.id === sid);
}

function activeLocalPaneCwd(tab: Tab): string {
  const pane = tab.panes[tab.activePaneIdx];
  if (!pane?.sessionId || pane.remote) return "";
  return findSessionInfo(pane.sessionId, false)?.cwd ?? "";
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
    const spawnCwd = currentTab.value ? activeLocalPaneCwd(currentTab.value) : "";
    const sid = await spawnLocalShell(spawnCwd, predictCellDims("single"));
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

// openSshTab seeds the freshly-adopted SSH session into localList (it is an
// AdoptSession-backed session on this host, so it surfaces in the local list)
// and opens it in a new single-pane tab — mirroring startNewTab's tail.
function openSshTab(sessionId: string) {
  pendingLocalIds.add(sessionId);
  if (!localList.value.some((s) => s.id === sessionId)) {
    const dims = predictCellDims("single");
    localList.value = [
      ...localList.value,
      {
        id: sessionId,
        command: "ssh",
        cwd: "",
        title: "ssh",
        cols: dims.cols,
        rows: dims.rows,
        started_at: Math.floor(Date.now() / 1000),
        host_id: localHostID.value,
      },
    ];
  }
  const id = newId();
  tabs.value.push({
    id,
    layout: "single",
    panes: [{ sessionId, remote: false }],
    activePaneIdx: 0,
    colRatio: RATIO_DEFAULT,
    rowRatio: RATIO_DEFAULT,
  });
  gotoTab(id);
}

function onSshConnected(sessionId: string) {
  showSshDialog.value = false;
  showSshHosts.value = false;
  openSshTab(sessionId);
}

async function onSplit(dir: SplitDir, mode: SplitMode) {
  const t = currentTab.value;
  if (!t) return;
  const spawnCwd = mode === "new" ? activeLocalPaneCwd(t) : "";

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

  // New shell inherits the active local pane's cwd when known; empty cwd
  // still lets the Go side fall back to HOME for remote/empty/unknown panes.
  // Cols/rows are predicted via the FitAddon probe so the PTY is born at
  // the same dimensions xterm.js will land on after fit(); without this
  // there's a SIGWINCH between fork and first prompt that some zsh themes
  // turn into a stray PROMPT_EOL_MARK ('%').
  try {
    const sid = await spawnLocalShell(spawnCwd, predictCellDims(result.layout));
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
    if (caps.localPty) await startNewTab();
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
  if (caps.localPty) await startNewTab();
}

// executeRestore rebuilds tabs/panes from a snapshot. Serial per tab (and
// per pane): the spawn must finish before we push the Tab so the spawn
// failure paths (pane = empty) land in the correct slot. Parallel would
// race against snapshot mutation and complicate error placement.
async function executeRestore(picks: RecoveryTabSnapshot[], savedActiveTabId: string) {
  const newIds: string[] = [];
  // Pin migration: local panes get a fresh session_id on respawn, which
  // would strand the previous generation's pin ids in config. Capture the
  // old sid from the snapshot and hand it to pins.rename after each spawn.
  // Remote panes keep their sid across restarts, so remote entries in the
  // pin set are already correct — no rename needed on that branch.
  // See 2026-07-23-pinned-session-recovery-design.md §4.3.
  // pins itself is the setup-scope instance (see top of <script setup>); wait
  // for its initial load here so isPinned(oldSid) below can't race the
  // fire-and-forget getPinnedSessionIds() kicked off elsewhere.
  await pins.ready();
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
      // Platforms without a local PTY (e.g. the web build) can't fork a
      // local shell at all — leave the pane empty instead of calling
      // newSession, matching how a snapshot-less boot renders there.
      if (!caps.localPty) {
        t.panes[i] = { sessionId: null, remote: false };
        continue;
      }
      // SSH sessions must NOT be forked as a local shell (that would spawn a
      // bogus "ssh" process). Saved-host SSH reconnects by host id; ad-hoc SSH
      // can't reconnect (used-once creds) so it's left empty with a hint.
      const sshKind = classifySSHRestore(snap);
      if (sshKind.kind === "reconnect") {
        try {
          const resp = await newSshSessionByID(sshKind.hostId);
          t.panes[i] = { sessionId: resp.session_id, remote: false };
          pendingLocalIds.add(resp.session_id);
          localList.value = [
            ...localList.value,
            {
              id: resp.session_id,
              command: snap.title || "ssh",
              cwd: "",
              title: snap.title || "ssh",
              type: "shell",
              cols: predictCellDims(tab.layout).cols,
              rows: predictCellDims(tab.layout).rows,
              started_at: Math.floor(Date.now() / 1000),
              host_id: localHostID.value,
            },
          ];
        } catch (e) {
          // Host deleted / key missing / TOFU on reconnect → leave empty with a
          // hint instead of stranding a broken pane.
          t.panes[i] = {
            sessionId: null,
            remote: false,
            lastSeenInfo: synthSessionInfoFromSnapshot({
              ...snap,
              title: (snap.title || "ssh") + " — reconnect failed, open it again",
            }),
          };
          void e;
        }
        continue;
      }
      if (sshKind.kind === "adhoc") {
        // Ad-hoc SSH: credentials were used-once; cannot reconnect.
        t.panes[i] = {
          sessionId: null,
          remote: false,
          lastSeenInfo: synthSessionInfoFromSnapshot({
            ...snap,
            title: (snap.title || "ssh") + " — SSH disconnected, reconnect to resume",
          }),
        };
        continue;
      }
      try {
        // oldSid: previous generation's session_id, saved by useRecoverySnapshot
        // for local panes (Task 2). Empty when the snapshot pre-dates that
        // change — skip pin migration in that case, matches old behavior.
        const oldSid = snap.session_id || "";
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
        if (oldSid && pins.isPinned(oldSid)) {
          pins.rename(oldSid, resp.session_id);
        }
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
  // Persist the migrated pin set now (rather than waiting for the 300ms
  // debounce). The sidebar's first repaint after recovery reads pinnedIds
  // from the reactive Set (already up to date), but a fast-follow force
  // quit before the debounce fires would strand the new sids out of
  // config. Idempotent no-op when no rename happened.
  await pins.flushNow();
}

async function onClosePane() {
  const t = currentTab.value;
  if (!t) return;
  requestClosePane(t, t.activePaneIdx);
}

function requestClosePane(t: Tab, idx: number) {
  const target = t.panes[idx];
  const risky = target ? riskyCloseCandidatesForPanes([target]) : [];
  if (risky.length > 0) {
    openCloseSessionConfirm(risky, () => closePaneAt(t, idx));
    return;
  }
  closePaneAt(t, idx);
}

function closePaneAt(t: Tab, idx: number, opts?: { detachOnly?: boolean }) {
  const target = t.panes[idx];
  // In single layout closePane() keeps the pane's sessionId and returns
  // closeTab:true; let closeTab() below handle the local kill so we don't
  // duplicate the RPC or the optimistic localList drop.
  const cascadeToCloseTab = t.layout === "single";
  if (!cascadeToCloseTab && target?.sessionId && !target.remote && !opts?.detachOnly) {
    optimisticallyDropLocalSession(target.sessionId);
  }
  const r = closePane(t.layout, t.panes, idx, t.colRatio, t.rowRatio);
  t.layout = r.layout;
  t.panes = r.panes;
  t.activePaneIdx = r.activePaneIdx;
  t.colRatio = r.colRatio;
  t.rowRatio = r.rowRatio;
  if (r.closeTab) {
    closeTab(t.id, opts);
  }
}

// Optimistic close for a single local session: drop from pending + localList
// right away, then fire the Go CloseSession RPC in the background. The next
// LIST_RESP will reconcile if anything survived on the Go side.
function optimisticallyDropLocalSession(sid: string) {
  pendingLocalIds.delete(sid);
  const filtered = localList.value.filter((s) => s.id !== sid);
  if (filtered.length !== localList.value.length) {
    localList.value = filtered;
    refreshVisibleRemoteSessions();
  }
  closeSession(sid).catch(() => undefined);
}

function closeTab(id: string, opts?: { detachOnly?: boolean }) {
  const plan = computeCloseTabState(tabs.value, localList.value, currentTabId.value, id, opts);
  if (!plan) return;
  for (const sid of plan.localIdsToClose) pendingLocalIds.delete(sid);
  // Optimistic UI: drop the tab and its local sessions from the visible state
  // right away so the sidebar and tabbar reflect the intent before the Go
  // CloseSession RPCs (and the follow-up LIST_RESP) round-trip. If a close
  // somehow fails on the Go side, the next LIST_RESP will resurface it —
  // better than sitting on a blocked await for hundreds of ms.
  tabs.value = plan.nextTabs;
  if (plan.nextLocalList !== localList.value) {
    localList.value = plan.nextLocalList;
    refreshVisibleRemoteSessions();
  }
  if (plan.wasCurrent) {
    if (tabs.value.length > 0) gotoTab(tabs.value[0].id);
    else location.hash = "";
  }
  for (const sid of plan.localIdsToClose) {
    closeSession(sid).catch(() => undefined);
  }
}

function requestCloseTab(id: string) {
  const t = tabs.value.find((tab) => tab.id === id);
  const risky = t ? riskyCloseCandidatesForPanes(t.panes) : [];
  if (risky.length > 0) {
    openCloseSessionConfirm(risky, () => closeTab(id));
    return;
  }
  closeTab(id);
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

function layoutForCount(n: number): LayoutKind | null {
  if (n === 1) return "single";
  if (n === 2) return "vertical";
  if (n === 3 || n === 4) return "grid2x2";
  return null;
}

function paneLocationForSession(id: string): { tabId: string; paneIdx: number } | null {
  return findPaneLocation(tabs.value, id);
}

function tabIndexById(tabId: string): number {
  const i = tabs.value.findIndex((t) => t.id === tabId);
  return i < 0 ? 0 : i + 1; // 1-based for display
}

async function mergeSelectedIntoTab(): Promise<void> {
  const ids = Array.from(sel.selectedIds.value);
  const layout = layoutForCount(ids.length);
  if (!layout) return; // UI already disables merge above 4 selections; belt-and-braces guard.
  // Detach any panes currently holding these ids WITHOUT killing local
  // shells — the sessions are moving into the merged tab, not closing.
  // closePaneAt cascades closeTab() when the last pane leaves a tab.
  for (const id of ids) {
    const loc = findPaneLocation(tabs.value, id);
    if (!loc) continue;
    const t = tabs.value.find((tt) => tt.id === loc.tabId);
    if (!t) continue;
    await closePaneAt(t, loc.paneIdx, { detachOnly: true });
  }
  const capacity = PANE_COUNT[layout];
  const panes: Pane[] = new Array(capacity).fill(null).map((_, i) => (
    i < ids.length
      ? { sessionId: ids[i], remote: true }
      : { sessionId: null, remote: false }
  ));
  const newTab: Tab = {
    id: newId(),
    layout,
    panes,
    activePaneIdx: 0,
    colRatio: RATIO_DEFAULT,
    rowRatio: RATIO_DEFAULT,
  };
  tabs.value.push(newTab);
  gotoTab(newTab.id);
  sel.clear();
}

function sessionInfoForOpenId(id: string): RemoteSession | null {
  const remote = isOpenRemoteSession(id);
  const info = findSessionInfo(id, remote) ?? findSessionInfo(id, !remote);
  return info ? adaptSession(info) : null;
}

function closeSelectedOpen(): void {
  const opens = allUsedSessionIds.value;
  const ids = Array.from(sel.selectedIds.value).filter((id) => opens.has(id));
  const risky = ids
    .map(sessionInfoForOpenId)
    .filter((s): s is RemoteSession => !!s && isCloseRiskySession(s));
  if (risky.length > 0) {
    openCloseSessionConfirm(risky, () => performCloseSelectedOpen(ids));
    return;
  }
  void performCloseSelectedOpen(ids);
}

async function performCloseSelectedOpen(ids: string[]): Promise<void> {
  for (const id of ids) {
    const loc = findPaneLocation(tabs.value, id);
    if (!loc) continue;
    const t = tabs.value.find((tt) => tt.id === loc.tabId);
    if (!t) continue;
    await closePaneAt(t, loc.paneIdx);
  }
  sel.clear();
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

// Total known sessions across every host the sidebar aggregates — local +
// remote (relay wins on ID collision, so shared sessions aren't
// double-counted). Matches what the user sees when they scan the host
// groups in the sidebar; the tab count is a separate concept (see
// tabs.length) and does not belong here.
const sessionCount = computed(() => sessions.all.value.length);

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
    onNewTab: () => { if (caps.localPty) startNewTab(); },
    onSwitchTab,
    onToggleTaskSidebar: () => setSidebarCollapsedAndPersist(!sidebarCollapsed.value),
    onFocusSidebarSearch: () => {
      void taskSidebarRef.value?.focusSearch();
    },
  },
  { bindings: shortcutBindings },
);

// Persists tab/pane structure (+ AI sid captures) so a crash or unclean exit
// can rebuild the workspace on next launch. cwd/title heartbeat is a
// follow-up; the structural watch covers the v1 use case.
// recovery.json is a Wails/desktop-only concept — the web build already
// persists its workspace via webTabsSnapshot, and has no Go-side bindings for
// SaveRecoverySnapshot to call. Gate construction on caps.wailsBindings so the
// composable's watchers/timers never fire (and never throw) on web.
const recovery = caps.wailsBindings
  ? useRecoverySnapshot({
      tabs,
      currentTabId,
      localHostID,
      // Look up both lists — restricting to local would null out the host_id
      // / title / cwd we need to persist for remote panes (so the next launch
      // can re-bind them instead of forking a fresh local shell).
      sessionInfoFor: (sid: string) =>
        findSessionInfo(sid, false) ?? findSessionInfo(sid, true),
    })
  : null;

watch([tabs, currentTabId], () => {
  if (tabs.value.length === 0) return;
  if (currentTabId.value && tabs.value.find((t) => t.id === currentTabId.value)) return;
  gotoTab(tabs.value[0].id);
});

// Web-only URL hash sync: reflects the active pane's session id as
// `#/session/<sid>` (lib/webTabsSnapshot's formatHash) so the address bar is
// bookmarkable/shareable on a real web deployment. Uses history.replaceState
// (never `location.hash =`) so it never adds a history entry and never fires
// a `hashchange` event back at onHashChange below. Gated on
// `!caps.wailsBindings` — desktop owns its own `#/t/<tabId>` hash via
// gotoTab()/syncRoute() and this watcher would clobber it on every pane
// change (leaving the tab hash out of sync with the visible tab).
if (!caps.wailsBindings) {
  watch(currentTab, (t) => {
    const activePane = t?.panes[t.activePaneIdx];
    const sid = activePane?.sessionId ?? "";
    const target = sid ? formatHash(sid) : "#/";
    if (location.hash !== target) history.replaceState({}, "", target);
  });
}

// Web-only per-window tabs snapshot (lib/webTabsSnapshot). loadSnapshot()/
// saveSnapshot() key off a sessionStorage-scoped window id and are gated on
// `!caps.wailsBindings` so a Wails/Capacitor build never touches the
// browser-only localStorage entry (used to be a no-op relying on
// sessionStorage minting a fresh id per launch — the wailsBindings gate
// makes the platform check explicit).
let snapshotSaveHandle: ReturnType<typeof setTimeout> | null = null;
function scheduleSnapshotSave() {
  if (caps.wailsBindings) return;
  if (snapshotSaveHandle) clearTimeout(snapshotSaveHandle);
  snapshotSaveHandle = setTimeout(() => {
    snapshotSaveHandle = null;
    saveSnapshot({
      tabs: tabs.value.map((t) => ({
        id: t.id,
        layout: t.layout,
        active_pane_idx: t.activePaneIdx,
        col_ratio: t.colRatio,
        row_ratio: t.rowRatio,
        panes: t.panes
          .map((p, slot) => ({
            slot,
            session_id: p.sessionId ?? "",
            host_id: p.lastSeenInfo?.host_id,
            sealed: p.lastSeenInfo?.sealed,
          }))
          .filter((p) => p.session_id),
      })),
      active_tab_id: currentTabId.value ?? "",
    });
  }, 300);
}
if (!caps.wailsBindings) {
  watch(tabs, () => { scheduleSnapshotSave(); }, { deep: true });
}

// restoreFromWebSnapshot rebuilds tabs from the web-only localStorage
// snapshot at boot. Every pane restores as remote (the web build has no
// local PTY — see caps.localPty gating throughout this file), mirroring
// executeRestore's remote-pane branch: re-bind sessionId + synth
// lastSeenInfo, no spawn.
function restoreFromWebSnapshot(snap: WebTabsSnapshot) {
  const validLayouts: LayoutKind[] = ["single", "vertical", "horizontal", "grid2x2"];
  const newIds: string[] = [];
  let activeIdx = -1;
  for (const snapTab of snap.tabs) {
    const layout: LayoutKind = validLayouts.includes(snapTab.layout as LayoutKind)
      ? (snapTab.layout as LayoutKind)
      : "single";
    const t: Tab = {
      id: newId(),
      layout,
      activePaneIdx: snapTab.active_pane_idx,
      colRatio: snapTab.col_ratio ?? RATIO_DEFAULT,
      rowRatio: snapTab.row_ratio ?? RATIO_DEFAULT,
      panes: [],
    };
    const want = PANE_COUNT[layout];
    for (let i = 0; i < want; i++) {
      const p = snapTab.panes.find((pp) => pp.slot === i);
      if (!p || !p.session_id) {
        t.panes[i] = { sessionId: null, remote: false };
        continue;
      }
      t.panes[i] = {
        sessionId: p.session_id,
        remote: true,
        lastSeenInfo: synthSessionInfoFromSnapshot({
          slot: p.slot,
          remote: true,
          host_id: p.host_id,
          session_id: p.session_id,
          shell: "",
        }),
      };
    }
    if (snapTab.id === snap.active_tab_id) activeIdx = newIds.length;
    tabs.value.push(t);
    newIds.push(t.id);
  }
  if (activeIdx >= 0 && newIds[activeIdx]) gotoTab(newIds[activeIdx]);
  else if (newIds.length > 0) gotoTab(newIds[0]);
}

// onHashChange handles `#/session/<sid>` deep links after mount (e.g. a
// share link pasted into an already-open tab, or a hand-edited hash).
// `focus`/`permission` are parsed by parseHashSid but not yet enforced
// anywhere in the pane/session model — reserved for a follow-up once
// view-only / auto-focus semantics land; openRemoteAsTab's own
// findPaneLocation dedup already covers "already open → activate that pane".
function onHashChange() {
  const { sid } = parseHashSid(location.hash);
  if (!sid) return;
  openRemoteAsTab(sid);
}

onMounted(async () => {
  if (caps.wailsBindings) {
    try {
      sidebarCollapsed.value = await getTaskSidebarCollapsed();
    } catch {
      /* default = expanded (false) */
    }
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
  $platform.events.on('relay:auth-restored', () => {
    if (caps.wailsBindings) return;
    void (async () => {
      try {
        const ok = await refreshPlatformRelayState();
        if (ok) {
          status.value = "ready";
          startPlatformRemotePoll();
        }
      } catch (e) {
        console.warn("[AT Term] failed to refresh relay state after login", e);
      }
    })();
  });
  // Fire-and-forget: drives TabBar's admin button + AdminPanel gating only.
  // Not on the critical boot path — an unconfigured/unauthenticated relay
  // rejects immediately and just leaves isAdmin false.
  void (async () => {
    try {
      me.value = await $platform.relay.fetchMe();
    } catch {
      me.value = null;
    }
  })();
  syncRoute();
  window.addEventListener("hashchange", syncRoute);
  window.addEventListener("hashchange", onHashChange);
  if (caps.wailsBindings) {
    try {
      const fatal = await getStartupError();
      if (fatal?.fatal) {
        startupFatal.value = fatal;
        status.value = "error";
        errorMsg.value = fatal.message;
        return;
      }
    } catch (e) {
      console.warn("[AT Term] failed to load startup fatal state", e);
    }
  }
  // Set up the size-prediction probe before anything spawns a PTY — the
  // probe must be ready by the time auto-startNewTab fires.
  await setupMeasureProbe();
  if (caps.wailsBindings) {
    try {
      commandNotifyThresholdSec.value = await getCommandNotifyThresholdSeconds();
    } catch (e) {
      console.warn("[AT Term] failed to load command-notify threshold", e);
    }
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
  // The Wails-only boot chain: every call below reaches into window.go.main.App
  // via lib/api's `bindings()`, which throws "Wails bindings not ready" on the
  // web build. Gate the whole block on `caps.wailsBindings`; the web branch
  // below runs an equivalent bootstrap via the platform bridge.
  if (caps.wailsBindings) {
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
  } else {
    // Web boot: no local PTY, no Wails uplink. The session list is served
    // by the relay's HTTP API (via platform.sessions.listRemoteSessions),
    // polled every 3s (rarer than desktop's 2s WS-backed poll — no
    // subscribe channel here, so lower is wasted bandwidth). Marks status
    // ready as soon as the first poll returns so the UI leaves the
    // "loading" splash even when the list is empty.
    try {
      bootStage = "loadRelayConfig";
      const hasRelayConfig = await refreshPlatformRelayState();
      if (!hasRelayConfig && !caps.localPty) {
        openSettingsAccount();
      }
      status.value = "ready";
      if (hasRelayConfig) startPlatformRemotePoll();
    } catch (e: any) {
      const name = e?.name ?? "Error";
      const msg = e?.message ?? String(e);
      console.error(`[boot-web] step "${bootStage}" failed`, {
        name, message: msg, stack: e?.stack,
      });
      // Non-fatal: leave status=loading but log; user can retry via reload.
      // The session-list refresh interval is still installed below so a
      // transient error clears itself.
      remotePollHandle = window.setInterval(() => {
        void pollRemoteSessionsViaPlatform();
      }, 3000);
    }
  }

  // Auto-update poll: every 5s pull state.available || ready and toggle the
  // ⚙ badge dot. Lower frequency than session poll because update state
  // changes are rare (boot check + 24h ticker). Wails-only — on web/capacitor
  // there is no bundled auto-updater, and calling this every 5s only pollutes
  // the console with rejected promises.
  if (caps.wailsBindings) {
    updatePollHandle = window.setInterval(async () => {
      try {
        const st: UpdateState = await getUpdateState();
        updateBadge.value = !!(st.available || st.ready);
      } catch {
        /* ignore — never block UI on updater failures */
      }
    }, 5000);
  }

  // Auto-start guarded by try/catch: a stray throw here (e.g. a Wails-side
  // shape regression making `recoverySnap.tabs` null) used to escape onMounted
  // and leave the app stuck on the "starting first session" message with no
  // surfaced error. Funnel any failure to errorMsg so the TitleBar shows it
  // instead of failing silently.
  try {
    if (!autoStarted && tabs.value.length === 0) {
      autoStarted = true;
      // Web-only: a per-window tabs snapshot in localStorage takes priority
      // over the server-side recovery snapshot below — it reflects exactly
      // what this browser tab/window had open (see restoreFromWebSnapshot).
      // loadSnapshot() is always null on desktop/capacitor, so this branch
      // is a no-op there and falls straight through to the existing logic.
      const webSnap = loadSnapshot();
      if (webSnap && webSnap.tabs.length > 0) {
        restoreFromWebSnapshot(webSnap);
      } else {
        // Deep link: `#/session/<sid>` opens that one remote session as a
        // new tab instead of falling through to recovery/auto-start.
        const { sid: hashSid } = parseHashSid(location.hash);
        if (hashSid) {
          openRemoteAsTab(hashSid);
        } else {
          const hasRecovery = recoveryEnabled && (recoverySnap?.tabs?.length ?? 0) > 0;
          if (hasRecovery) {
            // Dialog handlers (onRecoveryRestore / onRecoveryDiscard) decide
            // whether to spawn restored panes or fall back to startNewTab — so
            // we deliberately do NOT call startNewTab() here.
            recoveryDialogState.value = { open: true, snapshot: recoverySnap };
          } else if (caps.localPty) {
            startNewTab();
          }
          // else: no recovery snapshot and no local PTY (web build) — render
          // the empty state (sidebar only, no tab) instead of crashing on a
          // startNewTab() that has nothing to spawn.
        }
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
  window.removeEventListener("hashchange", onHashChange);
  localSessionListConn?.detach();
  stopRemotePoll();
  if (toastHandle !== null) window.clearTimeout(toastHandle);
  if (updatePollHandle !== null) window.clearInterval(updatePollHandle);
  if (snapshotSaveHandle !== null) window.clearTimeout(snapshotSaveHandle);
  teardownMeasureProbe();
});

// Test-only surface: lets App.test.ts drive the isAdmin-transition watcher
// (me.value changes → isAdmin recomputes → adminViewOpen auto-resets) without
// standing up a second live fetchMe() round-trip mid-test.
defineExpose({ me });
</script>

<template>
  <div class="app" :class="[`fe-theme-${fileExplorerTheme}`, { 'is-maximized': showMaximizedInset }]" :style="themeStyle">
    <TitleBar
      v-if="caps.windowControls"
      :status="status"
      :error-msg="errorMsg"
      :session-count="sessionCount"
      :remote-endpoint="remoteEndpoint"
      :current-title="currentTitleForBar"
      :current-task-state="currentTaskStateForBar"
    />
    <PasteImagePreviewHost />
    <PasteFilePreviewHost />

    <div v-if="authError" class="auth-error-banner" role="alert">
      <span class="auth-error-msg">{{ authErrorMessage }}</span>
      <button class="auth-error-action" @click="openSettingsRelay">{{ i18nT("app.openSettings") }}</button>
      <button class="auth-error-dismiss" @click="authError = null" :aria-label="i18nT('app.dismiss')">×</button>
    </div>

    <TabBar
      v-if="!startupFatal"
      :tabs="tabSummaries"
      :current-id="currentTabId"
      :starting="starting"
      :can-new-local="caps.localPty"
      :update-badge="updateBadge"
      :is-admin="isAdmin"
      :admin-open="adminViewOpen"
      @activate="gotoTab"
      @close="requestCloseTab"
      @new="startNewTab"
      @new-ssh="showSshHosts = true"
      @reorder="onTabReorder"
      @open-settings="showSettings = true"
      @toggle-admin="adminViewOpen = !adminViewOpen"
    />

    <StartupFatalPanel v-if="startupFatal" :fatal="startupFatal" @quit="onConfirmQuit" />

    <div v-else class="main-row">
      <TaskSidebar
        ref="taskSidebarRef"
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
        :pane-location-for="paneLocationForSession"
        :tab-index-by-id="tabIndexById"
        @update:collapsed="setSidebarCollapsedAndPersist"
        @open="onSidebarOpen"
        @close="requestCloseSession"
        @merge-selected="mergeSelectedIntoTab"
        @close-selected="closeSelectedOpen"
        @markSeen="onMarkSeen"
      />
      <main class="main">
        <AdminPanel v-if="adminViewOpen" />
        <template v-else-if="localEndpoint || remoteEndpoint">
          <div v-if="tabs.length === 0" class="empty">
            {{ emptyMainMessage }}
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
            @close-pane="(idx) => requestClosePane(t, idx)"
            @toast="showToast"
            @update:col-ratio="(r) => { t.colRatio = r; }"
            @update:row-ratio="(r) => { t.rowRatio = r; }"
          />
        </template>
        <div v-if="toast" class="toast">{{ toast }}</div>
      </main>
      <template v-if="caps.pluginHost && rightPanelHasPlugin">
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
      v-if="caps.pluginHost"
      slot-id="bottom-toolbar"
      :context="pluginContext"
      class="bottom-toolbar"
    />
    <TranslatePanelHost v-if="caps.pluginHost" />

    <SettingsDialog
      v-if="showSettings"
      :local-session-count="localSessionCount"
      :remote-session-count="remoteSessionCount"
      :terminal-theme-id="currentTerminalThemeID"
      :initial-tab="settingsInitialTab"
      @terminal-theme-changed="onTerminalThemeChanged"
      @command-notify-threshold-changed="onCommandNotifyThresholdChanged"
      @relay-config-changed="refreshDesktopRelayConfig"
      @close="onSettingsClose"
    />
    <SessionPickerDialog
      v-if="pickerCtx"
      :exclude-session-ids="currentTab ? currentTab.panes.map((p) => p.sessionId).filter((id): id is string => !!id) : []"
      :local-sessions="localList"
      :remote-sessions="remoteList"
      @pick="onPickerPick"
      @close="onPickerClose"
    />
    <NewSshDialog
      v-if="showSshDialog"
      @connected="onSshConnected"
      @cancel="showSshDialog = false"
    />
    <SshHostsPanel
      v-if="showSshHosts"
      @connected="onSshConnected"
      @close="showSshHosts = false"
    />
    <ConfirmQuitDialog
      v-if="quitDialogOpen"
      :local-count="localSessionCount"
      :remote-count="remoteSessionCount"
      @confirm="onConfirmQuit"
      @cancel="onCancelQuit"
    />
    <ConfirmCloseSessionDialog
      v-if="pendingCloseSessions.length > 0"
      :title="pendingCloseTitle"
      :count="pendingCloseSessions.length"
      :is-ai="pendingCloseIsAi"
      :is-running="pendingCloseIsRunning"
      :is-remote="pendingCloseIsRemote"
      @confirm="confirmCloseSession"
      @cancel="cancelCloseSession"
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
  position: absolute; bottom: calc(24px + 12px); left: 50%; transform: translateX(-50%);
  background: rgba(13, 17, 23, 0.92); border: 1px solid var(--border);
  color: var(--fg); padding: 6px 12px; border-radius: 6px; font-size: 12px;
  pointer-events: none;
}

@media (max-width: 767px) {
  .app {
    height: 100dvh;
    padding-top: env(safe-area-inset-top);
    padding-bottom: env(safe-area-inset-bottom);
    background: var(--bg);
    overflow: hidden;
  }
  .main-row {
    min-width: 0;
  }
  .main {
    min-width: 0;
  }
  .auth-error-banner {
    padding: 7px 10px;
    gap: 6px;
  }
  .auth-error-action {
    padding: 2px 6px;
  }
  .toast {
    bottom: calc(12px + env(safe-area-inset-bottom));
    max-width: calc(100vw - 24px);
    text-align: center;
  }
}
</style>
