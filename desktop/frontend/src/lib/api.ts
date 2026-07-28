// Wails Go bindings shim. The Wails runtime injects window.go.main.App.<Method>
// at startup; we expose typed wrappers so the rest of the app doesn't have to
// reach into globals directly.

import { t } from "../i18n";
import type { PresetId } from "./taskState";
import type { SessionInfo } from "./connection";
import { decryptSessionFields } from "./connection";
import {
  InitializeNotifications,
  IsNotificationAvailable,
  CheckNotificationAuthorization,
  RequestNotificationAuthorization,
  SendNotification,
} from "../../wailsjs/runtime/runtime";

export type TaskGroupBy = "host" | "state";

export interface Endpoint {
  url: string;
  session_token: string;
}

export interface StartupError {
  fatal: boolean;
  message: string;
  log_path: string;
}

export type LocalePreference = "system" | "en" | "zh-CN";

export interface NewSessionReq {
  command: string;
  args?: string[];
  cwd?: string;
  cols?: number;
  rows?: number;
  // Filled by classifyAIKind() in lib/aiKind.ts when the user-typed command
  // matches a known AI CLI. Empty value disables sniff + resume.
  ai_kind?: "claude" | "codex" | "aider" | "";
  // Round-tripped from the previous run's snapshot during executeRestore.
  // Not used by Go to spawn the child — only the frontend injects the
  // resume command after prompt-ready.
  initial_ai_session_id?: string;
  // Original full command line the AI CLI was launched with (snapshot
  // last_command_line). Go merges claude's launch flags (e.g.
  // --permission-mode) into the injected `claude --resume <id>` so recovery
  // preserves them. Not passed as a spawn arg.
  initial_ai_command_line?: string;
}

export interface NewSessionResp {
  session_id: string;
}

export interface RecoveryAIInfo {
  kind: "claude" | "codex" | "aider";
  session_id?: string;
  captured_at_unix?: number;
}

export interface RecoveryPaneSnapshot {
  slot: number;
  // remote=true panes skip the spawn path on restore; the original session_id
  // is re-bound to the pane so the existing remote session resumes instead of
  // being replaced by a freshly forked local shell. host_id is informational
  // (lets the recovery dialog show which host a pane came from).
  remote?: boolean;
  host_id?: string;
  session_id?: string;
  shell: string;
  shell_args?: string[];
  last_cwd?: string;
  session_type?: string;
  last_command_line?: string;
  title?: string;
  ai?: RecoveryAIInfo;
}

export interface RecoveryTabSnapshot {
  id: string;
  layout: "single" | "vertical" | "horizontal" | "grid2x2";
  active_pane_idx: number;
  col_ratio: number;
  row_ratio: number;
  panes: RecoveryPaneSnapshot[];
}

export interface RecoverySnapshot {
  version: number;
  host_id: string;
  clean_shutdown: boolean;
  saved_at_unix: number;
  active_tab_id?: string;
  tabs: RecoveryTabSnapshot[];
}

export interface RelayConfig {
  url: string;
  token: string;
  // Unix-seconds expiry of `token` when it was minted as a relay session
  // token (e.g. via /api/pair/consume). 0 means "unknown" — treat `token`
  // as an opaque, long-lived credential. Always present on the wire so the
  // frontend can branch on `> 0` without optional-chaining.
  session_expires_at: number;
  allow_insecure_relay: boolean;
  // disable_e2ee, when true, turns off this desktop's agent-side sealing.
  // The account_key stays loaded so cross-desktop decrypt keeps working;
  // only outbound OUT / META / SessionInfo / CommandEventPayload sealing
  // is suppressed. Intended for testing the unsealed fallback path.
  // Optional in the type so iOS Capacitor's local RelayConfig fixtures
  // (which never run an agent) can omit it; Go always returns a
  // concrete bool over the wire, never undefined.
  disable_e2ee?: boolean;
  remote_permission: string;
  // Email cached from the most recent successful LoginRemoteRelay. Used
  // by Settings → Relay to prefill the email field on reopen. Read-only
  // from the frontend's perspective — setRelayConfig ignores it; only
  // loginRemoteRelay writes it.
  last_email: string;
  connected: boolean;
  // Loopback ws:// base the frontend attaches remote sessions through (the Go
  // remoteProxy). Read-only; empty when unavailable. Remote /client attaches
  // tunnel through Go because the WebView can't TLS-dial the relay directly on
  // networks that fingerprint-filter its handshake. Optional so Capacitor
  // fixtures may omit it.
  remote_proxy_url?: string;
  // realmId is the relay realm this session belongs to (from login finalize).
  // Written by mobile on login; consumed by subproject C for node selection.
  // Not present on desktop (Go manages realm identity there).
  realmId?: string;
  // homeInstanceURL is the user's home relay node for this realm (from login
  // finalize `home_instance_url`). Written by mobile on login; consumed by
  // subproject C for node selection. Empty/absent falls back to `url`.
  homeInstanceURL?: string;
}

export interface RelayMe {
  user_id: string;
  email: string;
}

export interface RelaySessionRow {
  id_hash: string;
  user_agent: string;
  ip_prefix: string;
  created_at: number;   // unix ms
  expires_at: number;   // unix ms
  is_current: boolean;
}

export interface SignOutOthersResult {
  deleted: number;
}

export interface DiagnosticsPayload {
  generated_at: string;
  app_version: string;
  os: string;
  arch: string;
  os_version: string;
  webview_summary: string;
  user_agent: string;
  relay_url: string;
  relay_status: string;
  relay_token_redacted: string;
  allow_insecure_relay: boolean;
  remote_permission: string;
  uplink_paused: boolean;
  recent_relay_errors: { timestamp: string; message: string }[];
  config: {
    default_shell: string;
    locale: string;
    terminal_theme: string;
    notifications_enabled: boolean;
    shell_integration_enabled: boolean;
    webgl_renderer_enabled: boolean;
    logging_enabled: boolean;
    log_file_path: string;
    auto_check_updates: boolean;
    command_notify_threshold_seconds: number;
  };
}

export interface PairingToken {
  token: string;
  expires_at: number;
  qr_url: string;
  wrapped: boolean;
}

export interface FeishuCredentials {
  AppID: string;
  AppSecret: string;
  EncryptKey: string;
  VerifyToken: string;
}

export interface FeishuStatusResp {
  enabled: boolean;
  mode: "local" | "relay";
  bound: boolean;
  open_id: string;
  disabled: boolean;
  // relay_disabled: relay reachable but the admin turned Feishu off server-side.
  relay_disabled?: boolean;
  // error: the status fetch failed; the real state is unknown. When set, the UI
  // must not claim the integration is disabled.
  error?: string;
  // configured: app credentials are stored (regardless of bind state). Drives
  // the "configured" view instead of an empty form — secrets are never echoed
  // back, so without this the form looks blank on reopen.
  configured?: boolean;
  // app_id: stored (non-secret) App ID, echoed so the UI can show which app is
  // configured. Present in local mode; empty in relay mode.
  app_id?: string;
  // app_id_hash: sha256(app_id) — suffix of the event callback URL.
  app_id_hash?: string;
  // callback_url: relay event endpoint to paste into the Feishu console. Set
  // only in relay mode; empty in local mode (long-conn, no public URL).
  callback_url?: string;
}

// HookInstallState mirrors desktop/hookinstall.State (json tags). Returned by
// GetHookInstallState; rendered by SettingsFeishu's status row.
export interface HookInstallState {
  enabled: boolean;
  binary_path: string;
  binary_ok: boolean;
  binary_version: string;
  settings_path: string;
  settings_ok: boolean;
  last_error: string;
  last_check: string; // ISO timestamp (Go time.Time -> JSON string)
}

export interface HostInfo {
  host_id: string;
  host: string;
  user: string;
}

export interface LoggingConfig {
  enabled: boolean;
  path: string;
  effective_path: string;
  dev_dual_output: boolean;
}

export interface LogPreview {
  path: string;
  exists: boolean;
  truncated: boolean;
  content: string;
}

export interface ClipboardPastePayload {
  kind: "none" | "text" | "image";
  text?: string;
  filename?: string;
  content_type?: string;
  data_base64?: string;
  reason?: string;
}

// Mirrors desktop/updater.go VersionLine. One available minor-version line.
export interface VersionLine {
  minor: string;
  latest: string;
  notes: string;
  asset_url: string;
}

// Mirrors desktop/updater.go UpdateState. Field names are snake_case from
// Wails JSON marshaling; we match exactly.
export interface UpdateState {
  current: string;
  latest: string;
  available: boolean;
  notes: string;
  checking: boolean;
  last_check_at: number;
  downloading: boolean;
  download_pct: number;
  ready: boolean;
  error: string;
  asset_url: string;
  asset_size: number;
  download_dir: string;
  download_path: string;
  lines: VersionLine[];
  // downloaded_exists is true when the most recent DownloadVersion /
  // StartDownload call short-circuited to Ready because the archive was
  // already on disk. The frontend watches false→true to prompt the user
  // whether to redownload.
  downloaded_exists: boolean;
}

interface AppBindings {
  GetClipboardPastePayload(): Promise<ClipboardPastePayload>;
  GetStartupError?(): Promise<StartupError>;
  GetEndpoint(): Promise<Endpoint>;
  GetHostInfo(): Promise<HostInfo>;
  NewSession(req: NewSessionReq): Promise<NewSessionResp>;
  CloseSession(sessionID: string): Promise<void>;
  ListShells(): Promise<string[]>;
  GetRelayConfig(): Promise<RelayConfig>;
  SetRelayConfig(cfg: RelayConfig): Promise<void>;
  ClearRelayConfig(): Promise<void>;
  SetRelayDisableE2EE(disabled: boolean): Promise<void>;
  SetUplinkPaused(paused: boolean): Promise<void>;
  GetUplinkHealth(): Promise<ConnHealthSnapshot>;
  LoginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void>;
  RegisterRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void>;
  HasAccountKey(): Promise<boolean>;
  GetAccountKey(): Promise<string>;
  LoadSavedRelayPassword(): Promise<string>;
  RememberRelayPassword(password: string): Promise<void>;
  ProbeRelayVersion(arg1: string, arg2: boolean): Promise<void>;
  FetchRelayMe(): Promise<RelayMe>;
  ListRelaySessions(): Promise<RelaySessionRow[]>;
  RevokeRelaySession(idHash: string): Promise<void>;
  SignOutOtherRelaySessions(): Promise<SignOutOthersResult>;
  CreatePairingToken(): Promise<PairingToken>;
  GetLoggingConfig(): Promise<LoggingConfig>;
  SetLoggingConfig(cfg: LoggingConfig): Promise<void>;
  PickLogFilePath(): Promise<string>;
  GetLogPreview(): Promise<LogPreview>;
  GetTerminalTheme(): Promise<string>;
  SetTerminalTheme(themeID: string): Promise<void>;
  GetLocalePreference(): Promise<LocalePreference>;
  SetLocalePreference(preference: LocalePreference): Promise<void>;
  GetDefaultShell(): Promise<string>;
  SetDefaultShell(shell: string): Promise<void>;
  GetUpdateState(): Promise<UpdateState>;
  CheckUpdate(): Promise<void>;
  StartDownload(): Promise<void>;
  DownloadVersion(tag: string): Promise<void>;
  CancelDownload(): Promise<void>;
  ForceRedownload(tag: string): Promise<void>;
  InstallUpdate(): Promise<void>;
  GetAutoCheckUpdates(): Promise<boolean>;
  SetAutoCheckUpdates(enabled: boolean): Promise<void>;
  GetUpdateGHProxyURL(): Promise<string>;
  SetUpdateGHProxyURL(proxyURL: string): Promise<void>;
  ConfirmQuit(): Promise<void>;
  GetNotificationsEnabled(): Promise<boolean>;
  SetNotificationsEnabled(enabled: boolean): Promise<void>;
  GetAINotificationsOnly(): Promise<boolean>;
  SetAINotificationsOnly(enabled: boolean): Promise<void>;
  GetFeishuModePref(): Promise<string>;
  SetFeishuModePref(pref: string): Promise<void>;
  GetFeishuEffectiveMode(): Promise<string>;
  GetFeishuRemoteTerminalSettings(): Promise<FeishuRemoteTerminalSettings>;
  SetFeishuRemoteTerminalSettings(enabled: boolean, autoAttach: string): Promise<void>;
  GetPtyInputDebugEnabled(): Promise<boolean>;
  SetPtyInputDebugEnabled(enabled: boolean): Promise<void>;
  ShowNotification(title: string, body: string): Promise<void>;
  GetShellIntegrationEnabled(): Promise<boolean>;
  SetShellIntegrationEnabled(enabled: boolean): Promise<void>;
  LoadRecoverySnapshot(): Promise<RecoverySnapshot>;
  SaveRecoverySnapshot(payload: string): Promise<void>;
  DiscardRecoverySnapshot(): Promise<void>;
  GetRecoveryDialogEnabled(): Promise<boolean>;
  SetRecoveryDialogEnabled(enabled: boolean): Promise<void>;
  GetWebglRendererEnabled(): Promise<boolean>;
  SetWebglRendererEnabled(enabled: boolean): Promise<void>;
  GetCommandNotifyThresholdSeconds(): Promise<number>;
  SetCommandNotifyThresholdSeconds(seconds: number): Promise<void>;
  BroadcastCommandFinished(sessionId: string, exitCode: number, elapsedMs: number, label: string): Promise<void>;
  GetDiagnostics(userAgent: string): Promise<DiagnosticsPayload>;
  ExportDiagnostics(content: string): Promise<string>;
  GetFeishuStatus(): Promise<FeishuStatusResp>;
  SetFeishuCredentials(c: FeishuCredentials): Promise<void>;
  BeginFeishuPair(): Promise<string>;
  DeleteFeishuBinding(): Promise<void>;
  SendFeishuTestCard(scenario: string): Promise<void>;
  GetHookInstallState(): Promise<HookInstallState>;
  SetHookInstallEnabled(on: boolean): Promise<void>;
  GetQuickTemplates(): Promise<import('./templates').QuickTemplate[]>;
  SetQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void>;
  GetTaskPreset(): Promise<string>;
  SetTaskPreset(preset: PresetId): Promise<void>;
  GetTaskGroupBy(): Promise<string>;
  SetTaskGroupBy(groupBy: TaskGroupBy): Promise<void>;
  GetTaskSidebarCollapsed(): Promise<boolean>;
  SetTaskSidebarCollapsed(collapsed: boolean): Promise<void>;
  GetTaskSidebarWidth(): Promise<number>;
  SetTaskSidebarWidth(px: number): Promise<void>;
  GetPinnedSessionIds(): Promise<string[]>;
  SetPinnedSessionIds(ids: string[]): Promise<void>;
  GetUserHomeDir(): Promise<string>;
  GetAppVersion(): Promise<string>;
  MarkSessionsSeen(ids: string[], all: boolean): Promise<void>;
  ListRemoteSessions(): Promise<string>;
}

declare global {
  interface Window {
    go?: {
      main?: {
        App?: AppBindings;
      };
    };
  }
}

let _bindingsOverride: AppBindings | undefined;
let notificationRuntimeReady: Promise<boolean> | undefined;
let notificationID = 0;

export function __setBindingsForTest(b: AppBindings | undefined): void {
  _bindingsOverride = b;
}

function resetNotificationRuntime(): void {
  notificationRuntimeReady = undefined;
  notificationID = 0;
}

export function __resetNotificationRuntimeForTest(): void {
  resetNotificationRuntime();
}

function bindings(): AppBindings {
  if (_bindingsOverride) return _bindingsOverride;
  const b = window.go?.main?.App;
  if (!b) throw new Error(t("app.wailsBindingsNotReady"));
  return b;
}

async function ensureNotificationRuntimeReady(): Promise<boolean> {
  if (!notificationRuntimeReady) {
    notificationRuntimeReady = (async () => {
      try {
        if (!(await IsNotificationAvailable())) return false;
        await InitializeNotifications();
        if (await CheckNotificationAuthorization()) return true;
        return await RequestNotificationAuthorization();
      } catch {
        return false;
      }
    })();
  }
  return notificationRuntimeReady;
}

export function getClipboardPastePayload(): Promise<ClipboardPastePayload> {
  return bindings().GetClipboardPastePayload();
}

export function getStartupError(): Promise<StartupError> {
  const b = bindings();
  if (!b.GetStartupError) {
    return Promise.resolve({ fatal: false, message: "", log_path: "" });
  }
  return b.GetStartupError();
}

export function getEndpoint(): Promise<Endpoint> {
  return bindings().GetEndpoint();
}

export function newSession(req: NewSessionReq): Promise<NewSessionResp> {
  return bindings().NewSession(req);
}

export function closeSession(sessionId: string): Promise<void> {
  return bindings().CloseSession(sessionId);
}

// Cached shell list. ListShells shells out to exec.LookPath on the Go side;
// the result is stable for a session and only changes when the user picks a
// different default shell, so cache it to keep new-tab spawns off the IPC +
// lookup path. Invalidated by setDefaultShell (the configured default reorders
// the output) and by __resetShellsCacheForTest.
let shellsCache: Promise<string[]> | null = null;

export function __resetShellsCacheForTest(): void {
  shellsCache = null;
}

export function listShells(): Promise<string[]> {
  if (!shellsCache) {
    shellsCache = bindings()
      .ListShells()
      .catch((e) => {
        shellsCache = null; // don't cache failures — allow retry
        throw e;
      });
  }
  return shellsCache;
}

export function getRelayConfig(): Promise<RelayConfig> {
  return bindings().GetRelayConfig();
}

export function setRelayConfig(cfg: {
  url: string;
  token: string;
  session_expires_at?: number;
  allow_insecure_relay?: boolean;
  disable_e2ee?: boolean;
  remote_permission?: string;
  // When provided, persists the cached email (used to remember inputs after a
  // failed login). Empty leaves the stored value untouched.
  last_email?: string;
}): Promise<void> {
  return bindings().SetRelayConfig({
    url: cfg.url,
    token: cfg.token,
    session_expires_at: cfg.session_expires_at ?? 0,
    allow_insecure_relay: cfg.allow_insecure_relay ?? false,
    disable_e2ee: cfg.disable_e2ee ?? false,
    remote_permission: cfg.remote_permission ?? "full",
    last_email: cfg.last_email ?? "",
    connected: false,
  });
}

export function clearRelayConfig(): Promise<void> {
  return bindings().ClearRelayConfig();
}

// setRelayDisableE2EE flips the agent-side seal toggle directly without
// touching URL / token / permission — used by Settings when the user
// wants the change to take effect immediately, not on the next "Save".
export function setRelayDisableE2EE(disabled: boolean): Promise<void> {
  return bindings().SetRelayDisableE2EE(disabled);
}

export function setUplinkPaused(paused: boolean): Promise<void> {
  return bindings().SetUplinkPaused(paused);
}

// loginRemoteRelay drives the OPAQUE login flow on the Go side. The Wails
// method completes the protocol round-trip, persists the returned session
// token via SetRelayConfig, unlocks the account_key into App memory, and
// restarts the uplink — callers only need to refresh GetRelayConfig()
// afterwards.
export function loginRemoteRelay(relayURL: string, email: string, password: string, allowInsecure: boolean): Promise<void> {
  return bindings().LoginRemoteRelay(relayURL, email, password, allowInsecure);
}

// registerRemoteRelay drives the OPAQUE registration flow on the Go side.
// Same persistence semantics as loginRemoteRelay; claimToken is optional
// (supply the plaintext token printed by `atterm-relay` bootstrap to also
// promote the new user to admin, otherwise pass "").
export function registerRemoteRelay(relayURL: string, email: string, password: string, claimToken: string, allowInsecure: boolean): Promise<void> {
  return bindings().RegisterRemoteRelay(relayURL, email, password, claimToken, allowInsecure);
}

// hasAccountKey reports whether the E2EE account_key is currently unlocked
// in App memory. False after app restart (key is in-memory only in v1) —
// the frontend uses this to decide between "unlock" and full-login prompts.
export function hasAccountKey(): Promise<boolean> {
  return bindings().HasAccountKey();
}

// getAccountKey returns the unlocked account_key as a base64 std string,
// or empty string when locked / no user. The Wails platform layer
// caches this in JS memory so MetaPayload.Sealed decrypt in the hot
// path stays synchronous. See platform/wails.ts.
export function getAccountKey(): Promise<string> {
  return bindings().GetAccountKey();
}

// loadSavedRelayPassword reads the OPAQUE password that the most recent
// successful loginRemoteRelay / registerRemoteRelay persisted for the
// current relay URL + email. Returns "" when nothing is stored so the
// SettingsRelay password field can default to empty without extra checks.
export function loadSavedRelayPassword(): Promise<string> {
  return bindings().LoadSavedRelayPassword();
}

// rememberRelayPassword persists the typed-but-not-yet-verified password
// into the safekeyring slot tied to the current (relay URL, email) pair.
// Used by SettingsRelay's rememberInputs() on failed-connect paths so the
// password field is prefilled on the next attempt. Best-effort on the Go
// side — never throws even if the keychain is unavailable.
export function rememberRelayPassword(password: string): Promise<void> {
  return bindings().RememberRelayPassword(password);
}

// probeRelayVersion calls the Wails ProbeRelayVersion method on the Go side
// to verify the URL points at an atterm relay. Throws on probe failure.
// allowInsecure skips TLS verification so a self-signed relay is reachable.
export function probeRelayVersion(url: string, allowInsecure: boolean): Promise<void> {
  return bindings().ProbeRelayVersion(url, allowInsecure);
}

// ConnHealthSnapshot mirrors internal/connhealth.Snapshot. Returned by the
// Wails GetUplinkHealth method and rendered by the ConnHealthPill / drawer.
export interface ConnHealthSnapshot {
  state: "closed" | "connecting" | "connected" | "reconnecting";
  rtt: {
    last_ms: number | null;
    p50_ms: number | null;
    p95_ms: number | null;
  };
  rtt_samples: Array<{ at_ms: number; rtt_ms: number }>;
  reconnect: {
    count_last_hour: number;
    last_at_ms: number | null;
    last_reason: string;
    history: Array<{ at_ms: number; reason: string; duration_ms: number }>;
  };
  bytes: {
    in_per_sec: number;
    out_per_sec: number;
  };
  seq_gaps: number;
}

export function getUplinkHealth(): Promise<ConnHealthSnapshot> {
  return bindings().GetUplinkHealth();
}

export function getLoggingConfig(): Promise<LoggingConfig> {
  return bindings().GetLoggingConfig();
}

export function setLoggingConfig(cfg: {
  enabled: boolean;
  path?: string;
}): Promise<void> {
  return bindings().SetLoggingConfig({
    enabled: cfg.enabled,
    path: cfg.path ?? "",
    effective_path: "",
    dev_dual_output: false,
  });
}

export function pickLogFilePath(): Promise<string> {
  return bindings().PickLogFilePath();
}

export function getLogPreview(): Promise<LogPreview> {
  return bindings().GetLogPreview();
}

export function getTerminalThemePreference(): Promise<string> {
  return bindings().GetTerminalTheme();
}

export function setTerminalThemePreference(themeID: string): Promise<void> {
  return bindings().SetTerminalTheme(themeID);
}

export function getLocalePreference(): Promise<LocalePreference> {
  return bindings().GetLocalePreference();
}

export function setLocalePreference(preference: LocalePreference): Promise<void> {
  return bindings().SetLocalePreference(preference);
}

export function getDefaultShell(): Promise<string> {
  return bindings().GetDefaultShell();
}

export async function setDefaultShell(shell: string): Promise<void> {
  await bindings().SetDefaultShell(shell);
  shellsCache = null; // the configured default reorders ListShells output
}

export function getHostInfo(): Promise<HostInfo> {
  return bindings().GetHostInfo();
}

export function getUpdateState(): Promise<UpdateState> {
  return bindings().GetUpdateState();
}

export function checkUpdate(): Promise<void> {
  return bindings().CheckUpdate();
}

export function startDownload(): Promise<void> {
  return bindings().StartDownload();
}

export function downloadVersion(tag: string): Promise<void> {
  return bindings().DownloadVersion(tag);
}

export function cancelDownload(): Promise<void> {
  return bindings().CancelDownload();
}
export function forceRedownload(tag: string): Promise<void> {
  return bindings().ForceRedownload(tag);
}

export function installUpdate(): Promise<void> {
  return bindings().InstallUpdate();
}

export function getAutoCheckUpdates(): Promise<boolean> {
  return bindings().GetAutoCheckUpdates();
}

export function setAutoCheckUpdates(enabled: boolean): Promise<void> {
  return bindings().SetAutoCheckUpdates(enabled);
}

export function getUpdateGHProxyURL(): Promise<string> {
  return bindings().GetUpdateGHProxyURL();
}

export function setUpdateGHProxyURL(proxyURL: string): Promise<void> {
  return bindings().SetUpdateGHProxyURL(proxyURL);
}

export function confirmQuit(): Promise<void> {
  return bindings().ConfirmQuit();
}

export function getNotificationsEnabled(): Promise<boolean> {
  return bindings().GetNotificationsEnabled();
}

export function setNotificationsEnabled(enabled: boolean): Promise<void> {
  return bindings().SetNotificationsEnabled(enabled);
}

export function getAINotificationsOnly(): Promise<boolean> {
  return bindings().GetAINotificationsOnly();
}

export function setAINotificationsOnly(enabled: boolean): Promise<void> {
  return bindings().SetAINotificationsOnly(enabled);
}

export function getFeishuModePref(): Promise<string> {
  return bindings().GetFeishuModePref();
}

export function setFeishuModePref(pref: string): Promise<void> {
  return bindings().SetFeishuModePref(pref);
}

export function getFeishuEffectiveMode(): Promise<string> {
  return bindings().GetFeishuEffectiveMode();
}

export interface FeishuRemoteTerminalSettings {
  enabled: boolean;
  auto_attach: string;
}

export function getFeishuRemoteTerminalSettings(): Promise<FeishuRemoteTerminalSettings> {
  return bindings().GetFeishuRemoteTerminalSettings();
}

export function setFeishuRemoteTerminalSettings(enabled: boolean, autoAttach: string): Promise<void> {
  return bindings().SetFeishuRemoteTerminalSettings(enabled, autoAttach);
}

export function getPtyInputDebugEnabled(): Promise<boolean> {
  return bindings().GetPtyInputDebugEnabled();
}

export function setPtyInputDebugEnabled(enabled: boolean): Promise<void> {
  return bindings().SetPtyInputDebugEnabled(enabled);
}

export interface NotificationRouteData {
  session_id?: string;
  kind?: string;
}

export async function showNotification(title: string, body: string, data?: NotificationRouteData): Promise<void> {
  // Honor the user's "Show system notifications" toggle BEFORE we touch any
  // notification backend. The Wails runtime's SendNotification (preferred
  // path below) talks to macOS UserNotifications directly and would
  // otherwise bypass NotificationsEnabledOrDefault — which only the Go
  // fallback checks. A binding error (e.g. boot race before window.go is
  // wired) defaults to "allow" so we don't silently drop notifications.
  try {
    if (!(await getNotificationsEnabled())) return;
  } catch {
    /* default-allow on lookup failure */
  }
  if (await ensureNotificationRuntimeReady()) {
    try {
      notificationID += 1;
      await SendNotification({
        id: `atterm-${Date.now()}-${notificationID}`,
        title,
        body,
        ...(data ? { data } : {}),
      });
      return;
    } catch {
      resetNotificationRuntime();
    }
  }
  return bindings().ShowNotification(title, body);
}

export function getShellIntegrationEnabled(): Promise<boolean> {
  return bindings().GetShellIntegrationEnabled();
}

export function setShellIntegrationEnabled(enabled: boolean): Promise<void> {
  return bindings().SetShellIntegrationEnabled(enabled);
}

export function loadRecoverySnapshot(): Promise<RecoverySnapshot> {
  return bindings().LoadRecoverySnapshot();
}

export function saveRecoverySnapshot(snap: RecoverySnapshot): Promise<void> {
  return bindings().SaveRecoverySnapshot(JSON.stringify(snap));
}

export function discardRecoverySnapshot(): Promise<void> {
  return bindings().DiscardRecoverySnapshot();
}

export function getRecoveryDialogEnabled(): Promise<boolean> {
  return bindings().GetRecoveryDialogEnabled();
}

export function setRecoveryDialogEnabled(enabled: boolean): Promise<void> {
  return bindings().SetRecoveryDialogEnabled(enabled);
}

export function getWebglRendererEnabled(): Promise<boolean> {
  return bindings().GetWebglRendererEnabled();
}

export function setWebglRendererEnabled(enabled: boolean): Promise<void> {
  return bindings().SetWebglRendererEnabled(enabled);
}

export function getCommandNotifyThresholdSeconds(): Promise<number> {
  return bindings().GetCommandNotifyThresholdSeconds();
}

export function setCommandNotifyThresholdSeconds(seconds: number): Promise<void> {
  return bindings().SetCommandNotifyThresholdSeconds(seconds);
}

export function broadcastCommandFinished(
  sessionId: string,
  exitCode: number,
  elapsedMs: number,
  label: string,
): Promise<void> {
  return bindings().BroadcastCommandFinished(sessionId, exitCode, elapsedMs, label);
}

// fetchRelayMe calls the Go backend (FetchRelayMe Wails binding) which makes
// an HTTP GET to the configured relay's /api/me endpoint using the stored API
// token. The returned email is held in memory only (SEC-1 — not persisted).
export function fetchRelayMe(): Promise<RelayMe> {
  return bindings().FetchRelayMe();
}

export function listRelaySessions(): Promise<RelaySessionRow[]> {
  return bindings().ListRelaySessions();
}

export function revokeRelaySession(idHash: string): Promise<void> {
  return bindings().RevokeRelaySession(idHash);
}

export function signOutOtherRelaySessions(): Promise<SignOutOthersResult> {
  return bindings().SignOutOtherRelaySessions();
}

// createPairingToken asks the relay to mint a 5-minute single-use pairing
// token via the desktop's existing API-token-authenticated channel. The
// returned qr_url is the value to encode into the QR image.
export function createPairingToken(): Promise<PairingToken> {
  return bindings().CreatePairingToken();
}

export function getDiagnostics(userAgent: string): Promise<DiagnosticsPayload> {
  return bindings().GetDiagnostics(userAgent);
}

export function exportDiagnostics(content: string): Promise<string> {
  return bindings().ExportDiagnostics(content);
}

export function getQuickTemplates(): Promise<import('./templates').QuickTemplate[]> {
  return bindings().GetQuickTemplates();
}

export function setQuickTemplates(list: import('./templates').QuickTemplate[]): Promise<void> {
  return bindings().SetQuickTemplates(list);
}

export function getTaskPreset(): Promise<string> {
  return bindings().GetTaskPreset();
}

export function setTaskPreset(preset: PresetId): Promise<void> {
  return bindings().SetTaskPreset(preset);
}

export function getTaskGroupBy(): Promise<string> {
  return bindings().GetTaskGroupBy();
}

export function setTaskGroupBy(groupBy: TaskGroupBy): Promise<void> {
  return bindings().SetTaskGroupBy(groupBy);
}

export function getTaskSidebarCollapsed(): Promise<boolean> {
  return bindings().GetTaskSidebarCollapsed();
}

export function setTaskSidebarCollapsed(collapsed: boolean): Promise<void> {
  return bindings().SetTaskSidebarCollapsed(collapsed);
}

export function getTaskSidebarWidth(): Promise<number> {
  return bindings().GetTaskSidebarWidth();
}
export function setTaskSidebarWidth(px: number): Promise<void> {
  return bindings().SetTaskSidebarWidth(px);
}

export function getPinnedSessionIds(): Promise<string[]> {
  return bindings().GetPinnedSessionIds();
}
export function setPinnedSessionIds(ids: string[]): Promise<void> {
  return bindings().SetPinnedSessionIds(ids);
}

export function getUserHomeDir(): Promise<string> {
  return bindings().GetUserHomeDir();
}

export function getAppVersion(): Promise<string> {
  return bindings().GetAppVersion();
}

export type MarkSessionsSeenOpts = { ids: string[] } | { all: true };

export function markSessionsSeen(opts: MarkSessionsSeenOpts): Promise<void> {
  if ("all" in opts && opts.all) {
    return bindings().MarkSessionsSeen([], true);
  }
  return bindings().MarkSessionsSeen((opts as { ids: string[] }).ids, false);
}

// listRemoteSessions fetches the relay's owner-filtered session list through
// the Go backend (App.ListRemoteSessions) rather than a direct webview
// WebSocket — the WKWebView TLS handshake to the relay is fingerprint-RST on
// some networks while Go's TLS passes. The Go side returns the raw
// /api/sessions JSON, the same SessionInfo[] shape the WS LIST_RESP carries.
export async function listRemoteSessions(): Promise<SessionInfo[]> {
  const raw = await bindings().ListRemoteSessions();
  const parsed = JSON.parse(raw) as SessionInfo[] | null;
  // The Go side returns the relay's raw JSON verbatim, so the E2EE-sealed
  // title/cwd/command fields are still ciphertext here — decrypt them with the
  // unlocked account_key the same way the WS META and Capacitor list paths do.
  return decryptSessionFields(parsed ?? []);
}

export function getFeishuStatus(): Promise<FeishuStatusResp> {
  return bindings().GetFeishuStatus();
}

export function setFeishuCredentials(c: FeishuCredentials): Promise<void> {
  return bindings().SetFeishuCredentials(c);
}

export function beginFeishuPair(): Promise<string> {
  return bindings().BeginFeishuPair();
}

export function deleteFeishuBinding(): Promise<void> {
  return bindings().DeleteFeishuBinding();
}

// sendFeishuTestCard renders and sends one notification card to the bound
// OpenID through the live token + IM path, so the user can confirm delivery
// from Settings. scenario ∈ {command_success, command_failure, command_sealed,
// waiting_input}. Rejects with the backend error message on any failure.
export function sendFeishuTestCard(scenario: string): Promise<void> {
  return bindings().SendFeishuTestCard(scenario);
}

// getHookInstallState returns the current Claude Code hook auto-install
// health. The backend silently runs Install when enabled + unhealthy +
// not already attempted in the debounce window, so the returned state
// reflects the post-repair situation.
export function getHookInstallState(): Promise<HookInstallState> {
  return bindings().GetHookInstallState();
}

// setHookInstallEnabled persists the toggle and either installs or
// uninstalls. Errors propagate so the Retry button in Settings can
// surface them.
export function setHookInstallEnabled(on: boolean): Promise<void> {
  return bindings().SetHookInstallEnabled(on);
}
